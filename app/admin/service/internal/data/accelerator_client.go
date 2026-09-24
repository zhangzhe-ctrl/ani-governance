package data

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	acc "github.com/zhangzhe-ctrl/ani-accelerator-service/api/gen/go/accelerator/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const GovernanceAcceleratorURI = "spiffe://ani.internal/service/ani-governance"

type AcceleratorClientConfig struct {
	Address, ServerName, CAFile, CertFile, KeyFile string
	Timeout                                        time.Duration
}

// AcceleratorClient exposes only the public module's generated contracts.
// The interceptor reconstructs all outgoing identity metadata from typed
// delegated contexts. User credentials and public identity headers are dropped.
type AcceleratorClient struct {
	Admin   acc.AcceleratorAdminServiceClient
	Catalog acc.AcceleratorCatalogServiceClient
	Usage   acc.AcceleratorUsageServiceClient
}

func AcceleratorConfigFromEnv() (AcceleratorClientConfig, error) {
	c := AcceleratorClientConfig{Address: os.Getenv("ANI_ACCELERATOR_ADDR"), ServerName: os.Getenv("ANI_ACCELERATOR_SERVER_NAME"), CAFile: os.Getenv("ANI_ACCELERATOR_CA"), CertFile: os.Getenv("ANI_ACCELERATOR_CERT"), KeyFile: os.Getenv("ANI_ACCELERATOR_KEY"), Timeout: 3 * time.Second}
	if value := os.Getenv("ANI_ACCELERATOR_TIMEOUT"); value != "" {
		d, err := time.ParseDuration(value)
		if err != nil {
			return c, err
		}
		c.Timeout = d
	}
	return c, nil
}

func NewAcceleratorClient(c AcceleratorClientConfig) (*AcceleratorClient, func(), error) {
	if c.Address == "" || c.ServerName == "" || c.Timeout <= 0 {
		return nil, nil, fmt.Errorf("accelerator address, exact server DNS name and positive timeout required")
	}
	pem, err := os.ReadFile(c.CAFile)
	if err != nil {
		return nil, nil, fmt.Errorf("accelerator CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, nil, fmt.Errorf("accelerator CA has no certificates")
	}
	cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("accelerator client identity: %w", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, nil, err
	}
	if len(leaf.URIs) != 1 || leaf.URIs[0].String() != GovernanceAcceleratorURI {
		return nil, nil, fmt.Errorf("accelerator client certificate requires unique exact Governance URI SAN")
	}
	var conn *grpc.ClientConn
	interceptor := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		md, err := acceleratorMetadata(method, req)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(metadata.NewOutgoingContext(ctx, md), c.Timeout)
		defer cancel()
		err = invoke(ctx, method, req, reply, cc, opts...)
		if status.Code(err) == codes.DeadlineExceeded && cc.GetState() != connectivity.Ready {
			return status.Error(codes.Unavailable, "accelerator transport unavailable")
		}
		return err
	}
	conn, err = grpc.NewClient(c.Address, grpc.WithDisableServiceConfig(), grpc.WithUnaryInterceptor(interceptor), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{cert}, ServerName: c.ServerName})))
	if err != nil {
		return nil, nil, err
	}
	return &AcceleratorClient{Admin: acc.NewAcceleratorAdminServiceClient(conn), Catalog: acc.NewAcceleratorCatalogServiceClient(conn), Usage: acc.NewAcceleratorUsageServiceClient(conn)}, func() { _ = conn.Close() }, nil
}

// SyncGpuUsage intentionally has no delegated actor/action/tenant metadata.
// Its authority is the exact service certificate plus persisted ledger facts.
func (c *AcceleratorClient) SyncGpuUsage(ctx context.Context, r *acc.SyncGpuUsageRequest) (*acc.SyncGpuUsageResponse, error) {
	return c.Usage.SyncGpuUsage(ctx, r)
}

func acceleratorMetadata(method string, request any) (metadata.MD, error) {
	invalid := func() (metadata.MD, error) {
		return nil, status.Error(codes.PermissionDenied, "invalid accelerator delegation")
	}
	parts := strings.Split(method, "/")
	if len(parts) != 3 {
		return invalid()
	}
	action := parts[2]
	if method == acc.AcceleratorUsageService_SyncGpuUsage_FullMethodName {
		r, ok := request.(*acc.SyncGpuUsageRequest)
		if !ok || r == nil || r.RequestId == "" || r.Projection == nil {
			return invalid()
		}
		return metadata.MD{}, nil
	}
	var actor *acc.Actor
	var tenant, requestID string
	switch r := request.(type) {
	case interface{ GetMutation() *acc.AdminMutation }:
		if !strings.HasPrefix(method, "/accelerator.v1.AcceleratorAdminService/") {
			return invalid()
		}
		m := r.GetMutation()
		if m == nil || m.Context == nil {
			return invalid()
		}
		actor = m.Context.Actor
		requestID = m.Context.RequestId
	case interface{ GetContext() *acc.AdminRead }:
		if !strings.HasPrefix(method, "/accelerator.v1.AcceleratorAdminService/") {
			return invalid()
		}
		a := r.GetContext()
		if a == nil {
			return invalid()
		}
		actor = a.Actor
		requestID = a.RequestId
	case interface{ GetContext() *acc.TenantContext }:
		if !strings.HasPrefix(method, "/accelerator.v1.AcceleratorCatalogService/") && !strings.HasPrefix(method, "/accelerator.v1.AcceleratorUsageService/") {
			return invalid()
		}
		t := r.GetContext()
		if t == nil {
			return invalid()
		}
		actor = t.Actor
		tenant = t.TenantId
		requestID = t.RequestId
		id, err := uuid.Parse(tenant)
		if err != nil || id == uuid.Nil || id.String() != tenant {
			return invalid()
		}
	default:
		return invalid()
	}
	if actor == nil || actor.Type != "user" || actor.Id == "" || requestID == "" {
		return invalid()
	}
	md := metadata.Pairs("x-ani-action", action, "x-ani-actor-type", actor.Type, "x-ani-actor-id", actor.Id)
	if tenant != "" {
		md.Set("x-ani-tenant-id", tenant)
	}
	return md, nil
}

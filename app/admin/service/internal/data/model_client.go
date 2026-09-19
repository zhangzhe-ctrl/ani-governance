package data

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	modelv1 "github.com/zhangzhe-ctrl/ani-model-service/api/model/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type ModelClientConfig struct {
	Address, CAFile, CertFile, KeyFile string
	Timeout                            time.Duration
}
type ModelClient struct {
	client          modelv1.ModelServiceClient
	timeout         time.Duration
	connectionState func() connectivity.State
}

// NewModelClient constructs a fail-closed mTLS client; server identity is fixed.
func NewModelClient(c ModelClientConfig) (*ModelClient, func(), error) {
	if c.Address == "" || c.Timeout <= 0 {
		return nil, nil, fmt.Errorf("model address and positive timeout are required")
	}
	pem, err := os.ReadFile(c.CAFile)
	if err != nil {
		return nil, nil, fmt.Errorf("read model CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, nil, fmt.Errorf("model CA contains no certificates")
	}
	cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("load governance client certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, nil, err
	}
	trustedName := false
	for _, name := range leaf.DNSNames {
		if name == "ani-governance" {
			trustedName = true
		}
	}
	if !trustedName {
		return nil, nil, fmt.Errorf("governance certificate requires exact DNS SAN ani-governance")
	}
	// This fixed internal contract has no DNS TXT service-config authority.
	conn, err := grpc.NewClient(c.Address, grpc.WithDisableServiceConfig(), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{cert}, ServerName: "ani-model-service",
	})))
	if err != nil {
		return nil, nil, err
	}
	return &ModelClient{client: modelv1.NewModelServiceClient(conn), timeout: c.Timeout, connectionState: conn.GetState}, func() { _ = conn.Close() }, nil
}

func ModelConfigFromEnv() (ModelClientConfig, error) {
	timeout := 3 * time.Second
	if v := os.Getenv("ANI_MODEL_TIMEOUT"); v != "" {
		var err error
		timeout, err = time.ParseDuration(v)
		if err != nil {
			return ModelClientConfig{}, err
		}
	}
	return ModelClientConfig{Address: os.Getenv("ANI_MODEL_ADDR"), CAFile: os.Getenv("ANI_MODEL_CA"), CertFile: os.Getenv("ANI_MODEL_CERT"), KeyFile: os.Getenv("ANI_MODEL_KEY"), Timeout: timeout}, nil
}

func (c *ModelClient) ListModels(ctx context.Context, tenant string, user uint32, limit uint32, state string) (*modelv1.ListModelsResponse, error) {
	id, err := uuid.Parse(tenant)
	if err != nil || id == uuid.Nil || id.String() != tenant || user == 0 {
		return nil, fmt.Errorf("invalid trusted model identity")
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	// Rebuild metadata; never append inbound/public identity headers.
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("x-ani-tenant-id", tenant, "x-ani-actor", "governance:user:"+strconv.FormatUint(uint64(user), 10), "x-ani-request-id", uuid.NewString()))
	reply, err := c.client.ListModels(ctx, &modelv1.ListModelsRequest{TenantId: tenant, Status: state, Page: &modelv1.CursorPageRequest{Limit: int32(limit)}})
	// A disconnected transport can spend the entire deadline reconnecting.
	// Report that dependency outage as 503; a connected, slow RPC remains 504.
	if status.Code(err) == codes.DeadlineExceeded && c.connectionState != nil && c.connectionState() != connectivity.Ready {
		return nil, status.Errorf(codes.Unavailable, "model transport unavailable: %v", err)
	}
	return reply, err
}

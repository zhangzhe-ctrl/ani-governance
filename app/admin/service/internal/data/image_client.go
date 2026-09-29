package data

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	imagev1 "github.com/zhangzhe-ctrl/ani-resource-service/api/image/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type ImageClientConfig struct {
	Address, CAFile, CertFile, KeyFile string
	Timeout                            time.Duration
}
type ImageClient struct {
	client          imagev1.TenantImageServiceClient
	timeout         time.Duration
	connectionState func() connectivity.State
}

func NewImageClient(c ImageClientConfig) (*ImageClient, func(), error) {
	invalid := func() (*ImageClient, func(), error) {
		return nil, nil, fmt.Errorf("invalid Image mTLS client configuration")
	}
	if c.Address == "" || c.Timeout <= 0 || c.Timeout > 45*time.Second {
		return invalid()
	}
	pem, err := os.ReadFile(c.CAFile)
	if err != nil {
		return invalid()
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return invalid()
	}
	cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if err != nil {
		return invalid()
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil || !imageExactSAN(leaf, "ani-governance") {
		return invalid()
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{cert}, ServerName: "ani-network-service", VerifyConnection: func(cs tls.ConnectionState) error {
		if len(cs.VerifiedChains) == 0 || len(cs.PeerCertificates) == 0 || !imageExactSAN(cs.PeerCertificates[0], "ani-network-service") {
			return fmt.Errorf("Image server identity rejected")
		}
		return nil
	}}
	conn, err := grpc.NewClient(c.Address, grpc.WithDisableServiceConfig(), grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	if err != nil {
		return invalid()
	}
	return &ImageClient{client: imagev1.NewTenantImageServiceClient(conn), timeout: c.Timeout, connectionState: conn.GetState}, func() { _ = conn.Close() }, nil
}
func imageExactSAN(c *x509.Certificate, name string) bool {
	for _, dns := range c.DNSNames {
		if dns == name {
			return true
		}
	}
	return false
}
func ImageConfigFromEnv() (ImageClientConfig, error) {
	timeout := 30 * time.Second
	if raw := os.Getenv("ANI_IMAGE_TIMEOUT"); raw != "" {
		var err error
		timeout, err = time.ParseDuration(raw)
		if err != nil || timeout <= 0 || timeout > 45*time.Second {
			return ImageClientConfig{}, fmt.Errorf("invalid Image client timeout")
		}
	}
	return ImageClientConfig{Address: os.Getenv("ANI_IMAGE_ADDR"), CAFile: os.Getenv("ANI_IMAGE_CA"), CertFile: os.Getenv("ANI_IMAGE_CERT"), KeyFile: os.Getenv("ANI_IMAGE_KEY"), Timeout: timeout}, nil
}
func validImageIdentity(tenant, actor string) bool {
	id, err := uuid.Parse(tenant)
	if err != nil || id == uuid.Nil || id.String() != tenant {
		return false
	}
	for _, prefix := range []string{"governance:user:", "governance:access-key:"} {
		if strings.HasPrefix(actor, prefix) {
			id, err := strconv.ParseUint(strings.TrimPrefix(actor, prefix), 10, 32)
			return err == nil && id > 0 && actor == prefix+strconv.FormatUint(id, 10)
		}
	}
	return false
}
func trustedImageCall[T any](c *ImageClient, ctx context.Context, tenant, actor string, call func(context.Context) (T, error)) (T, error) {
	var zero T
	if c == nil || c.client == nil {
		return zero, status.Error(codes.Unavailable, "Image client unavailable")
	}
	if !validImageIdentity(tenant, actor) {
		return zero, status.Error(codes.Unauthenticated, "invalid trusted Image identity")
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	// Replaces all pre-existing outbound metadata, including public identity,
	// bearer tokens and operator hints. Only verified principal facts cross hops.
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("x-ani-tenant-id", tenant, "x-ani-actor", actor, "x-ani-request-id", uuid.NewString()))
	reply, err := call(ctx)
	if status.Code(err) == codes.DeadlineExceeded && c.connectionState != nil && c.connectionState() != connectivity.Ready {
		return zero, status.Error(codes.Unavailable, "Image transport unavailable")
	}
	return reply, err
}
func imageRequestRequired() error {
	return status.Error(codes.InvalidArgument, "Image request required")
}
func (c *ImageClient) EnsureImageSpace(ctx context.Context, tenant, actor string, r *imagev1.EnsureImageSpaceRequest) (*imagev1.EnsureImageSpaceResponse, error) {
	if r == nil {
		return nil, imageRequestRequired()
	}
	req := proto.Clone(r).(*imagev1.EnsureImageSpaceRequest)
	req.TenantId = tenant
	return trustedImageCall(c, ctx, tenant, actor, func(ctx context.Context) (*imagev1.EnsureImageSpaceResponse, error) {
		return c.client.EnsureImageSpace(ctx, req)
	})
}
func (c *ImageClient) GetImageSpace(ctx context.Context, tenant, actor string, r *imagev1.GetImageSpaceRequest) (*imagev1.GetImageSpaceResponse, error) {
	if r == nil {
		return nil, imageRequestRequired()
	}
	req := proto.Clone(r).(*imagev1.GetImageSpaceRequest)
	req.TenantId = tenant
	return trustedImageCall(c, ctx, tenant, actor, func(ctx context.Context) (*imagev1.GetImageSpaceResponse, error) {
		return c.client.GetImageSpace(ctx, req)
	})
}
func (c *ImageClient) GetPublisherCredential(ctx context.Context, tenant, actor string, r *imagev1.GetPublisherCredentialRequest) (*imagev1.GetPublisherCredentialResponse, error) {
	if r == nil {
		return nil, imageRequestRequired()
	}
	req := proto.Clone(r).(*imagev1.GetPublisherCredentialRequest)
	req.TenantId = tenant
	return trustedImageCall(c, ctx, tenant, actor, func(ctx context.Context) (*imagev1.GetPublisherCredentialResponse, error) {
		return c.client.GetPublisherCredential(ctx, req)
	})
}
func (c *ImageClient) IssuePublisherCredential(ctx context.Context, tenant, actor string, r *imagev1.IssuePublisherCredentialRequest) (*imagev1.IssuePublisherCredentialResponse, error) {
	if r == nil {
		return nil, imageRequestRequired()
	}
	req := proto.Clone(r).(*imagev1.IssuePublisherCredentialRequest)
	req.TenantId = tenant
	return trustedImageCall(c, ctx, tenant, actor, func(ctx context.Context) (*imagev1.IssuePublisherCredentialResponse, error) {
		return c.client.IssuePublisherCredential(ctx, req)
	})
}
func (c *ImageClient) ResetPublisherCredential(ctx context.Context, tenant, actor string, r *imagev1.ResetPublisherCredentialRequest) (*imagev1.ResetPublisherCredentialResponse, error) {
	if r == nil {
		return nil, imageRequestRequired()
	}
	req := proto.Clone(r).(*imagev1.ResetPublisherCredentialRequest)
	req.TenantId = tenant
	return trustedImageCall(c, ctx, tenant, actor, func(ctx context.Context) (*imagev1.ResetPublisherCredentialResponse, error) {
		return c.client.ResetPublisherCredential(ctx, req)
	})
}
func (c *ImageClient) DisablePublisherCredential(ctx context.Context, tenant, actor string, r *imagev1.DisablePublisherCredentialRequest) (*imagev1.DisablePublisherCredentialResponse, error) {
	if r == nil {
		return nil, imageRequestRequired()
	}
	req := proto.Clone(r).(*imagev1.DisablePublisherCredentialRequest)
	req.TenantId = tenant
	return trustedImageCall(c, ctx, tenant, actor, func(ctx context.Context) (*imagev1.DisablePublisherCredentialResponse, error) {
		return c.client.DisablePublisherCredential(ctx, req)
	})
}
func (c *ImageClient) RegisterImage(ctx context.Context, tenant, actor string, r *imagev1.RegisterImageRequest) (*imagev1.RegisterImageResponse, error) {
	if r == nil {
		return nil, imageRequestRequired()
	}
	req := proto.Clone(r).(*imagev1.RegisterImageRequest)
	req.TenantId = tenant
	return trustedImageCall(c, ctx, tenant, actor, func(ctx context.Context) (*imagev1.RegisterImageResponse, error) {
		return c.client.RegisterImage(ctx, req)
	})
}
func (c *ImageClient) GetImage(ctx context.Context, tenant, actor string, r *imagev1.GetImageRequest) (*imagev1.GetImageResponse, error) {
	if r == nil {
		return nil, imageRequestRequired()
	}
	req := proto.Clone(r).(*imagev1.GetImageRequest)
	req.TenantId = tenant
	return trustedImageCall(c, ctx, tenant, actor, func(ctx context.Context) (*imagev1.GetImageResponse, error) { return c.client.GetImage(ctx, req) })
}
func (c *ImageClient) ListImages(ctx context.Context, tenant, actor string, r *imagev1.ListImagesRequest) (*imagev1.ListImagesResponse, error) {
	if r == nil {
		return nil, imageRequestRequired()
	}
	req := proto.Clone(r).(*imagev1.ListImagesRequest)
	req.TenantId = tenant
	return trustedImageCall(c, ctx, tenant, actor, func(ctx context.Context) (*imagev1.ListImagesResponse, error) { return c.client.ListImages(ctx, req) })
}
func (c *ImageClient) UpdateImage(ctx context.Context, tenant, actor string, r *imagev1.UpdateImageRequest) (*imagev1.UpdateImageResponse, error) {
	if r == nil {
		return nil, imageRequestRequired()
	}
	req := proto.Clone(r).(*imagev1.UpdateImageRequest)
	req.TenantId = tenant
	return trustedImageCall(c, ctx, tenant, actor, func(ctx context.Context) (*imagev1.UpdateImageResponse, error) { return c.client.UpdateImage(ctx, req) })
}
func (c *ImageClient) UnregisterImage(ctx context.Context, tenant, actor string, r *imagev1.UnregisterImageRequest) (*imagev1.UnregisterImageResponse, error) {
	if r == nil {
		return nil, imageRequestRequired()
	}
	req := proto.Clone(r).(*imagev1.UnregisterImageRequest)
	req.TenantId = tenant
	return trustedImageCall(c, ctx, tenant, actor, func(ctx context.Context) (*imagev1.UnregisterImageResponse, error) {
		return c.client.UnregisterImage(ctx, req)
	})
}

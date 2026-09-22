//go:build quota_lab

package simulator

// gRPC 服务与 mTLS（计划 §11.3）：只接受精确客户端身份 ani-governance。
// 协议字段/身份不合法时不执行资源变更。

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	quotalabpb "go-wind-admin/api/gen/go/quota_lab/service/v1"
)

// ServerConfig 模拟器 gRPC 服务配置。
type ServerConfig struct {
	Address  string
	CAFile   string
	CertFile string
	KeyFile  string
}

// GRPCServer GpuSimulatorService 的 mTLS gRPC server。
type GRPCServer struct {
	quotalabpb.UnimplementedGpuSimulatorServiceServer

	grpc *grpc.Server
	lis  net.Listener
	sim  *Simulator
}

// NewGRPCServer 构造 mTLS gRPC server；证书 SAN 必须精确为 ani-gpu-simulator，
// 只接受客户端精确 SAN ani-governance。
func NewGRPCServer(cfg ServerConfig, sim *Simulator) (*GRPCServer, error) {
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load simulator certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, err
	}
	trusted := false
	for _, name := range leaf.DNSNames {
		if name == "ani-gpu-simulator" {
			trusted = true
		}
	}
	if !trusted {
		return nil, fmt.Errorf("simulator certificate requires exact DNS SAN ani-gpu-simulator")
	}
	caPEM, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read simulator CA: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("simulator CA contains no certificates")
	}
	lis, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("listen simulator address: %w", err)
	}
	s := &GRPCServer{sim: sim, lis: lis}
	s.grpc = grpc.NewServer(
		grpc.Creds(credentials.NewTLS(&tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{cert},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    caPool,
		})),
		grpc.ChainUnaryInterceptor(s.authInterceptor),
	)
	quotalabpb.RegisterGpuSimulatorServiceServer(s.grpc, s)
	return s, nil
}

// requireGovernanceSAN 校验客户端证书精确 SAN。
func (s *GRPCServer) authInterceptor(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	p, ok := peer.FromContext(ctx)
	if !ok || p == nil {
		return nil, status.Error(codes.Unauthenticated, "SIMULATOR_AUTH: missing peer")
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.VerifiedChains) == 0 {
		return nil, status.Error(codes.Unauthenticated, "SIMULATOR_AUTH: client certificate required")
	}
	leaf := tlsInfo.State.VerifiedChains[0][0]
	name := ""
	for _, n := range leaf.DNSNames {
		if n == "ani-governance" {
			name = n
		}
	}
	if name != "ani-governance" || len(leaf.DNSNames) != 1 {
		return nil, status.Error(codes.Unauthenticated, "SIMULATOR_AUTH: only ani-governance is accepted")
	}
	return handler(ctx, req)
}

func requireUUID(field, v string) error {
	if _, err := uuid.Parse(v); err != nil {
		return status.Errorf(codes.InvalidArgument, "SIMULATOR_INVALID: %s must be a UUID", field)
	}
	return nil
}

// AcceptCreate 实现下游创建命令。
func (s *GRPCServer) AcceptCreate(ctx context.Context, req *quotalabpb.AcceptCreateRequest) (*quotalabpb.AcceptCreateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "SIMULATOR_INVALID: empty request")
	}
	for field, v := range map[string]string{
		"operation_id": req.GetOperationId(),
		"resource_id":  req.GetResourceId(),
		"tenant_id":    req.GetTenantId(),
		"charge_id":    req.GetChargeId(),
		"request_hash": "",
	} {
		if field == "request_hash" {
			if v == "" && req.GetRequestHash() == "" {
				return nil, status.Error(codes.InvalidArgument, "SIMULATOR_INVALID: request_hash required")
			}
			continue
		}
		if err := requireUUID(field, v); err != nil {
			return nil, err
		}
	}
	if req.GetActor() == "" {
		return nil, status.Error(codes.InvalidArgument, "SIMULATOR_INVALID: actor required")
	}
	// quota_code 必须 gpu.count；charged_units 必须等于 gpu_count（§11.3）。
	if req.GetQuotaCode() != "gpu.count" {
		return nil, status.Error(codes.FailedPrecondition, "SIMULATOR_CONFLICT: quota_code must be gpu.count")
	}
	if req.GetChargedUnits() != int64(req.GetGpuCount()) {
		return nil, status.Error(codes.FailedPrecondition, "SIMULATOR_CONFLICT: charged_units must equal gpu_count")
	}
	if req.GetGpuCount() < 1 || req.GetGpuCount() > 16 {
		return nil, status.Error(codes.InvalidArgument, "SIMULATOR_INVALID: gpu_count must be 1..16")
	}

	accepted, err := s.sim.AcceptCreate(ctx, &AcceptCreateInput{
		OperationID: req.GetOperationId(),
		ResourceID:  req.GetResourceId(),
		TenantID:    req.GetTenantId(),
		Actor:       req.GetActor(),
		RequestHash: req.GetRequestHash(),
		Name:        req.GetName(),
		GpuCount:    req.GetGpuCount(),
		ChargeID:    req.GetChargeId(),
	})
	if err != nil {
		return nil, mapSimError(err)
	}
	return &quotalabpb.AcceptCreateResponse{
		OperationId: req.GetOperationId(),
		ResourceId:  req.GetResourceId(),
		Accepted:    accepted,
	}, nil
}

// AcceptDelete 实现下游删除命令。
func (s *GRPCServer) AcceptDelete(ctx context.Context, req *quotalabpb.AcceptDeleteRequest) (*quotalabpb.AcceptDeleteResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "SIMULATOR_INVALID: empty request")
	}
	for field, v := range map[string]string{
		"operation_id":        req.GetOperationId(),
		"create_operation_id": req.GetCreateOperationId(),
		"resource_id_uuid":    "",
		"tenant_id":           req.GetTenantId(),
		"charge_id":           req.GetChargeId(),
	} {
		if v == "" {
			continue
		}
		if field == "resource_id_uuid" {
			if err := requireUUID("resource_id", req.GetResourceId()); err != nil {
				return nil, err
			}
			continue
		}
		if err := requireUUID(field, v); err != nil {
			return nil, err
		}
	}
	if req.GetActor() == "" || req.GetRequestHash() == "" {
		return nil, status.Error(codes.InvalidArgument, "SIMULATOR_INVALID: actor and request_hash required")
	}
	accepted, err := s.sim.AcceptDelete(ctx, &AcceptDeleteInput{
		OperationID:       req.GetOperationId(),
		CreateOperationID: req.GetCreateOperationId(),
		ResourceID:        req.GetResourceId(),
		TenantID:          req.GetTenantId(),
		Actor:             req.GetActor(),
		RequestHash:       req.GetRequestHash(),
		ChargeID:          req.GetChargeId(),
	})
	if err != nil {
		return nil, mapSimError(err)
	}
	return &quotalabpb.AcceptDeleteResponse{
		OperationId: req.GetOperationId(),
		ResourceId:  req.GetResourceId(),
		Accepted:    accepted,
	}, nil
}

// GetResource 纯读取。
func (s *GRPCServer) GetResource(ctx context.Context, req *quotalabpb.GetResourceRequest) (*quotalabpb.GetResourceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "SIMULATOR_INVALID: empty request")
	}
	if err := requireUUID("tenant_id", req.GetTenantId()); err != nil {
		return nil, err
	}
	view, err := s.sim.GetResource(ctx, req.GetTenantId(), req.GetResourceId())
	if err != nil {
		return nil, mapSimError(err)
	}
	return &quotalabpb.GetResourceResponse{
		ResourceId: view.ResourceID,
		TenantId:   view.TenantID,
		Name:       view.Name,
		GpuCount:   int32(view.UnitCount),
		Status:     view.Status,
		UnitCount:  int32(view.UnitCount),
	}, nil
}

func mapSimError(err error) error {
	switch {
	case err == nil:
		return nil
	case err == ErrPermanentConflict || err == ErrCapacity:
		return status.Error(codes.FailedPrecondition, err.Error())
	case err == ErrNotFound:
		return status.Error(codes.NotFound, err.Error())
	case err == ErrClosed:
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return status.Error(codes.Unavailable, err.Error())
	}
}

// Start 启动 gRPC listener。
func (s *GRPCServer) Start() error {
	go func() { _ = s.grpc.Serve(s.lis) }()
	return nil
}

// Stop 优雅停止。
func (s *GRPCServer) Stop() {
	stopped := make(chan struct{})
	go func() { s.grpc.GracefulStop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		s.grpc.Stop()
	}
}

// Addr 返回 gRPC 监听地址。
func (s *GRPCServer) Addr() string { return s.lis.Addr().String() }

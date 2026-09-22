package server

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	quotapb "go-wind-admin/api/gen/go/quota/service/v1"

	"go-wind-admin/app/admin/service/internal/data"
)

// QuotaInternalServerConfig 内部退额 listener 配置（§9.3）。
// 默认 disabled；enabled 时地址/证书/owner 映射缺失或不合法，启动报错，不降级明文。
type QuotaInternalServerConfig struct {
	Enabled  bool
	Address  string
	CAFile   string
	CertFile string
	KeyFile  string
	// CertOwnerMap 将客户端证书的精确 DNS SAN 映射到已注册 owner_service。
	// 仅 quota_lab 构建注册模拟 owner ani-gpu-simulator；正式构建不允许
	// 通过配置一个字符串加载模拟身份。
	CertOwnerMap map[string]string
}

// QuotaInternalConfigFromEnv 从环境变量解析：
// ANI_QUOTA_ENABLED / ANI_QUOTA_INTERNAL_ADDR / ANI_QUOTA_CA_FILE /
// ANI_QUOTA_CERT_FILE / ANI_QUOTA_KEY_FILE。
func QuotaInternalConfigFromEnv() QuotaInternalServerConfig {
	return QuotaInternalServerConfig{
		Enabled:  os.Getenv("ANI_QUOTA_ENABLED") == "true",
		Address:  os.Getenv("ANI_QUOTA_INTERNAL_ADDR"),
		CAFile:   os.Getenv("ANI_QUOTA_CA_FILE"),
		CertFile: os.Getenv("ANI_QUOTA_CERT_FILE"),
		KeyFile:  os.Getenv("ANI_QUOTA_KEY_FILE"),
	}
}

// Validate enabled 配置完整性；fail-closed，不降级明文。
func (c *QuotaInternalServerConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.Address == "" || c.CAFile == "" || c.CertFile == "" || c.KeyFile == "" {
		return fmt.Errorf("quota internal server requires ANI_QUOTA_INTERNAL_ADDR, ANI_QUOTA_CA_FILE, ANI_QUOTA_CERT_FILE and ANI_QUOTA_KEY_FILE when enabled")
	}
	if len(c.CertOwnerMap) == 0 {
		return fmt.Errorf("quota internal server requires at least one registered owner mapping")
	}
	return nil
}

// QuotaInternalServer 独立内部 mTLS gRPC listener（QUOTA-03）。
// TLS 最低 1.3，RequireAndVerifyClientCert；验证链后必须匹配精确 DNS SAN
// 与已注册 owner。不接受 X-Forwarded-*、owner header、Bearer 或 tenant 参数
// 替代证书身份。
type QuotaInternalServer struct {
	quotapb.UnimplementedQuotaReleaseServiceServer

	grpcServer *grpc.Server
	listener   net.Listener
	ledger     *data.QuotaLedgerRepo
	owners     map[string]string // 精确 SAN -> owner_service
}

// NewQuotaInternalServer 构造；disabled 返回 (nil, nil)。
func NewQuotaInternalServer(cfg QuotaInternalServerConfig, ledger *data.QuotaLedgerRepo) (*QuotaInternalServer, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// 服务端自己的证书（SAN 必须是 ani-governance，供下游校验）。
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load quota internal server certificate: %w", err)
	}
	caPEM, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read quota internal CA: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("quota internal CA contains no certificates")
	}
	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
	}

	lis, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("listen quota internal address: %w", err)
	}

	srv := &QuotaInternalServer{
		ledger:   ledger,
		owners:   cfg.CertOwnerMap,
		listener: lis,
	}
	srv.grpcServer = grpc.NewServer(
		grpc.Creds(credentials.NewTLS(tlsCfg)),
		grpc.ChainUnaryInterceptor(srv.authInterceptor),
	)
	quotapb.RegisterQuotaReleaseServiceServer(srv.grpcServer, srv)
	return srv, nil
}

// AuthInterceptor 从客户端证书提取精确 DNS SAN 并映射到已注册 owner；
// 无证书/未知 SAN/多 SAN 混用一律拒绝（§9.3）。
func (s *QuotaInternalServer) authInterceptor(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	p, ok := peer.FromContext(ctx)
	if !ok || p == nil {
		return nil, status.Error(codes.Unauthenticated, "QUOTA_RELEASE_DENIED: missing peer")
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.VerifiedChains) == 0 {
		return nil, status.Error(codes.Unauthenticated, "QUOTA_RELEASE_DENIED: client certificate required")
	}
	leaf := tlsInfo.State.VerifiedChains[0][0]
	// 同一 SAN 集内必须恰好命中一个已注册 owner；同 CA 的另一服务不能退他人额度。
	ownerService := ""
	for _, name := range leaf.DNSNames {
		if mapped, registered := s.owners[name]; registered {
			if ownerService != "" && ownerService != mapped {
				return nil, status.Error(codes.Unauthenticated, "QUOTA_RELEASE_DENIED: ambiguous certificate identity")
			}
			ownerService = mapped
		}
	}
	if ownerService == "" {
		return nil, status.Error(codes.Unauthenticated, "QUOTA_RELEASE_DENIED: unknown client identity")
	}
	return handler(withQuotaOwner(ctx, ownerService), req)
}

type quotaOwnerKey struct{}

func withQuotaOwner(ctx context.Context, owner string) context.Context {
	return context.WithValue(ctx, quotaOwnerKey{}, owner)
}

func quotaOwnerFromContext(ctx context.Context) (string, error) {
	owner, ok := ctx.Value(quotaOwnerKey{}).(string)
	if !ok || owner == "" {
		return "", status.Error(codes.Unauthenticated, "QUOTA_RELEASE_DENIED: missing verified owner")
	}
	return owner, nil
}

// ReportQuotaRelease 实现 QUOTA-03：请求不含 tenant_id/owner_service，
// 租户/owner 从 charge 与已验证证书取出（§9.1）。
func (s *QuotaInternalServer) ReportQuotaRelease(ctx context.Context, req *quotapb.ReportQuotaReleaseRequest) (*quotapb.ReportQuotaReleaseResponse, error) {
	owner, err := quotaOwnerFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "INVALID_QUOTA_REQUEST: empty request")
	}
	if _, perr := uuid.Parse(req.GetReleaseEventId()); perr != nil {
		return nil, status.Error(codes.InvalidArgument, "INVALID_QUOTA_REQUEST: release_event_id must be a UUID")
	}
	if _, perr := uuid.Parse(req.GetOperationId()); perr != nil {
		return nil, status.Error(codes.InvalidArgument, "INVALID_QUOTA_REQUEST: operation_id must be a UUID")
	}
	items := req.GetItems()
	if len(items) == 0 {
		return nil, status.Error(codes.InvalidArgument, "INVALID_QUOTA_REQUEST: items must not be empty")
	}
	if len(items) > 16 {
		return nil, status.Error(codes.InvalidArgument, "INVALID_QUOTA_REQUEST: at most 16 items")
	}
	switch req.GetReason() {
	case quotapb.ReleaseReason_ABORTED_CLEANED, quotapb.ReleaseReason_RESOURCE_RELEASED:
	default:
		return nil, status.Error(codes.InvalidArgument, "INVALID_QUOTA_REQUEST: reason must be ABORTED_CLEANED or RESOURCE_RELEASED")
	}
	if len(req.GetResourceRefs()) > 64 {
		return nil, status.Error(codes.InvalidArgument, "INVALID_QUOTA_REQUEST: at most 64 resource_refs")
	}

	// 同 charge 重复 item 拒绝；协议内部数量用整数。
	seen := make(map[string]struct{}, len(items))
	releaseItems := make([]data.QuotaReleaseItemInput, 0, len(items))
	for _, it := range items {
		if _, perr := uuid.Parse(it.GetChargeId()); perr != nil {
			return nil, status.Error(codes.InvalidArgument, "INVALID_QUOTA_REQUEST: charge_id must be a UUID")
		}
		if it.GetQuotaCode() == "" {
			return nil, status.Error(codes.InvalidArgument, "INVALID_QUOTA_REQUEST: quota_code is required")
		}
		if _, dup := seen[it.GetChargeId()]; dup {
			return nil, status.Error(codes.InvalidArgument, "INVALID_QUOTA_REQUEST: duplicate charge item")
		}
		seen[it.GetChargeId()] = struct{}{}
		releaseItems = append(releaseItems, data.QuotaReleaseItemInput{
			ChargeID:      it.GetChargeId(),
			QuotaCode:     it.GetQuotaCode(),
			ReleasedTotal: it.GetReleasedTotal(),
		})
	}

	payloadHash := quotaReleasePayloadHash(req.GetOperationId(), req.GetReason().String(), items, req.GetResourceRefs())

	results, err := s.ledger.Release(ctx, &data.QuotaReleaseInput{
		OwnerService:   owner,
		ReleaseEventID: req.GetReleaseEventId(),
		OperationID:    req.GetOperationId(),
		Reason:         req.GetReason().String(),
		PayloadHash:    payloadHash,
		PayloadJSON:    quotaReleasePayloadJSON(req, owner),
		Items:          releaseItems,
	})
	if err != nil {
		return nil, err // gRPC status 错误原样透传
	}

	out := make([]*quotapb.QuotaReleaseResult, 0, len(results))
	for _, r := range results {
		out = append(out, &quotapb.QuotaReleaseResult{
			ChargeId:      r.ChargeID,
			AppliedDelta:  r.AppliedDelta,
			ReleasedTotal: r.ReleasedTotal,
		})
	}
	return &quotapb.ReportQuotaReleaseResponse{Items: out}, nil
}

// quotaReleasePayloadHash 基于固定结构、排序后的唯一 items/resource_refs 计算；
// 排除传输 request-id，不把 JSON 字段顺序视为不同业务内容（§9.2）。
func quotaReleasePayloadHash(operationID, reason string, items []*quotapb.QuotaReleaseItem, refs []*quotapb.QuotaResourceRef) string {
	lines := make([]string, 0, len(items)+len(refs)+2)
	lines = append(lines, "v1", operationID, reason)
	sorted := make([]*quotapb.QuotaReleaseItem, len(items))
	copy(sorted, items)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].GetChargeId() != sorted[j].GetChargeId() {
			return sorted[i].GetChargeId() < sorted[j].GetChargeId()
		}
		return sorted[i].GetQuotaCode() < sorted[j].GetQuotaCode()
	})
	seenItem := make(map[string]struct{}, len(sorted))
	for _, it := range sorted {
		line := fmt.Sprintf("item:%s:%s:%d", it.GetChargeId(), it.GetQuotaCode(), it.GetReleasedTotal())
		key := line
		if _, dup := seenItem[key]; dup {
			continue
		}
		seenItem[key] = struct{}{}
		lines = append(lines, line)
	}
	refLines := make([]string, 0, len(refs))
	for _, r := range refs {
		refLines = append(refLines, "ref:"+r.GetResourceId())
	}
	sort.Strings(refLines)
	lines = append(lines, refLines...)

	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// quotaReleasePayloadJSON 保存整个已验证批次的原文（含 owner，供审计）。
func quotaReleasePayloadJSON(req *quotapb.ReportQuotaReleaseRequest, owner string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `{"schema_version":1,"owner_service":%q,"release_event_id":%q,"operation_id":%q,"reason":%q,"items":[`,
		owner, req.GetReleaseEventId(), req.GetOperationId(), req.GetReason().String())
	for i, it := range req.GetItems() {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"charge_id":%q,"quota_code":%q,"released_total":%d}`,
			it.GetChargeId(), it.GetQuotaCode(), it.GetReleasedTotal())
	}
	b.WriteString("]")
	if refs := req.GetResourceRefs(); len(refs) > 0 {
		b.WriteString(`,"resource_refs":[`)
		for i, r := range refs {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, "%q", r.GetResourceId())
		}
		b.WriteString("]")
	}
	b.WriteString("}")
	return b.String()
}

// Start 实现 transport.Server 生命周期。
func (s *QuotaInternalServer) Start(_ context.Context) error {
	go func() {
		if err := s.grpcServer.Serve(s.listener); err != nil {
			_ = err // Stop 时正常返回错误；记录交给调用方日志
		}
	}()
	return nil
}

// Stop 优雅停止。
func (s *QuotaInternalServer) Stop(_ context.Context) error {
	stopped := make(chan struct{})
	go func() {
		s.grpcServer.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		s.grpcServer.Stop()
	}
	return nil
}

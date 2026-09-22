//go:build quota_lab

// Package quotalab 仅在 quota_lab 构建中存在（计划 §11.1）：
// GPU 模拟闭环的实验入口、下游适配器与故障控制面。
// 正式构建不导入本包、不注册实验路由/adapter。
package quotalab

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"github.com/google/uuid"
	"github.com/go-kratos/kratos/v2/transport"
	khttp "github.com/go-kratos/kratos/v2/transport/http"

	quotalabpb "go-wind-admin/api/gen/go/quota_lab/service/v1"

	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/service"
	"go-wind-admin/pkg/middleware/auth"
)

// QuotaLabService 实现 QUOTA-LAB-01～04 的用户入口。
// 接受真实用户 JWT（中间件已完成认证/租户闸门/Casbin）；本服务负责
// 幂等键、规范请求、占额与转发登记。资源业务请求不接受 tenantId、
// actorId、ownerService、quotaCode、chargeId、故障参数（§10.2）。
type QuotaLabService struct {
	quotalabpb.QuotaLabServiceHTTPServer

	log      *bLogger.Helper
	ledger   *data.QuotaLedgerRepo
	registry *service.QuotaAdapterRegistry
	worker   *service.QuotaDispatchWorker
	resolver service.ResourceTenantResolver
}

func NewQuotaLabService(
	ctx *bootstrap.Context,
	ledger *data.QuotaLedgerRepo,
	registry *service.QuotaAdapterRegistry,
	worker *service.QuotaDispatchWorker,
	resolver service.ResourceTenantResolver,
) *QuotaLabService {
	return &QuotaLabService{
		log:      ctx.NewLoggerHelper("quota-lab/service/gpu"),
		ledger:   ledger,
		registry: registry,
		worker:   worker,
		resolver: resolver,
	}
}

const (
	labOwnerService = data.QuotaCodeOwnerLab
	labActionCreate = "LAB_GPU_CREATE"
	labActionDelete = "LAB_GPU_DELETE"
	labQuotaCode    = data.QuotaCodeGpuCount
)

// requireTenantUser 实验入口要求真实租户用户；平台 tenant=0 不代租户创建（AUTH-03）。
func requireTenantUser(ctx context.Context) (*auth.Principal, error) {
	p, err := auth.PrincipalFromContext(ctx)
	if err != nil || p == nil {
		return nil, err
	}
	if p.Type != auth.SubjectUser || p.TenantID == 0 || p.ID == 0 {
		return nil, data.QuotaErrNotFound("resource not found")
	}
	return p, nil
}

// idempotencyKeyFromHTTP 从请求头读取必需的 Idempotency-Key 并校验 UUID。
func idempotencyKeyFromHTTP(ctx context.Context) (string, error) {
	key := ""
	if tr, ok := transport.FromServerContext(ctx); ok {
		if ht, isHTTP := tr.(*khttp.Transport); isHTTP {
			key = strings.TrimSpace(ht.Request().Header.Get("Idempotency-Key"))
		}
	}
	if key == "" {
		return "", data.QuotaErrInvalid("Idempotency-Key header is required")
	}
	if _, err := uuid.Parse(key); err != nil {
		return "", data.QuotaErrInvalid("Idempotency-Key must be a UUID")
	}
	return key, nil
}

// canonicalHash 固定字段顺序序列化后哈希：可信 tenant/actor/action/owner +
// 校验后业务参数；不含 request-id、时间戳或新 UUID（§6.5）。
func canonicalHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func (s *QuotaLabService) CreateGpuAllocation(ctx context.Context, req *quotalabpb.CreateGpuAllocationRequest) (*quotalabpb.CreateGpuAllocationResponse, error) {
	p, err := requireTenantUser(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || req.Data == nil {
		return nil, data.QuotaErrInvalid("data is required")
	}
	name := strings.TrimSpace(req.Data.GetName())
	if name == "" || len(name) > 64 {
		return nil, data.QuotaErrInvalid("name must be 1-64 characters after trimming")
	}
	gpuCount := int64(req.Data.GetGpuCount())
	if gpuCount < 1 || gpuCount > 16 {
		return nil, data.QuotaErrInvalid("gpu_count must be between 1 and 16")
	}
	key, err := idempotencyKeyFromHTTP(ctx)
	if err != nil {
		return nil, err
	}
	// 适配器缺失或配置不全时，新操作在占额前拒绝（§8.2）。
	if !s.registry.HasAdapter(labOwnerService, labActionCreate) {
		return nil, data.QuotaErrAdapterUnavailable("gpu simulator adapter is not configured")
	}

	actor, err := p.Actor()
	if err != nil {
		return nil, err
	}

	// resource_id 由 Governance 随创建 operation 生成并持久化；重试不得重新生成
	// （重试路径由幂等命中返回原 operation 的 resource_id）。
	resourceTenantID, err := s.resolveResourceTenantID(ctx, p.TenantID)
	if err != nil {
		return nil, err
	}
	resourceID := uuid.NewString()
	requestHash := canonicalHash(
		"v1",
		fmt.Sprintf("tenant:%d", p.TenantID),
		actor,
		labActionCreate,
		labOwnerService,
		resourceID,
		resourceTenantID,
		name,
		strconv.FormatInt(gpuCount, 10),
	)
	canonical := fmt.Sprintf(`{"schema_version":1,"action":%q,"resource_id":%q,"resource_tenant_id":%q,"name":%q,"gpu_count":%d}`,
		labActionCreate, resourceID, resourceTenantID, name, gpuCount)

	res, err := s.ledger.Occupy(ctx, &data.QuotaOccupyInput{
		TenantID:         p.TenantID,
		ResourceTenantID: resourceTenantID,
		ResourceID:       resourceID,
		ActorType:        string(p.Type),
		ActorID:          strconv.FormatUint(uint64(p.ID), 10),
		OwnerService:     labOwnerService,
		Action:           labActionCreate,
		IdempotencyKey:   key,
		RequestHash:      requestHash,
		CanonicalRequest: canonical,
		Items:            []data.QuotaOccupyItem{{QuotaCode: labQuotaCode, Units: gpuCount}},
	})
	if err != nil {
		return nil, err
	}

	chargeID := ""
	if len(res.ChargeIDs) > 0 {
		chargeID = res.ChargeIDs[0]
	}

	// 提交成功后才能通知 worker 转发（§7.2.12）。
	s.worker.Notify()

	return &quotalabpb.CreateGpuAllocationResponse{
		OperationId: res.OperationID,
		ChargeId:    chargeID,
		ResourceId:  res.ResourceID,
	}, nil
}

// resolveResourceTenantID 解析持久 resource_tenant_id（复用现有租户仓库合同）。
func (s *QuotaLabService) resolveResourceTenantID(ctx context.Context, tenantId uint32) (string, error) {
	id, err := s.resolver.ResourceTenantID(ctx, tenantId)
	if err != nil {
		return "", err
	}
	return id, nil
}

// DeleteGpuAllocation 实现 QUOTA-LAB-03：登记删除操作，不立即退额。
// 按 tenant/resource/原 charge 关联校验归属；跨租户统一 404。
func (s *QuotaLabService) DeleteGpuAllocation(ctx context.Context, req *quotalabpb.DeleteGpuAllocationRequest) (*quotalabpb.DeleteGpuAllocationResponse, error) {
	p, err := requireTenantUser(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || req.GetResourceId() == "" {
		return nil, data.QuotaErrInvalid("resource_id is required")
	}
	key, err := idempotencyKeyFromHTTP(ctx)
	if err != nil {
		return nil, err
	}
	if !s.registry.HasAdapter(labOwnerService, labActionDelete) {
		return nil, data.QuotaErrAdapterUnavailable("gpu simulator adapter is not configured")
	}

	actor, err := p.Actor()
	if err != nil {
		return nil, err
	}
	// 归属校验：原创建操作必须属于本租户；找不到统一 404。
	createOp, _, ferr := s.ledger.FindCreateOperationByResource(ctx, p.TenantID, labOwnerService, req.GetResourceId())
	if ferr != nil {
		return nil, data.QuotaErrNotFound("resource not found")
	}

	requestHash := canonicalHash(
		"v1",
		fmt.Sprintf("tenant:%d", p.TenantID),
		actor,
		labActionDelete,
		labOwnerService,
		createOp.ResourceID,
		createOp.OperationID,
	)
	canonical := fmt.Sprintf(`{"schema_version":1,"action":%q,"resource_id":%q,"create_operation_id":%q}`,
		labActionDelete, createOp.ResourceID, createOp.OperationID)

	res, err := s.ledger.CreateDeleteOperation(ctx, &data.QuotaDeleteInput{
		TenantID:         p.TenantID,
		ActorType:        string(p.Type),
		ActorID:          strconv.FormatUint(uint64(p.ID), 10),
		OwnerService:     labOwnerService,
		Action:           labActionDelete,
		IdempotencyKey:   key,
		RequestHash:      requestHash,
		CanonicalRequest: canonical,
		ResourceID:       req.GetResourceId(),
	})
	if err != nil {
		return nil, err
	}

	s.worker.Notify()

	return &quotalabpb.DeleteGpuAllocationResponse{OperationId: res.OperationID}, nil
}

// GetGpuAllocation 实现 QUOTA-LAB-02：经 Governance 转发查询 simulator；
// 不触发占额和资源状态推进。跨租户/不存在统一 404。
func (s *QuotaLabService) GetGpuAllocation(ctx context.Context, req *quotalabpb.GetGpuAllocationRequest) (*quotalabpb.GetGpuAllocationResponse, error) {
	p, err := requireTenantUser(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || req.GetResourceId() == "" {
		return nil, data.QuotaErrInvalid("resource_id is required")
	}
	resourceTenantID, err := s.resolveResourceTenantID(ctx, p.TenantID)
	if err != nil {
		return nil, err
	}
	a := s.registry.Lookup(labOwnerService, labActionCreate)
	if a == nil {
		return nil, data.QuotaErrAdapterUnavailable("gpu simulator adapter is not configured")
	}
	gpuAdapter, ok := a.(GpuResourceReader)
	if !ok {
		return nil, data.QuotaErrAdapterUnavailable("gpu simulator read is not configured")
	}
	reply, err := gpuAdapter.GetResource(ctx, resourceTenantID, req.GetResourceId())
	if err != nil {
		return nil, data.QuotaErrNotFound("resource not found")
	}
	return &quotalabpb.GetGpuAllocationResponse{
		ResourceId:      reply.ResourceId,
		Name:            reply.Name,
		GpuCount:        int32(reply.UnitCount),
		SimulatorStatus: reply.Status,
		UnitCount:       int32(reply.UnitCount),
	}, nil
}

// GetQuotaOperation 实现 QUOTA-LAB-04：只返回本用户操作的投递状态与账本标识。
func (s *QuotaLabService) GetQuotaOperation(ctx context.Context, req *quotalabpb.GetQuotaOperationRequest) (*quotalabpb.GetQuotaOperationResponse, error) {
	p, err := requireTenantUser(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || req.GetOperationId() == "" {
		return nil, data.QuotaErrInvalid("operation_id is required")
	}
	op, oerr := s.ledger.GetOperationForUser(ctx, p.TenantID, req.GetOperationId())
	if oerr != nil {
		return nil, data.QuotaErrNotFound("operation not found")
	}
	chargeID := ""
	if ch, cerr := s.ledger.GetChargeForOperation(ctx, p.TenantID, op.OperationID); cerr == nil {
		chargeID = ch.ChargeID
	}
	createOpID := ""
	if op.CreateOperationID != nil {
		createOpID = *op.CreateOperationID
	}
	_ = createOpID
	return &quotalabpb.GetQuotaOperationResponse{
		OperationId:   op.OperationID,
		Action:        op.Action,
		DispatchState: op.DispatchState.String(),
		AttemptCount:  int32(op.AttemptCount),
		ResourceId:    op.ResourceID,
		ChargeId:      chargeID,
		OwnerService:  op.OwnerService,
	}, nil
}

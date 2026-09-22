package service

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go-wind-admin/app/admin/service/internal/data"
)

// QuotaDispatchAdapter 是 owner 服务命令投递的编译期注册适配器（§8.2）。
// owner/action 必须命中编译注册的 adapter；用户不能指定 URL 或服务名称，
// 不做通用 URL/方法自由转发。
type QuotaDispatchAdapter interface {
	// OwnerService 返回该适配器服务的稳定 owner 标识（如 ani-gpu-simulator）。
	OwnerService() string
	// Actions 返回支持的 action 集合（如 LAB_GPU_CREATE/LAB_GPU_DELETE）。
	Actions() []string
	// Dispatch 投递命令；实现必须使用固定 RPC 超时（本批 3 秒），
	// 并区分：持久接受（返回 ACK，nil）、永久合同错误、传输不确定三类结果。
	Dispatch(ctx context.Context, cmd *QuotaDispatchCommand) (ackJSON []byte, err error)
}

// QuotaChargeRef 命令携带的占额明细（与 data 层账本定义一致）。
type QuotaChargeRef = data.QuotaChargeRef

// QuotaDispatchCommand 投递命令：字段与 operation/charge 持久记录一致，
// 重试保持相同 ID 与报文（§6.6/§8.2）。
type QuotaDispatchCommand struct {
	OperationID      string
	ResourceID       string
	ResourceTenantID string
	Actor            string
	Action           string
	RequestHash      string
	CanonicalRequest []byte
	// CREATE 为空；DELETE 指向原创建操作（§6.2）。
	CreateOperationID string
	Charges           []QuotaChargeRef
}

// QuotaAdapterRegistry 编译期适配器注册表。构造不查询数据库、不启动 goroutine。
type QuotaAdapterRegistry struct {
	mu       sync.RWMutex
	byOwner  map[string]QuotaDispatchAdapter
	byAction map[string]QuotaDispatchAdapter
}

func NewQuotaAdapterRegistry() *QuotaAdapterRegistry {
	return &QuotaAdapterRegistry{
		byOwner:  make(map[string]QuotaDispatchAdapter),
		byAction: make(map[string]QuotaDispatchAdapter),
	}
}

// Register 注册 adapter；同一 action 重复注册视为装配错误。
func (r *QuotaAdapterRegistry) Register(a QuotaDispatchAdapter) error {
	if a == nil || a.OwnerService() == "" {
		return fmt.Errorf("quota adapter must have an owner service")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, action := range a.Actions() {
		if _, dup := r.byAction[action]; dup {
			return fmt.Errorf("quota adapter for action %s already registered", action)
		}
	}
	if _, dup := r.byOwner[a.OwnerService()]; dup {
		return fmt.Errorf("quota adapter for owner %s already registered", a.OwnerService())
	}
	r.byOwner[a.OwnerService()] = a
	for _, action := range a.Actions() {
		r.byAction[action] = a
	}
	return nil
}

// Lookup 按 owner+action 定位 adapter；未注册返回 nil（新操作在占额前拒绝，
// 恢复遇到缺失则保留账本报错，§8.2）。
func (r *QuotaAdapterRegistry) Lookup(ownerService, action string) QuotaDispatchAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.byOwner[ownerService]
	if !ok {
		return nil
	}
	for _, act := range a.Actions() {
		if act == action {
			return a
		}
	}
	return nil
}

// HasAdapter 是否存在可用适配器（占额前检查，§8.2/§7.2.8）。
func (r *QuotaAdapterRegistry) HasAdapter(ownerService, action string) bool {
	return r.Lookup(ownerService, action) != nil
}

// ── gRPC 错误分类辅助 ────────────────────────────────────────

// 常量别名避免 worker 文件直接依赖 grpc 包名膨胀（保持可读）。
const (
	codesInvalidArgument    = codes.InvalidArgument
	codesFailedPrecondition = codes.FailedPrecondition
	codesNotFound           = codes.NotFound
	codesPermissionDenied   = codes.PermissionDenied
	codesUnauthenticated    = codes.Unauthenticated
)

func grpcCodeIn(err error, allowed ...codes.Code) bool {
	return slices.Contains(allowed, status.Code(err))
}

func errorReason(err error) string {
	if st, ok := status.FromError(err); ok {
		if st.Message() != "" {
			return st.Code().String() + ":" + st.Message()
		}
		return st.Code().String()
	}
	return err.Error()
}

package viewer

import (
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"

	"github.com/tx7do/go-crud/viewer"
)

// UserViewer describes a user-viewer.
type UserViewer struct {
	uid         uint64
	tid         uint64
	ouid        uint64
	dataScopes  []viewer.DataScope
	roles       []string
	permissions []string
	traceID     string
}

func NewUserViewer(
	uid uint64,
	tid uint64,
	ouid uint64,
	traceID string,
	dataScopes []viewer.DataScope,
) viewer.Context {
	uv := UserViewer{
		uid:        uid,
		tid:        tid,
		ouid:       ouid,
		dataScopes: dataScopes,
		traceID:    traceID,
	}
	return uv
}

// UserID 返回当前用户ID
func (v UserViewer) UserID() uint64 {
	return v.uid
}

// TenantID 返回租户ID
func (v UserViewer) TenantID() uint64 {
	return v.tid
}

// OrgUnitID 返回当前身份挂载的组织单元 ID
func (v UserViewer) OrgUnitID() uint64 {
	return v.ouid
}

// Permissions 返回当前 Viewer 的权限列表（可用于细粒度判断）
func (v UserViewer) Permissions() []string {
	return v.permissions
}

// Roles 返回当前 Viewer 的角色列表（可选，用于审计或策略）
func (v UserViewer) Roles() []string {
	return v.roles
}

// DataScope 返回当前身份的数据权限范围（用于 SQL 拼接）
func (v UserViewer) DataScope() []viewer.DataScope {
	return v.dataScopes
}

// TraceID 返回当前请求的 Trace ID（用于日志跟踪）
func (v UserViewer) TraceID() string {
	return v.traceID
}

// HasPermission 判断是否具有某个动作/资源的权限（如 "update:user"）
func (v UserViewer) HasPermission(_, _ string) bool {
	return false
}

// IsPlatformContext 当前是否处于平台管理视图（tenant_id == 0）
func (v UserViewer) IsPlatformContext() bool {
	return v.tid == 0
}

// IsTenantContext 当前是否处于租户业务视图（tenant_id > 0）
func (v UserViewer) IsTenantContext() bool {
	return v.tid > 0
}

// IsSystemContext 判断是否为系统后台任务
func (v UserViewer) IsSystemContext() bool {
	return false
}

// ShouldAudit 返回是否需要记录审计日志（便于在中间件/Hook 中快速判断）
func (v UserViewer) ShouldAudit() bool {
	return false
}

// BuildDataScopes 把令牌承载的聚合数据范围转换为库层执行结构。
//
// 旧单值回退：过渡期令牌可能只带单值 ds 声明（无 dss），按单元素处理，
// 与旧 convertDataScope 行为对齐（UNIT 类目标集为空时由库规则按
// "无有效谓词" fail-closed 拒绝，而非放行）。
//
// UNSPECIFIED 一律剔除：空集不做兜底放行，交由库 rule.PermissionRule
// 以 "no data scope defined" 拒绝。
func BuildDataScopes(
	scopes []identityV1.DataScope,
	unitIDs []uint64,
	legacy identityV1.DataScope,
) []viewer.DataScope {
	if len(scopes) == 0 && legacy != identityV1.DataScope_DATA_SCOPE_UNSPECIFIED {
		scopes = []identityV1.DataScope{legacy}
	}

	result := make([]viewer.DataScope, 0, len(scopes))
	for _, s := range scopes {
		switch s {
		case identityV1.DataScope_ALL:
			result = append(result, viewer.DataScope{
				ScopeType: viewer.ScopeTypeAll,
			})

		case identityV1.DataScope_SELF:
			result = append(result, viewer.DataScope{
				ScopeType: viewer.ScopeTypeSelf,
			})

		case identityV1.DataScope_UNIT_ONLY,
			identityV1.DataScope_UNIT_AND_CHILD,
			identityV1.DataScope_SELECTED_UNITS:
			result = append(result, viewer.DataScope{
				ScopeType: viewer.ScopeTypeUnit,
				TargetIDs: unitIDs,
			})

		default:
			// UNSPECIFIED / 未知值剔除
		}
	}
	return result
}

package data

import (
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	"go-wind-admin/app/admin/service/internal/data/ent/planquota"
)

// 配额 code/旧枚举兼容映射：唯一权威来源。
// 禁止在 Service/Repo 其他位置再写映射 switch（AGENTS/计划 §5.2）。
//
// 兼容规则（计划 §5.2 固定）：
//   - user.count/storage.bytes/api.calls 与旧 USER_LIMIT/STORAGE/API_CALL 一一对应；
//   - gpu.count 没有旧枚举，读取时旧 enum 投影为 UNSPECIFIED/nil，绝不冒充旧值；
//   - 旧字段 tag/类型/编号不变，仅标 deprecated；本批不删除旧字段。
const (
	QuotaCodeUserCount  = "user.count"
	QuotaCodeStorage    = "storage.bytes"
	QuotaCodeApiCalls   = "api.calls"
	QuotaCodeGpuCount   = "gpu.count"
	QuotaCodeUnitGpu    = "gpu"
	QuotaCodeOwnerLab   = "ani-gpu-simulator"
	QuotaCodeActionCreate = "LAB_GPU_CREATE"
	QuotaCodeActionDelete = "LAB_GPU_DELETE"
)

// codeToLegacyProto 将配额编码映射到旧 proto 枚举；第二返回值 false 表示无旧枚举。
func codeToLegacyProto(code string) (identityV1.PlanQuota_QuotaType, bool) {
	switch code {
	case QuotaCodeUserCount:
		return identityV1.PlanQuota_USER_LIMIT, true
	case QuotaCodeStorage:
		return identityV1.PlanQuota_STORAGE, true
	case QuotaCodeApiCalls:
		return identityV1.PlanQuota_API_CALL, true
	default:
		return identityV1.PlanQuota_PLAN_QUOTA_TYPE_UNSPECIFIED, false
	}
}

// CodeToLegacyEntType 将配额编码映射到 ent 旧枚举。
func CodeToLegacyEntType(code string) (planquota.QuotaType, bool) {
	t, ok := codeToLegacyProto(code)
	if !ok {
		return planquota.QuotaTypeUserLimit, false
	}
	switch t {
	case identityV1.PlanQuota_USER_LIMIT:
		return planquota.QuotaTypeUserLimit, true
	case identityV1.PlanQuota_STORAGE:
		return planquota.QuotaTypeStorage, true
	case identityV1.PlanQuota_API_CALL:
		return planquota.QuotaTypeApiCall, true
	default:
		return planquota.QuotaTypeUserLimit, false
	}
}

// LegacyProtoTypeToCode 将旧 proto 枚举映射到配额编码；UNSPECIFIED 无映射。
func LegacyProtoTypeToCode(t identityV1.PlanQuota_QuotaType) (string, bool) {
	switch t {
	case identityV1.PlanQuota_USER_LIMIT:
		return QuotaCodeUserCount, true
	case identityV1.PlanQuota_STORAGE:
		return QuotaCodeStorage, true
	case identityV1.PlanQuota_API_CALL:
		return QuotaCodeApiCalls, true
	default:
		return "", false
	}
}

// LegacyEntTypeToProto 将 ent 旧枚举映射到 proto 旧枚举。
func LegacyEntTypeToProto(qt planquota.QuotaType) identityV1.PlanQuota_QuotaType {
	switch qt {
	case planquota.QuotaTypeUserLimit:
		return identityV1.PlanQuota_USER_LIMIT
	case planquota.QuotaTypeStorage:
		return identityV1.PlanQuota_STORAGE
	case planquota.QuotaTypeApiCall:
		return identityV1.PlanQuota_API_CALL
	default:
		return identityV1.PlanQuota_PLAN_QUOTA_TYPE_UNSPECIFIED
	}
}

// IsLegacyQuotaCode 判断编码是否属于旧三项。
func IsLegacyQuotaCode(code string) bool {
	_, ok := codeToLegacyProto(code)
	return ok
}

// ResolveQuotaCodeForWrite 解析写入路径的权威 quota_code（计划 §5.2）：
//   - 同时给出 code/type：必须一致，否则返回 false（调用方回 400）；
//   - 只有旧枚举：按固定映射转换；UNSPECIFIED/未知枚举返回 false；
//   - 只有 code：直接使用（catalog 存在性由调用方在目录校验）；
//   - 两者都缺失：返回 false。
func ResolveQuotaCodeForWrite(code *string, legacyType *identityV1.PlanQuota_QuotaType) (string, bool) {
	switch {
	case code != nil && *code != "" && legacyType != nil:
		mapped, ok := LegacyProtoTypeToCode(*legacyType)
		if !ok || mapped != *code {
			return "", false
		}
		return *code, true
	case code != nil && *code != "":
		return *code, true
	case legacyType != nil:
		return LegacyProtoTypeToCode(*legacyType)
	default:
		return "", false
	}
}

// ProjectLegacyTypeForRead 读取投影：旧三项返回对应旧枚举；其余（如 gpu.count）返回 nil，
// 由 proto 序列化为 UNSPECIFIED/省略，绝不冒充 USER_LIMIT 等旧值。
func ProjectLegacyTypeForRead(code string) *identityV1.PlanQuota_QuotaType {
	t, ok := codeToLegacyProto(code)
	if !ok {
		return nil
	}
	return &t
}

package data

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go-wind-admin/pkg/localdeps/go-utils/trans"

	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
)

// TestQuotaCodeMap_Mappings 固定 §5.2 兼容映射矩阵：
// 旧三项 ↔ 旧枚举一一对应；gpu.count 无旧枚举；读取投影不冒充旧值。
func TestQuotaCodeMap_Mappings(t *testing.T) {
	// 写路径解析
	for _, c := range []struct {
		code *string
		typ  *identityV1.PlanQuota_QuotaType
		want string
	}{
		{trans.Ptr(QuotaCodeUserCount), identityV1.PlanQuota_USER_LIMIT.Enum(), QuotaCodeUserCount},
		{trans.Ptr(QuotaCodeStorage), identityV1.PlanQuota_STORAGE.Enum(), QuotaCodeStorage},
		{trans.Ptr(QuotaCodeApiCalls), identityV1.PlanQuota_API_CALL.Enum(), QuotaCodeApiCalls},
		{trans.Ptr(QuotaCodeGpuCount), nil, QuotaCodeGpuCount},
		{nil, identityV1.PlanQuota_USER_LIMIT.Enum(), QuotaCodeUserCount},
		{nil, identityV1.PlanQuota_STORAGE.Enum(), QuotaCodeStorage},
		{nil, identityV1.PlanQuota_API_CALL.Enum(), QuotaCodeApiCalls},
	} {
		got, ok := ResolveQuotaCodeForWrite(c.code, c.typ)
		require.True(t, ok, "code=%v type=%v 应可解析", c.code, c.typ)
		require.Equal(t, c.want, got)
	}

	// 冲突/缺失/未知
	_, ok := ResolveQuotaCodeForWrite(trans.Ptr(QuotaCodeUserCount), identityV1.PlanQuota_STORAGE.Enum())
	require.False(t, ok, "code 与 type 不一致必须拒绝")
	_, ok = ResolveQuotaCodeForWrite(trans.Ptr(QuotaCodeGpuCount), identityV1.PlanQuota_USER_LIMIT.Enum())
	require.False(t, ok, "gpu.count 与任何旧枚举组合都必须拒绝")
	_, ok = ResolveQuotaCodeForWrite(nil, identityV1.PlanQuota_PLAN_QUOTA_TYPE_UNSPECIFIED.Enum())
	require.False(t, ok, "UNSPECIFIED 无映射")
	_, ok = ResolveQuotaCodeForWrite(nil, nil)
	require.False(t, ok, "两者都缺失必须拒绝")

	// 读取投影
	require.Equal(t, identityV1.PlanQuota_USER_LIMIT.Enum(), ProjectLegacyTypeForRead(QuotaCodeUserCount))
	require.Nil(t, ProjectLegacyTypeForRead(QuotaCodeGpuCount), "gpu.count 不得投影为旧枚举")
	require.False(t, IsLegacyQuotaCode(QuotaCodeGpuCount))
	require.True(t, IsLegacyQuotaCode(QuotaCodeStorage))
}

package constants

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestUserTenantRelationTypeValues 钉死关系类型枚举值。
// 代码库中存在按 0/1/2 分流的逻辑（如 IsTenantModeEnabled 的编译期判断），
// 枚举值漂移会静默改变多租户行为。
func TestUserTenantRelationTypeValues(t *testing.T) {
	assert.EqualValues(t, 0, UserTenantRelationNone)
	assert.EqualValues(t, 1, UserTenantRelationOneToOne)
	assert.EqualValues(t, 2, UserTenantRelationOneToMany)
}

// TestDefaultTenantRelationConfig 钉死默认租户配置：
// 用户-租户一对一关联 + 租户模式启用。
// IsTenantModeEnabled 是租户隔离闸门（数据层读写隔离）的根开关，
// 该值意外翻 false 会让隔离层整体旁路，必须显式改配置并同步本测试。
func TestDefaultTenantRelationConfig(t *testing.T) {
	assert.Equal(t, UserTenantRelationOneToOne, DefaultUserTenantRelationType,
		"默认用户-租户关系必须是一对一；改动意味着数据模型语义变化")
	assert.True(t, IsTenantModeEnabled,
		"租户模式默认必须启用（IsTenantModeEnabled 为 false 会旁路租户隔离闸门）")
}

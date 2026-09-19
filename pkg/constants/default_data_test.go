package constants

import (
	"testing"

	"github.com/stretchr/testify/assert"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	"go-wind-admin/pkg/password"
)

// TestDefaultPasswordMeetsPolicy 种子密码必须通过等保复杂度策略。
// 回归背景：默认管理员密码曾是 "admin"，prepareCredential 入库前的复杂度校验
// 必然拒绝，导致空库全新部署时凭证行创建失败且错误被吞，admin 永久无法登录
// （GitHub issue #58）。
func TestDefaultPasswordMeetsPolicy(t *testing.T) {
	assert.NoError(t, password.ValidateComplexity(DefaultUserPassword, password.DefaultMinLen),
		"DefaultUserPassword 必须满足口令复杂度策略，否则默认数据初始化会半途失败")
}

// TestDefaultUserCredentialsUsePolicyCompliantPassword 防漂移：
// 哈希类凭证的种子密码必须是 DefaultUserPassword（策略合规的唯一默认密码），
// 不允许再引入独立的明文密码常量绕过复杂度校验。
func TestDefaultUserCredentialsUsePolicyCompliantPassword(t *testing.T) {
	for _, credential := range DefaultUserCredentials {
		if credential.GetCredentialType() != authenticationV1.UserCredential_PASSWORD_HASH {
			continue
		}
		assert.Equal(t, DefaultUserPassword, credential.GetCredential(),
			"PASSWORD_HASH 种子凭证的 Credential 必须引用 DefaultUserPassword")
		assert.NoError(t, password.ValidateComplexity(credential.GetCredential(), password.DefaultMinLen))
	}
}

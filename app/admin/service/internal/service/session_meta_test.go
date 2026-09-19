// session_meta.go 的守卫分支测试（白盒，包内测试）。
//
// 覆盖目标：recordSessionMeta / recordSessionMetaAt 的三个早退守卫——
// authenticator 为 nil、tokenPayload 为 nil、jti 为空。
//
// 跳过项：真实会话元数据落库链路（Authenticator.SaveSessionMeta 经
// UserTokenCache 走 Redis，本批 redisClient 恒 nil 无构造途径）与
// User-Agent/IP 提取（依赖 kratos HTTP 传输层上下文）。
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-utils/trans"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
)

// TestRecordSessionMeta_Guards 验证三个早退守卫均不 panic 且不触碰下游。
func TestRecordSessionMeta_Guards(t *testing.T) {
	ctx := context.Background()

	require.NotPanics(t, func() {
		// authenticator 为 nil
		recordSessionMeta(ctx, nil, nil, authenticationV1.ClientType_admin,
			&authenticationV1.UserTokenPayload{Jti: trans.Ptr("jti-1")})
		// tokenPayload 为 nil
		recordSessionMeta(ctx, nil, nil, authenticationV1.ClientType_admin, nil)
		// jti 为空
		recordSessionMeta(ctx, nil, nil, authenticationV1.ClientType_admin,
			&authenticationV1.UserTokenPayload{})
	}, "三守卫均应早退而非触碰 nil 下游")

	require.NotPanics(t, func() {
		recordSessionMetaAt(ctx, nil, nil, authenticationV1.ClientType_admin,
			&authenticationV1.UserTokenPayload{Jti: trans.Ptr("jti-2")}, 123)
		recordSessionMetaAt(ctx, nil, nil, authenticationV1.ClientType_admin, nil, 123)
		recordSessionMetaAt(ctx, nil, nil, authenticationV1.ClientType_admin,
			&authenticationV1.UserTokenPayload{}, 123)
	}, "recordSessionMetaAt 的同款三守卫均应早退")
}

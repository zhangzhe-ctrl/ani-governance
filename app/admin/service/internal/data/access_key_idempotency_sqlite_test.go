package data

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go-wind-admin/pkg/localdeps/go-crud/viewer"
	"go-wind-admin/pkg/localdeps/go-utils/trans"

	accesskeyV1 "go-wind-admin/api/gen/go/access_key/service/v1"
	"go-wind-admin/app/admin/service/internal/data/ent/accesskey"
	"go-wind-admin/app/admin/service/internal/data/ent/accesskeyidempotency"
	"go-wind-admin/app/admin/service/internal/data/ent/role"
	"go-wind-admin/app/admin/service/tests/testutil"
	appcrypto "go-wind-admin/pkg/crypto"
	appViewer "go-wind-admin/pkg/entgo/viewer"
)

// SQLite covers the idempotent create decision table. The tenant lock and true
// concurrent contention are checked on the isolated PostgreSQL lab; here the
// unique index is the only serialization point exercised.
func newIdempotencyFixture(t *testing.T) (*AccessKeyRepo, context.Context, context.Context, uint32) {
	t.Helper()
	cipher, err := appcrypto.NewAccessKeyCipher("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	repo := NewAccessKeyRepo(testutil.NewBootstrapContext(nil), testutil.NewEntClientForTest(t), cipher)
	system := testutil.NewSystemViewerCtx(context.Background())
	tenantCtx := viewer.WithContext(context.Background(), appViewer.NewUserViewer(10, 7, 0, "", []viewer.DataScope{{ScopeType: viewer.ScopeTypeAll}}))
	r, err := repo.entClient.Client().Role.Create().SetTenantID(7).SetName("reader").SetCode("tenant:reader").SetType(role.TypeTenant).SetStatus(role.StatusOn).Save(system)
	require.NoError(t, err)
	return repo, system, tenantCtx, r.ID
}

func idempotentData(roleID uint32, key string) *accesskeyV1.CreateAccessKeyData {
	return &accesskeyV1.CreateAccessKeyData{Name: trans.Ptr("reader"), RoleId: trans.Ptr(roleID), IdempotencyKey: trans.Ptr(key)}
}

func countIdempotencyRows(t *testing.T, repo *AccessKeyRepo, ctx context.Context) int {
	t.Helper()
	n, err := repo.entClient.Client().AccessKeyIdempotency.Query().Where(accesskeyidempotency.TenantIDEQ(7)).Count(ctx)
	require.NoError(t, err)
	return n
}

func TestAccessKeyIdempotentReplaySameKey(t *testing.T) {
	repo, system, ctx, roleID := newIdempotencyFixture(t)
	first, replayed, err := repo.CreateIdempotent(ctx, idempotentData(roleID, "key-1"), 7, 10, "ak-1", "sk-1")
	require.NoError(t, err)
	require.False(t, replayed)
	require.NotZero(t, first.GetId())

	again, replayed, err := repo.CreateIdempotent(ctx, idempotentData(roleID, "key-1"), 7, 10, "ak-2", "sk-2")
	require.NoError(t, err)
	require.True(t, replayed)
	require.Equal(t, first.GetId(), again.GetId())
	require.Equal(t, "ak-1", again.GetAccessKey())

	count, err := repo.entClient.Client().AccessKey.Query().Where(accesskey.TenantIDEQ(7)).Count(system)
	require.NoError(t, err)
	require.Equal(t, 1, count, "同键同意图只应存在一把凭证")
	require.Equal(t, 1, countIdempotencyRows(t, repo, system))
}

func TestAccessKeyIdempotentDistinctKeysCreateDistinctKeys(t *testing.T) {
	repo, system, ctx, roleID := newIdempotencyFixture(t)
	first, replayed, err := repo.CreateIdempotent(ctx, idempotentData(roleID, "key-a"), 7, 10, "ak-a", "sk-a")
	require.NoError(t, err)
	require.False(t, replayed)
	second, replayed, err := repo.CreateIdempotent(ctx, idempotentData(roleID, "key-b"), 7, 10, "ak-b", "sk-b")
	require.NoError(t, err)
	require.False(t, replayed)
	require.NotEqual(t, first.GetId(), second.GetId())
	count, err := repo.entClient.Client().AccessKey.Query().Where(accesskey.TenantIDEQ(7)).Count(system)
	require.NoError(t, err)
	require.Equal(t, 2, count)
}

func TestAccessKeyIdempotentConflictOnDifferentIntent(t *testing.T) {
	repo, _, ctx, roleID := newIdempotencyFixture(t)
	_, replayed, err := repo.CreateIdempotent(ctx, idempotentData(roleID, "key-1"), 7, 10, "ak-1", "sk-1")
	require.NoError(t, err)
	require.False(t, replayed)

	changed := idempotentData(roleID, "key-1")
	changed.Name = trans.Ptr("renamed")
	_, _, err = repo.CreateIdempotent(ctx, changed, 7, 10, "ak-2", "sk-2")
	require.ErrorIs(t, err, ErrAccessKeyIdempotencyConflict)
}

func TestAccessKeyIdempotentScopeIsolation(t *testing.T) {
	repo, _, ctx, roleID := newIdempotencyFixture(t)
	_, _, err := repo.CreateIdempotent(ctx, idempotentData(roleID, "key-1"), 7, 10, "ak-1", "sk-1")
	require.NoError(t, err)

	// 不同 actor 使用同一 key 属于独立作用域，不得回放到他人的凭证。
	otherActor, replayed, err := repo.CreateIdempotent(ctx, idempotentData(roleID, "key-1"), 7, 11, "ak-2", "sk-2")
	require.NoError(t, err)
	require.False(t, replayed)
	require.NotZero(t, otherActor.GetId())
	require.Equal(t, 2, countIdempotencyRows(t, repo, testutil.NewSystemViewerCtx(context.Background())))
}

// 独立幂等记录表的核心价值：凭证行被物理删除后，同键重放必须判定为"无对象可回放"，
// 而不是静默再建第二把。
func TestAccessKeyIdempotentReplayAfterDelete(t *testing.T) {
	repo, system, ctx, roleID := newIdempotencyFixture(t)
	created, _, err := repo.CreateIdempotent(ctx, idempotentData(roleID, "key-1"), 7, 10, "ak-1", "sk-1")
	require.NoError(t, err)
	require.NoError(t, repo.Delete(ctx, created.GetId(), 7))

	_, replayed, err := repo.CreateIdempotent(ctx, idempotentData(roleID, "key-1"), 7, 10, "ak-2", "sk-2")
	require.Error(t, err)
	require.False(t, replayed)

	count, err := repo.entClient.Client().AccessKey.Query().Where(accesskey.TenantIDEQ(7)).Count(system)
	require.NoError(t, err)
	require.Zero(t, count, "删除后同键重放不得静默再建一把")
	require.Equal(t, 1, countIdempotencyRows(t, repo, system), "幂等记录需比凭证行活得更久")
}

func TestAccessKeyCreateFingerprintDistinguishesIntent(t *testing.T) {
	base := idempotentData(12, "ignored")
	require.Equal(t, AccessKeyCreateFingerprint(base), AccessKeyCreateFingerprint(idempotentData(12, "other")))

	byRole := idempotentData(12, "ignored")
	byRole.RoleId = trans.Ptr(uint32(13))
	require.NotEqual(t, AccessKeyCreateFingerprint(base), AccessKeyCreateFingerprint(byRole))

	byName := idempotentData(12, "ignored")
	byName.Name = trans.Ptr("other")
	require.NotEqual(t, AccessKeyCreateFingerprint(base), AccessKeyCreateFingerprint(byName))
}

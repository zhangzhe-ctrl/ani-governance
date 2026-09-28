package service

import (
	"testing"

	"github.com/redis/go-redis/v9"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/tests/testutil"
	appcrypto "go-wind-admin/pkg/crypto"
	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"
	conf "go-wind-admin/pkg/localdeps/kratos-bootstrap/api/gen/go/conf/v1"
	"go-wind-admin/pkg/localdeps/kratos-bootstrap/bootstrap"
)

func newRepoContext() *bootstrap.Context {
	return testutil.NewBootstrapContext(nil)
}

// The graph below uses exactly the production constructors and one test-owned
// Ent client. No mapper, converter, or repository fields are recreated here.
func newPermissionRepo(client *entCrud.EntClient[*ent.Client]) *data.PermissionRepo {
	ctx := newRepoContext()
	return data.NewPermissionRepo(ctx, client,
		data.NewPermissionApiRepo(ctx, client), data.NewPermissionMenuRepo(ctx, client))
}

func newRoleRepo(client *entCrud.EntClient[*ent.Client]) *data.RoleRepo {
	ctx := newRepoContext()
	return data.NewRoleRepo(ctx, client,
		data.NewRolePermissionRepo(ctx, client),
		data.NewRoleOrgUnitRepo(ctx, client),
		data.NewPermissionRepo(ctx, client,
			data.NewPermissionApiRepo(ctx, client), data.NewPermissionMenuRepo(ctx, client)),
		data.NewRoleMetadataRepo(ctx, client),
		data.NewRoleFieldPermissionRepo(ctx, client))
}

func newOrgUnitRepo(client *entCrud.EntClient[*ent.Client]) *data.OrgUnitRepo {
	ctx := newRepoContext()
	return data.NewOrgUnitRepo(ctx, client, data.NewUserOrgUnitRepo(ctx, client))
}

func newDictEntryRepo(client *entCrud.EntClient[*ent.Client]) *data.DictEntryRepo {
	ctx := newRepoContext()
	return data.NewDictEntryRepo(ctx, client, data.NewDictEntryI18nRepo(ctx, client))
}

func newConfigRepo(t *testing.T, client *entCrud.EntClient[*ent.Client], rdb *redis.Client) *data.ConfigRepo {
	t.Helper()
	repo := data.NewConfigRepo(newRepoContext(), client, rdb)
	t.Cleanup(repo.Close)
	return repo
}

func newAuthenticator(t *testing.T, jwt *conf.Authentication_Jwt, cache *data.UserTokenCache) *data.Authenticator {
	t.Helper()
	// Exercise the production precedence rules with an explicit test config.
	t.Setenv("GWA_AUTH_JWT_PRIVATE_KEY", "")
	t.Setenv("GWA_AUTH_JWT_PUBLIC_KEY", "")
	t.Setenv("GWA_AUTH_JWT_KEY", "")
	ctx := testutil.NewBootstrapContext(&conf.Bootstrap{Authn: &conf.Authentication{Jwt: jwt}})
	a := data.NewAuthenticator(ctx, cache)
	if a == nil {
		t.Fatal("production authenticator constructor returned nil")
	}
	return a
}

func newAccessKeyRepo(t *testing.T, client *entCrud.EntClient[*ent.Client]) *data.AccessKeyRepo {
	t.Helper()
	cipher, err := appcrypto.NewAccessKeyCipher("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	return data.NewAccessKeyRepo(newRepoContext(), client, cipher)
}

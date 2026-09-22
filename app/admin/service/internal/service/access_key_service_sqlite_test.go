package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-crud/viewer"
	"github.com/tx7do/go-utils/trans"
	accesskeyV1 "go-wind-admin/api/gen/go/access_key/service/v1"
	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/ent/role"
	"go-wind-admin/app/admin/service/internal/data/enttest"
	appViewer "go-wind-admin/pkg/entgo/viewer"
	"go-wind-admin/pkg/middleware/auth"
)

func TestAccessKeyServiceCRUD(t *testing.T) {
	client := enttest.NewEntClientForTest(t)
	system := enttest.NewSystemViewerCtx(context.Background())
	r, err := client.Client().Role.Create().SetTenantID(7).SetName("reader").SetCode("tenant:reader").SetType(role.TypeTenant).SetStatus(role.StatusOn).Save(system)
	require.NoError(t, err)
	ctx := viewer.WithContext(context.Background(), appViewer.NewUserViewer(10, 7, 0, "", []viewer.DataScope{{ScopeType: viewer.ScopeTypeAll}}))
	ctx = auth.NewContext(ctx, &authenticationV1.UserTokenPayload{UserId: 10, TenantId: trans.Ptr(uint32(7))})
	svc := NewAccessKeyService(nil, data.NewAccessKeyRepoForTest(client))
	created, err := svc.Create(ctx, &accesskeyV1.CreateAccessKeyRequest{Data: &accesskeyV1.AccessKey{Name: trans.Ptr("reader"), RoleId: trans.Ptr(r.ID)}})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(created.SecretKey, "sk-"))
	require.True(t, strings.HasPrefix(created.Data.GetAccessKey(), "ak-"))
	reset, err := svc.ResetSecret(ctx, &accesskeyV1.ResetAccessKeySecretRequest{KeyId: created.Data.GetId()})
	require.NoError(t, err)
	require.NotEqual(t, created.SecretKey, reset.SecretKey)
	require.Equal(t, created.Data.GetAccessKey(), reset.Data.GetAccessKey())
	deleted, err := svc.Delete(ctx, &accesskeyV1.DeleteAccessKeyRequest{KeyId: created.Data.GetId()})
	require.NoError(t, err)
	require.Equal(t, "revoked", deleted.Status)
	machine := auth.NewContext(ctx, &authenticationV1.UserTokenPayload{UserId: 0, TenantId: trans.Ptr(uint32(7))})
	_, err = svc.Get(machine, &accesskeyV1.GetAccessKeyRequest{KeyId: created.Data.GetId()})
	require.Error(t, err)
	_, err = svc.Create(ctx, nil)
	require.Error(t, err)
}

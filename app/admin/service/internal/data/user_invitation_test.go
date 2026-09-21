package data

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	authV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/user"
	"go-wind-admin/app/admin/service/internal/data/ent/usercredential"
	"go-wind-admin/app/admin/service/internal/data/enttest"
)

func TestInvitationAcceptance(t *testing.T) {
	for _, scenario := range []string{"success", "expired", "weak-password", "email-changed", "disabled-user", "tenant-changed", "credential-conflict"} {
		t.Run(scenario, func(t *testing.T) {
			r := newUserCredentialRepoSqlite(t)
			ctx := enttest.NewSystemViewerCtx(context.Background())
			client := r.entClient.Client()
			userTenant := uint32(7)
			if scenario == "tenant-changed" {
				userTenant = 8
			}
			u, err := client.User.Create().SetUsername("invited").SetTenantID(userTenant).SetEmail("invite@example.com").SetStatus(user.StatusPending).Save(ctx)
			require.NoError(t, err)
			secret := make([]byte, 32)
			_, err = rand.Read(secret)
			require.NoError(t, err)
			token := base64.RawURLEncoding.EncodeToString(secret)
			expires := time.Now().Add(time.Hour)
			if scenario == "expired" {
				expires = time.Now().Add(-time.Second)
			}
			require.NoError(t, r.InTransaction(ctx, func(tx *ent.Tx) error {
				return r.CreateInvitationWithTx(ctx, tx, u.ID, 7, "invited", "invite@example.com", token, expires)
			}))
			row := client.UserCredential.Query().OnlyX(ctx)
			require.Nil(t, row.Credential)
			require.NotEqual(t, token, *row.ActivateTokenHash)
			_, err = r.FindUserCredential(ctx, 7, authV1.UserCredential_USERNAME, "invited", "Password123!", false)
			require.Error(t, err, "pending credential cannot log in")
			require.Error(t, r.AcceptInvitation(context.Background(), "invalid", "Password123!"))
			password := "Password123!"
			switch scenario {
			case "weak-password":
				password = "123"
			case "email-changed":
				client.User.UpdateOneID(u.ID).SetEmail("changed@example.com").SaveX(ctx)
			case "disabled-user":
				client.User.UpdateOneID(u.ID).SetStatus(user.StatusDisabled).SaveX(ctx)
			case "credential-conflict":
				client.UserCredential.Create().SetUserID(u.ID).SetTenantID(7).SetIdentityType(usercredential.IdentityTypeEmail).SetIdentifier("invite@example.com").SaveX(ctx)
			}
			err = r.AcceptInvitation(context.Background(), token, password)
			if scenario != "success" {
				require.Error(t, err)
				unchanged := client.UserCredential.GetX(ctx, row.ID)
				require.Nil(t, unchanged.Credential)
				require.Nil(t, unchanged.ActivateTokenUsedAt)
				require.Equal(t, row.ActivateTokenHash, unchanged.ActivateTokenHash)
				if scenario == "weak-password" {
					require.NoError(t, r.AcceptInvitation(context.Background(), token, "Password123!"))
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, user.StatusNormal, *client.User.GetX(ctx, u.ID).Status)
			accepted := client.UserCredential.GetX(ctx, row.ID)
			require.Nil(t, accepted.ActivateTokenHash)
			require.NotNil(t, accepted.ActivateTokenUsedAt)
			require.NotEqual(t, password, *accepted.Credential)
			id, err := r.FindUserCredential(ctx, 7, authV1.UserCredential_USERNAME, "invited", password, false)
			require.NoError(t, err)
			require.Equal(t, u.ID, id)
			_, err = r.FindUserCredential(ctx, 8, authV1.UserCredential_USERNAME, "invited", password, false)
			require.Error(t, err, "password must not authenticate in another tenant")
			require.Error(t, r.AcceptInvitation(context.Background(), token, "Different123!"))
			require.Equal(t, accepted.Credential, client.UserCredential.GetX(ctx, row.ID).Credential)
		})
	}
}

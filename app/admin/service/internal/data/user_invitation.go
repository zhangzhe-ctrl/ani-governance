package data

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/user"
	"go-wind-admin/app/admin/service/internal/data/ent/usercredential"
	appViewer "go-wind-admin/pkg/entgo/viewer"
)

// InTransaction exposes the existing repositories' CreateWithTx composition.
// Commit errors are returned to the caller; a callback error never commits.
func (r *UserCredentialRepo) InTransaction(ctx context.Context, fn func(*ent.Tx) error) (err error) {
	tx, err := r.entClient.Client().Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin account transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); err != nil && rollbackErr != nil {
			err = errors.Join(err, fmt.Errorf("rollback account transaction: %w", rollbackErr))
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit account transaction: %w", err)
	}
	return nil
}

type invitationSnapshot struct {
	Email string `json:"invitation_email"`
}

func invitationTokenHash(token string) (string, bool) {
	if len(token) != 43 {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != token {
		return "", false
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:]), true
}

// CreateInvitationWithTx stores only a token hash and the originally invited
// address. There is deliberately no usable password before acceptance.
func (r *UserCredentialRepo) CreateInvitationWithTx(ctx context.Context, tx *ent.Tx, userID, tenantID uint32, username, email, token string, expires time.Time) error {
	hash, ok := invitationTokenHash(token)
	if !ok || userID == 0 || username == "" || email == "" {
		return authenticationV1.ErrorBadRequest("invalid invitation")
	}
	snapshot, err := json.Marshal(invitationSnapshot{Email: email})
	if err != nil {
		return err
	}
	return tx.UserCredential.Create().
		SetUserID(userID).SetTenantID(tenantID).
		SetIdentityType(usercredential.IdentityTypeUsername).SetIdentifier(username).
		SetCredentialType(usercredential.CredentialTypePasswordHash).
		SetIsPrimary(true).SetStatus(usercredential.StatusDisabled).
		SetActivateTokenHash(hash).SetActivateTokenExpiresAt(expires).
		SetExtraInfo(string(snapshot)).SetCreatedAt(time.Now()).Exec(ctx)
}

func (r *UserCredentialRepo) AcceptInvitation(ctx context.Context, token, password string) error {
	invalid := func() error {
		return authenticationV1.ErrorBadRequest("invitation is invalid, expired or already used")
	}
	hash, ok := invitationTokenHash(token)
	if !ok {
		return invalid()
	}
	// This public operation derives both user and tenant only from a stored secret
	// hash. No client-supplied user, role or tenant identifier is accepted.
	ctx = appViewer.NewSystemViewerContext(ctx)
	credential, err := r.entClient.Client().UserCredential.Query().Where(
		usercredential.ActivateTokenHashEQ(hash),
		usercredential.IdentityTypeEQ(usercredential.IdentityTypeUsername),
		usercredential.StatusEQ(usercredential.StatusDisabled),
		usercredential.ActivateTokenUsedAtIsNil(),
		usercredential.ActivateTokenExpiresAtGT(time.Now()),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return invalid()
	}
	if err != nil {
		return authenticationV1.ErrorInternalServerError("read invitation failed")
	}
	var snapshot invitationSnapshot
	if credential.UserID == nil || credential.TenantID == nil || credential.ExtraInfo == nil ||
		json.Unmarshal([]byte(*credential.ExtraInfo), &snapshot) != nil || snapshot.Email == "" {
		return invalid()
	}
	kind := usercredential.CredentialTypePasswordHash
	hashedPassword, err := r.prepareCredential(ctx, &kind, password)
	if err != nil {
		return err
	}
	return r.InTransaction(ctx, func(tx *ent.Tx) error {
		now := time.Now()
		count, err := tx.UserCredential.Update().Where(
			usercredential.IDEQ(credential.ID), usercredential.UserIDEQ(*credential.UserID),
			usercredential.TenantIDEQ(*credential.TenantID), usercredential.ActivateTokenHashEQ(hash),
			usercredential.StatusEQ(usercredential.StatusDisabled),
			usercredential.ActivateTokenUsedAtIsNil(), usercredential.ActivateTokenExpiresAtGT(now),
		).SetCredential(hashedPassword).SetStatus(usercredential.StatusEnabled).
			SetActivateTokenUsedAt(now).ClearActivateTokenHash().ClearExtraInfo().SetUpdatedAt(now).Save(ctx)
		if err != nil {
			return fmt.Errorf("consume invitation: %w", err)
		}
		if count != 1 {
			return invalid()
		}
		count, err = tx.User.Update().Where(user.IDEQ(*credential.UserID),
			user.TenantIDEQ(*credential.TenantID), user.StatusEQ(user.StatusPending),
			user.EmailEQ(snapshot.Email),
		).SetStatus(user.StatusNormal).SetUpdatedAt(now).Save(ctx)
		if err != nil {
			return fmt.Errorf("activate invited user: %w", err)
		}
		if count != 1 {
			return invalid()
		}
		// Verified email is an identity locator for password recovery, not another
		// password. The USERNAME credential remains the password authority.
		if err = tx.UserCredential.Create().SetUserID(*credential.UserID).SetTenantID(*credential.TenantID).
			SetIdentityType(usercredential.IdentityTypeEmail).SetIdentifier(snapshot.Email).
			SetCredentialType(usercredential.CredentialTypePasswordHash).
			SetIsPrimary(false).SetStatus(usercredential.StatusEnabled).SetCreatedAt(now).Exec(ctx); err != nil {
			return fmt.Errorf("confirm invited email: %w", err)
		}
		return nil
	})
}

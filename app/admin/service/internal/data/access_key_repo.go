package data

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/privacy"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"
	"go-wind-admin/pkg/localdeps/go-utils/copierutil"
	"go-wind-admin/pkg/localdeps/go-utils/mapper"
	"go-wind-admin/pkg/localdeps/go-utils/trans"
	"google.golang.org/protobuf/types/known/timestamppb"

	accesskeyV1 "go-wind-admin/api/gen/go/access_key/service/v1"
	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/accesskey"
	"go-wind-admin/app/admin/service/internal/data/ent/predicate"
	"go-wind-admin/app/admin/service/internal/data/ent/role"
	appcrypto "go-wind-admin/pkg/crypto"
	"go-wind-admin/pkg/middleware/auth"
)

type AccessKeyRepo struct {
	entClient  *entCrud.EntClient[*ent.Client]
	cipher     *appcrypto.AccessKeyCipher
	repository *entCrud.Repository[ent.AccessKeyQuery, ent.AccessKeySelect, ent.AccessKeyCreate, ent.AccessKeyCreateBulk, ent.AccessKeyUpdate, ent.AccessKeyUpdateOne, ent.AccessKeyDelete, predicate.AccessKey, accesskeyV1.AccessKey, ent.AccessKey]
}

func NewAccessKeyRepo(_ *bootstrap.Context, client *entCrud.EntClient[*ent.Client], cipher *appcrypto.AccessKeyCipher) *AccessKeyRepo {
	m := mapper.NewCopierMapper[accesskeyV1.AccessKey, ent.AccessKey]()
	m.AppendConverters(copierutil.NewTimeStringConverterPair())
	m.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())
	return &AccessKeyRepo{entClient: client, cipher: cipher, repository: entCrud.NewRepository[ent.AccessKeyQuery, ent.AccessKeySelect, ent.AccessKeyCreate, ent.AccessKeyCreateBulk, ent.AccessKeyUpdate, ent.AccessKeyUpdateOne, ent.AccessKeyDelete, predicate.AccessKey, accesskeyV1.AccessKey, ent.AccessKey](m)}
}
func accessKeyDTO(e *ent.AccessKey) *accesskeyV1.AccessKey {
	return &accesskeyV1.AccessKey{Id: trans.Ptr(e.ID), Name: e.Name, AccessKey: trans.Ptr(e.AccessKey), RoleId: trans.Ptr(e.RoleID), IsActive: trans.Ptr(e.Status != nil && *e.Status == accesskey.StatusOn), ExpiresAt: keyTimestamp(e.ExpiresAt), CreatedAt: keyTimestamp(e.CreatedAt), LastUsedAt: keyTimestamp(e.LastUsedAt)}
}
func keyTimestamp(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}
func keyRepoError(err error) error {
	if ent.IsNotFound(err) {
		return kerrors.NotFound("ACCESS_KEY_NOT_FOUND", "access key not found")
	}
	return kerrors.ServiceUnavailable("ACCESS_KEY_STORAGE_UNAVAILABLE", "access key storage unavailable").WithCause(err)
}
func (r *AccessKeyRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*accesskeyV1.ListAccessKeyResponse, error) {
	if req == nil {
		return nil, adminV1.ErrorBadRequest("invalid parameter")
	}
	builder := r.entClient.Client().AccessKey.Query()
	ret, err := r.repository.ListWithPaging(ctx, builder, builder.Clone(), req)
	if err != nil {
		return nil, err
	}
	result := &accesskeyV1.ListAccessKeyResponse{Items: []*accesskeyV1.AccessKey{}}
	if ret == nil {
		return result, nil
	}
	result.Total = ret.Total
	// Fetch only the selected IDs to map the internal ON/OFF status explicitly.
	// The generic mapper cannot map ON/OFF to the public is_active boolean.
	ids := make([]uint32, 0, len(ret.Items))
	for _, item := range ret.Items {
		ids = append(ids, item.GetId())
	}
	if len(ids) == 0 {
		return result, nil
	}
	entities, err := r.entClient.Client().AccessKey.Query().Where(accesskey.IDIn(ids...)).All(ctx)
	if err != nil {
		return nil, keyRepoError(err)
	}
	byID := make(map[uint32]*ent.AccessKey, len(entities))
	for _, e := range entities {
		byID[e.ID] = e
	}
	for _, id := range ids {
		if e := byID[id]; e != nil {
			result.Items = append(result.Items, accessKeyDTO(e))
		}
	}
	return result, nil
}
func (r *AccessKeyRepo) Get(ctx context.Context, req *accesskeyV1.GetAccessKeyRequest) (*accesskeyV1.AccessKey, error) {
	if req == nil || req.GetKeyId() == 0 {
		return nil, adminV1.ErrorBadRequest("key_id is required")
	}
	e, err := r.entClient.Client().AccessKey.Query().Where(accesskey.IDEQ(req.GetKeyId())).Only(ctx)
	if err != nil {
		return nil, keyRepoError(err)
	}
	return accessKeyDTO(e), nil
}
func validKeyRole(ctx context.Context, client *ent.Client, tenantID, roleID uint32) error {
	if tenantID == 0 || roleID == 0 {
		return adminV1.ErrorForbidden("an enabled role in the current tenant is required")
	}
	e, err := client.Role.Query().Where(role.IDEQ(roleID), role.TenantIDEQ(tenantID), role.TypeEQ(role.TypeTenant), role.StatusEQ(role.StatusOn)).Only(ctx)
	if ent.IsNotFound(err) {
		return adminV1.ErrorForbidden("an enabled role in the current tenant is required")
	}
	if err != nil {
		return keyRepoError(err)
	}
	if e.Code == nil || *e.Code == "" {
		return adminV1.ErrorForbidden("role has no authorization identity")
	}
	return nil
}
func (r *AccessKeyRepo) Create(ctx context.Context, data *accesskeyV1.AccessKey, tenantID, userID uint32, ak, sk string) (*accesskeyV1.AccessKey, error) {
	ciphertext, err := r.cipher.Encrypt(sk)
	if err != nil {
		return nil, keyRepoError(err)
	}
	tx, err := r.entClient.Client().Tx(ctx)
	if err != nil {
		return nil, keyRepoError(err)
	}
	defer tx.Rollback()
	if err = validKeyRole(ctx, tx.Client(), tenantID, data.GetRoleId()); err != nil {
		return nil, err
	}
	b := tx.AccessKey.Create().SetName(data.GetName()).SetAccessKey(ak).SetSecretCiphertext(ciphertext).SetRoleID(data.GetRoleId()).SetTenantID(tenantID).SetCreatedBy(userID).SetStatus(accesskey.StatusOn).SetCreatedAt(time.Now())
	if data.ExpiresAt != nil {
		b.SetExpiresAt(data.ExpiresAt.AsTime())
	}
	e, err := b.Save(ctx)
	if err != nil {
		return nil, keyRepoError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, keyRepoError(err)
	}
	return accessKeyDTO(e), nil
}
func (r *AccessKeyRepo) Update(ctx context.Context, req *accesskeyV1.UpdateAccessKeyRequest, tenantID, userID uint32) error {
	tx, err := r.entClient.Client().Tx(ctx)
	if err != nil {
		return keyRepoError(err)
	}
	defer tx.Rollback()
	_, err = tx.AccessKey.Query().Where(accesskey.IDEQ(req.GetKeyId()), accesskey.TenantIDEQ(tenantID)).Only(ctx)
	if err != nil {
		return keyRepoError(err)
	}
	b := tx.AccessKey.UpdateOneID(req.GetKeyId()).SetUpdatedBy(userID).SetUpdatedAt(time.Now())
	for _, path := range req.UpdateMask.Paths {
		switch path {
		case "name":
			b.SetName(req.Data.GetName())
		case "role_id":
			if err = validKeyRole(ctx, tx.Client(), tenantID, req.Data.GetRoleId()); err != nil {
				return err
			}
			b.SetRoleID(req.Data.GetRoleId())
		case "is_active":
			if req.Data.GetIsActive() {
				b.SetStatus(accesskey.StatusOn)
			} else {
				b.SetStatus(accesskey.StatusOff)
			}
		case "expires_at":
			if req.Data.ExpiresAt == nil {
				b.ClearExpiresAt()
			} else {
				b.SetExpiresAt(req.Data.ExpiresAt.AsTime())
			}
		}
	}
	if err = b.Exec(ctx); err != nil {
		return keyRepoError(err)
	}
	if err = tx.Commit(); err != nil {
		return keyRepoError(err)
	}
	return nil
}
func (r *AccessKeyRepo) Delete(ctx context.Context, id, tenantID uint32) error {
	n, err := r.entClient.Client().AccessKey.Delete().Where(accesskey.IDEQ(id), accesskey.TenantIDEQ(tenantID)).Exec(ctx)
	if err != nil {
		return keyRepoError(err)
	}
	if n == 0 {
		return kerrors.NotFound("ACCESS_KEY_NOT_FOUND", "access key not found")
	}
	return nil
}
func (r *AccessKeyRepo) ResetSecret(ctx context.Context, id, tenantID, userID uint32, sk string) (*accesskeyV1.AccessKey, error) {
	ciphertext, err := r.cipher.Encrypt(sk)
	if err != nil {
		return nil, keyRepoError(err)
	}
	tx, err := r.entClient.Client().Tx(ctx)
	if err != nil {
		return nil, keyRepoError(err)
	}
	defer tx.Rollback()
	_, err = tx.AccessKey.Query().Where(accesskey.IDEQ(id), accesskey.TenantIDEQ(tenantID)).Only(ctx)
	if err != nil {
		return nil, keyRepoError(err)
	}
	e, err := tx.AccessKey.UpdateOneID(id).SetSecretCiphertext(ciphertext).SetUpdatedBy(userID).SetUpdatedAt(time.Now()).Save(ctx)
	if err != nil {
		return nil, keyRepoError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, keyRepoError(err)
	}
	return accessKeyDTO(e), nil
}

// LookupSigningKey makes only the credential lookup and its bound-role lookup
// without a tenant scope. This local context never escapes into the request and
// never installs a platform/system viewer.
func (r *AccessKeyRepo) LookupSigningKey(ctx context.Context, ak string) (*auth.SigningKey, error) {
	lookupCtx := privacy.DecisionContext(ctx, privacy.Allow)
	e, err := r.entClient.Client().AccessKey.Query().Where(accesskey.AccessKeyEQ(ak)).Only(lookupCtx)
	if ent.IsNotFound(err) {
		return nil, auth.ErrSigningKeyRejected
	}
	if err != nil {
		return nil, fmt.Errorf("lookup signing key: %w", err)
	}
	if e.TenantID == nil || *e.TenantID == 0 || e.Status == nil || *e.Status != accesskey.StatusOn || (e.ExpiresAt != nil && !time.Now().Before(*e.ExpiresAt)) {
		return nil, auth.ErrSigningKeyRejected
	}
	secret, err := r.cipher.Decrypt(e.SecretCiphertext)
	if err != nil {
		return nil, err
	}
	candidate := &auth.SigningKey{ID: e.ID, TenantID: *e.TenantID, Secret: secret}
	bound, err := r.entClient.Client().Role.Query().Where(role.IDEQ(e.RoleID), role.TenantIDEQ(*e.TenantID), role.TypeEQ(role.TypeTenant)).Only(lookupCtx)
	if ent.IsNotFound(err) {
		return candidate, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup signing role: %w", err)
	}
	if bound.Code != nil {
		candidate.Role = *bound.Code
	}
	candidate.RoleAllowed = bound.Code != nil && *bound.Code != "" && bound.Status != nil && *bound.Status == role.StatusOn
	return candidate, nil
}
func (r *AccessKeyRepo) MarkSigningKeyUsed(ctx context.Context, id uint32) error {
	return r.entClient.Client().AccessKey.UpdateOneID(id).SetLastUsedAt(time.Now()).Exec(privacy.DecisionContext(ctx, privacy.Allow))
}

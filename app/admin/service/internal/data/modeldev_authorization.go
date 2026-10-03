package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/api"
	"go-wind-admin/app/admin/service/internal/data/ent/permission"
	"go-wind-admin/app/admin/service/internal/data/ent/permissionapi"
	"go-wind-admin/app/admin/service/internal/data/ent/role"
	"go-wind-admin/app/admin/service/internal/data/ent/rolepermission"
	"go-wind-admin/app/admin/service/internal/data/ent/tenant"
	"go-wind-admin/app/admin/service/internal/data/ent/user"
	"go-wind-admin/app/admin/service/internal/data/ent/userrole"
	"go-wind-admin/pkg/constants"
	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"
)

var (
	ErrModelDevAuthorizationDenied      = errors.New("modeldev create authorization denied")
	ErrModelDevAuthorizationUnavailable = errors.New("modeldev create authorization unavailable")
)

// ModelDevAuthorizationRepo checks the current database grant for the fixed
// CPU-P01 create action. JWT authentication, TenantAccess and Casbin remain
// required before this check; it does not authenticate a user or enable assets.
type ModelDevAuthorizationRepo struct {
	entClient *entCrud.EntClient[*ent.Client]
}

func NewModelDevAuthorizationRepo(client *entCrud.EntClient[*ent.Client]) *ModelDevAuthorizationRepo {
	return &ModelDevAuthorizationRepo{entClient: client}
}

// AuthorizeCreate requires trusted tenant/user IDs from the verified Principal.
// It must run before FindAccepted for both new requests and original-key replay.
func (r *ModelDevAuthorizationRepo) AuthorizeCreate(ctx context.Context, tenantID, userID uint32) (result error) {
	// Cancellation remains distinguishable from a denied grant or failed store,
	// including cancellation during transaction completion or cleanup.
	defer func() {
		if err := ctx.Err(); err != nil {
			result = err
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if tenantID == 0 || userID == 0 {
		return ErrModelDevAuthorizationDenied
	}
	if r == nil || r.entClient == nil || r.entClient.Client() == nil {
		return ErrModelDevAuthorizationUnavailable
	}
	tx, err := r.entClient.Client().BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return ErrModelDevAuthorizationUnavailable
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			result = ErrModelDevAuthorizationUnavailable
		}
	}()

	active, err := tx.Tenant.Query().Where(
		tenant.IDEQ(tenantID), tenant.StatusEQ(tenant.StatusOn), tenant.DeletedAtIsNil(),
	).Exist(ctx)
	if err != nil {
		return ErrModelDevAuthorizationUnavailable
	}
	if !active {
		return ErrModelDevAuthorizationDenied
	}
	active, err = tx.User.Query().Where(
		user.IDEQ(userID), user.TenantIDEQ(tenantID), user.StatusEQ(user.StatusNormal), user.DeletedAtIsNil(),
	).Exist(ctx)
	if err != nil {
		return ErrModelDevAuthorizationUnavailable
	}
	if !active {
		return ErrModelDevAuthorizationDenied
	}

	// The current OneToOne tenant model uses UserRole, not Membership. All
	// membership times and subsequent grants are evaluated in this RR snapshot.
	now := time.Now().UTC()
	var memberRoleIDs []uint32
	err = tx.UserRole.Query().Where(
		userrole.TenantIDEQ(tenantID), userrole.UserIDEQ(userID), userrole.RoleIDGT(0),
		userrole.StatusEQ(userrole.StatusActive), userrole.DeletedAtIsNil(),
		userrole.Or(userrole.StartAtIsNil(), userrole.StartAtLTE(now)),
		userrole.Or(userrole.EndAtIsNil(), userrole.EndAtGT(now)),
	).Select(userrole.FieldRoleID).Scan(ctx, &memberRoleIDs)
	if err != nil {
		return ErrModelDevAuthorizationUnavailable
	}
	if len(memberRoleIDs) == 0 {
		return ErrModelDevAuthorizationDenied
	}
	roles, err := tx.Role.Query().Where(
		role.IDIn(memberRoleIDs...), role.TenantIDEQ(tenantID), role.StatusEQ(role.StatusOn),
		role.TypeEQ(role.TypeTenant), role.CodeNEQ(""), role.DeletedAtIsNil(),
		role.Not(role.CodeHasPrefix(constants.TemplateRoleCodePrefix)),
	).All(ctx)
	if err != nil {
		return ErrModelDevAuthorizationUnavailable
	}
	roleIDs := make([]uint32, 0, len(roles))
	hasAll := false
	for _, current := range roles {
		roleIDs = append(roleIDs, current.ID)
		if current.DataScope != nil && *current.DataScope == role.DataScopeAll {
			hasAll = true
		}
	}
	// Existing data scopes form a union: one effective ALL grants the tenant
	// range. A separate current action grant is still mandatory below.
	if !hasAll {
		return ErrModelDevAuthorizationDenied
	}

	var permissionIDs []uint32
	err = tx.RolePermission.Query().Where(
		rolepermission.TenantIDEQ(tenantID), rolepermission.RoleIDIn(roleIDs...),
		rolepermission.PermissionIDGT(0), rolepermission.StatusEQ(rolepermission.StatusOn),
		rolepermission.EffectEQ(rolepermission.EffectAllow), rolepermission.DeletedAtIsNil(),
	).Select(rolepermission.FieldPermissionID).Scan(ctx, &permissionIDs)
	if err != nil {
		return ErrModelDevAuthorizationUnavailable
	}
	if len(permissionIDs) == 0 {
		return ErrModelDevAuthorizationDenied
	}
	permissionIDs, err = tx.Permission.Query().Where(
		permission.IDIn(permissionIDs...), permission.StatusEQ(permission.StatusOn), permission.DeletedAtIsNil(),
	).IDs(ctx)
	if err != nil {
		return ErrModelDevAuthorizationUnavailable
	}
	if len(permissionIDs) == 0 {
		return ErrModelDevAuthorizationDenied
	}
	var apiIDs []uint32
	err = tx.PermissionApi.Query().Where(
		permissionapi.PermissionIDIn(permissionIDs...), permissionapi.APIIDGT(0), permissionapi.DeletedAtIsNil(),
	).Select(permissionapi.FieldAPIID).Scan(ctx, &apiIDs)
	if err != nil {
		return ErrModelDevAuthorizationUnavailable
	}
	if len(apiIDs) == 0 {
		return ErrModelDevAuthorizationDenied
	}
	allowed, err := tx.Api.Query().Where(
		api.IDIn(apiIDs...), api.StatusEQ(api.StatusOn), api.DeletedAtIsNil(),
		api.ScopeEQ(api.ScopeAdmin), api.BusinessModuleEQ(api.BusinessModuleModel),
		api.PathEQ("/admin/v1/modeldev/executions"), api.MethodEQ("POST"),
	).Exist(ctx)
	if err != nil {
		return ErrModelDevAuthorizationUnavailable
	}
	if !allowed {
		return ErrModelDevAuthorizationDenied
	}
	if err := tx.Commit(); err != nil {
		return ErrModelDevAuthorizationUnavailable
	}
	return nil
}

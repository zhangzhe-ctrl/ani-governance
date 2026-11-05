package data

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go-wind-admin/app/admin/service/internal/data/ent/api"
	"go-wind-admin/app/admin/service/internal/data/ent/permission"
	"go-wind-admin/app/admin/service/internal/data/ent/plan"
	"go-wind-admin/app/admin/service/internal/data/ent/planmodule"
	"go-wind-admin/app/admin/service/internal/data/ent/role"
	"go-wind-admin/app/admin/service/internal/data/ent/rolepermission"
	"go-wind-admin/app/admin/service/internal/data/ent/tenant"
	"go-wind-admin/app/admin/service/internal/data/ent/user"
	"go-wind-admin/app/admin/service/internal/data/ent/userrole"
	"go-wind-admin/app/admin/service/tests/testutil"
	appViewer "go-wind-admin/pkg/entgo/viewer"
	"go-wind-admin/pkg/localdeps/go-crud/viewer"
)

func TestSandboxDetailsRequireIndependentSandboxEntitlement(t *testing.T) {
	c := testutil.NewEntClientForTest(t)
	sys := testutil.NewSystemViewerCtx(context.Background())
	db := c.Client()
	p := db.Plan.Create().SetExpiryPolicy(plan.ExpiryPolicyReadonly).SaveX(sys)
	ten := db.Tenant.Create().SetName("sandbox-details-entitlement").SetStatus(tenant.StatusOn).SetPlanID(p.ID).SaveX(sys)
	db.PlanModule.Create().SetPlanID(p.ID).SetModule(planmodule.ModuleInstance).SaveX(sys)
	actor := db.User.Create().SetTenantID(ten.ID).SetUsername("sandbox-details").SetStatus(user.StatusNormal).SaveX(sys)
	r := db.Role.Create().SetTenantID(ten.ID).SetName("sandbox-details").SetCode("tenant:sandbox-details").SetType(role.TypeTenant).SetStatus(role.StatusOn).SetDataScope(role.DataScopeAll).SaveX(sys)
	db.UserRole.Create().SetTenantID(ten.ID).SetUserID(actor.ID).SetRoleID(r.ID).SetStatus(userrole.StatusActive).SaveX(sys)
	paths := []struct{ path, method string }{{SandboxOperationsListPath, "GET"}, {SandboxEnvironmentPath, "GET"}, {SandboxEgressPath, "GET"}, {SandboxEventsPath, "GET"}, {SandboxSecurityEventsPath, "GET"}, {SandboxCheckpointsPath, "GET"}, {SandboxLogsPath, "GET"}, {SandboxMetricsPath, "GET"}, {SandboxMetricsLatestPath, "GET"}, {SandboxPreviewOpenPath, "POST"}, {SandboxPreviewClosePath, "POST"}, {SandboxPreviewListPath, "GET"}, {SandboxPreviewOperationPath, "GET"}}
	previewPermission := map[string]uint32{}
	for i, routeSpec := range paths {
		code := fmt.Sprintf("sandbox:details:test:%d", i)
		perm := db.Permission.Create().SetName(code).SetCode(code).SetStatus(permission.StatusOn).SaveX(sys)
		previewPermission[routeSpec.path] = perm.ID
		route := db.Api.Create().SetModule("SandboxService").SetPath(routeSpec.path).SetMethod(routeSpec.method).SetScope(api.ScopeAdmin).SetBusinessModule(api.BusinessModuleSandbox).SetStatus(api.StatusOn).SaveX(sys)
		db.PermissionApi.Create().SetPermissionID(perm.ID).SetAPIID(route.ID).SaveX(sys)
		db.RolePermission.Create().SetTenantID(ten.ID).SetRoleID(r.ID).SetPermissionID(perm.ID).SetStatus(rolepermission.StatusOn).SetEffect(rolepermission.EffectAllow).SaveX(sys)
	}
	ctx := viewer.WithContext(context.Background(), appViewer.NewUserViewer(0, uint64(ten.ID), 0, "", nil))
	authz := NewInstanceAuthorizationRepo(c)
	for _, routeSpec := range paths {
		require.ErrorIs(t, authz.Authorize(ctx, ten.ID, actor.ID, routeSpec.path, routeSpec.method), ErrActionAuthorizationDenied, routeSpec.path)
	}
	db.PlanModule.Create().SetPlanID(p.ID).SetModule(planmodule.ModuleSandbox).SaveX(sys)
	for _, routeSpec := range paths {
		require.NoError(t, authz.Authorize(ctx, ten.ID, actor.ID, routeSpec.path, routeSpec.method), routeSpec.path)
	}
	for _, routeSpec := range paths[len(paths)-4:] {
		db.Permission.UpdateOneID(previewPermission[routeSpec.path]).SetStatus(permission.StatusOff).SaveX(sys)
		require.ErrorIs(t, authz.Authorize(ctx, ten.ID, actor.ID, routeSpec.path, routeSpec.method), ErrActionAuthorizationDenied, routeSpec.path)
		db.Permission.UpdateOneID(previewPermission[routeSpec.path]).SetStatus(permission.StatusOn).SaveX(sys)
	}
}

func TestSandboxCheckpointCreateRequiresSandboxPlanAndExplicitGrant(t *testing.T) {
	c := testutil.NewEntClientForTest(t)
	sys := testutil.NewSystemViewerCtx(context.Background())
	db := c.Client()
	p := db.Plan.Create().SetExpiryPolicy(plan.ExpiryPolicyReadonly).SaveX(sys)
	ten := db.Tenant.Create().SetName("checkpoint-entitlement").SetStatus(tenant.StatusOn).SetPlanID(p.ID).SaveX(sys)
	db.PlanModule.Create().SetPlanID(p.ID).SetModule(planmodule.ModuleInstance).SaveX(sys)
	actor := db.User.Create().SetTenantID(ten.ID).SetUsername("checkpoint-user").SetStatus(user.StatusNormal).SaveX(sys)
	r := db.Role.Create().SetTenantID(ten.ID).SetName("checkpoint").SetCode("tenant:checkpoint").SetType(role.TypeTenant).SetStatus(role.StatusOn).SetDataScope(role.DataScopeAll).SaveX(sys)
	db.UserRole.Create().SetTenantID(ten.ID).SetUserID(actor.ID).SetRoleID(r.ID).SetStatus(userrole.StatusActive).SaveX(sys)
	permissions := make([]uint32, 0, 3)
	paths := []string{SandboxCheckpointCreatePath, SandboxCheckpointClonePath, SandboxCheckpointRestorePath}
	for i, path := range paths {
		code := fmt.Sprintf("sandbox:checkpoint:test:%d", i)
		perm := db.Permission.Create().SetName(code).SetCode(code).SetStatus(permission.StatusOn).SaveX(sys)
		route := db.Api.Create().SetModule("SandboxService").SetPath(path).SetMethod("POST").SetScope(api.ScopeAdmin).SetBusinessModule(api.BusinessModuleSandbox).SetStatus(api.StatusOn).SaveX(sys)
		db.PermissionApi.Create().SetPermissionID(perm.ID).SetAPIID(route.ID).SaveX(sys)
		permissions = append(permissions, perm.ID)
	}
	ctx := viewer.WithContext(context.Background(), appViewer.NewUserViewer(0, uint64(ten.ID), 0, "", nil))
	authz := NewInstanceAuthorizationRepo(c)
	for _, path := range paths {
		require.ErrorIs(t, authz.Authorize(ctx, ten.ID, actor.ID, path, "POST"), ErrActionAuthorizationDenied)
	}
	db.PlanModule.Create().SetPlanID(p.ID).SetModule(planmodule.ModuleSandbox).SaveX(sys)
	for _, path := range paths {
		require.ErrorIs(t, authz.Authorize(ctx, ten.ID, actor.ID, path, "POST"), ErrActionAuthorizationDenied)
	}
	for _, permID := range permissions {
		db.RolePermission.Create().SetTenantID(ten.ID).SetRoleID(r.ID).SetPermissionID(permID).SetStatus(rolepermission.StatusOn).SetEffect(rolepermission.EffectAllow).SaveX(sys)
	}
	for _, path := range paths {
		require.NoError(t, authz.Authorize(ctx, ten.ID, actor.ID, path, "POST"))
	}
}

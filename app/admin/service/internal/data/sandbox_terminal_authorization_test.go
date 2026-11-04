package data

import (
	"context"
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
)

// This exercises the real tenant, plan and current role authorization query.
func TestSandboxTerminalRequiresSandboxPlanAndTerminalPermission(t *testing.T) {
	c := testutil.NewEntClientForTest(t)
	ctx := testutil.NewSystemViewerCtx(context.Background())
	db := c.Client()
	p := db.Plan.Create().SetExpiryPolicy(plan.ExpiryPolicyReadonly).SaveX(ctx)
	tenantRow := db.Tenant.Create().SetName("terminal-tenant").SetStatus(tenant.StatusOn).SetPlanID(p.ID).SaveX(ctx)
	member := db.User.Create().SetTenantID(tenantRow.ID).SetUsername("terminal-user").SetStatus(user.StatusNormal).SaveX(ctx)
	r := db.Role.Create().SetTenantID(tenantRow.ID).SetName("terminal").SetCode("tenant:terminal-test").SetType(role.TypeTenant).SetStatus(role.StatusOn).SetDataScope(role.DataScopeAll).SaveX(ctx)
	db.UserRole.Create().SetTenantID(tenantRow.ID).SetUserID(member.ID).SetRoleID(r.ID).SetStatus(userrole.StatusActive).SaveX(ctx)
	permissionRow := db.Permission.Create().SetName("sandbox-terminal").SetCode("sandbox:terminal:connect").SetStatus(permission.StatusOn).SaveX(ctx)
	route := db.Api.Create().SetModule("SandboxService").SetPath(SandboxExecPath).SetMethod("POST").SetScope(api.ScopeAdmin).SetBusinessModule(api.BusinessModuleSandbox).SetStatus(api.StatusOn).SaveX(ctx)
	db.PermissionApi.Create().SetPermissionID(permissionRow.ID).SetAPIID(route.ID).SaveX(ctx)
	grant := db.RolePermission.Create().SetTenantID(tenantRow.ID).SetRoleID(r.ID).SetPermissionID(permissionRow.ID).SetStatus(rolepermission.StatusOn).SetEffect(rolepermission.EffectAllow).SaveX(ctx)
	authz := NewInstanceAuthorizationRepo(c)

	// A matching role grant cannot compensate for a missing plan entitlement.
	require.ErrorIs(t, authz.Authorize(ctx, tenantRow.ID, member.ID, SandboxExecPath, "POST"), ErrActionAuthorizationDenied)
	db.PlanModule.Create().SetPlanID(p.ID).SetModule(planmodule.ModuleInstance).SaveX(ctx)
	// An INSTANCE entitlement does not grant access to the independent Sandbox module.
	require.ErrorIs(t, authz.Authorize(ctx, tenantRow.ID, member.ID, SandboxExecPath, "POST"), ErrActionAuthorizationDenied)
	db.PlanModule.Create().SetPlanID(p.ID).SetModule(planmodule.ModuleSandbox).SaveX(ctx)
	require.NoError(t, authz.Authorize(ctx, tenantRow.ID, member.ID, SandboxExecPath, "POST"))
	require.ErrorIs(t, authz.Authorize(ctx, tenantRow.ID, member.ID, SandboxExecPath, "GET"), ErrActionAuthorizationDenied)

	other := db.Tenant.Create().SetName("other-tenant").SetStatus(tenant.StatusOn).SetPlanID(p.ID).SaveX(ctx)
	require.ErrorIs(t, authz.Authorize(ctx, other.ID, member.ID, SandboxExecPath, "POST"), ErrActionAuthorizationDenied)
	db.RolePermission.UpdateOneID(grant.ID).SetStatus(rolepermission.StatusOff).SaveX(ctx)
	require.ErrorIs(t, authz.Authorize(ctx, tenantRow.ID, member.ID, SandboxExecPath, "POST"), ErrActionAuthorizationDenied)
}

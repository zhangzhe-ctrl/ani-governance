package auth

import (
	"context"
	"testing"

	engine "go-wind-admin/pkg/localdeps/kratos-authz/engine"
	"go-wind-admin/pkg/localdeps/kratos-authz/engine/casbin"
	authz "go-wind-admin/pkg/localdeps/kratos-authz/middleware"
	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	"go-wind-admin/pkg/localdeps/go-utils/trans"
)

// TestAuthorizationTenantDomain 校验 casbin 策略的租户域隔离：策略里 V3 是租户域，
// 只有令牌租户与请求租户一致（且非平台租户 0）时才放行。路径本身与本用例无关，
// 取一个当前在跑的下游路由即可。
func TestAuthorizationTenantDomain(t *testing.T) {
	const op = "/api/v1/networks/vpcs/vpc_00000000000000000000000000000000"

	ctx := context.Background()
	ce, err := casbin.NewEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = ce.SetPolicies(ctx, engine.PolicyMap{"policies": []casbin.PolicyRule{{PType: "p", V0: "tenant:reader", V1: op, V2: "ANY", V3: "11"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tid := range []uint32{11, 12, 0} {
		ft := &fakeTransporter{hdr: fakeHeader{"X-Tenant-Id": "11"}, op: op}
		c, err := processAuthz(ctx, ft, &authenticationV1.UserTokenPayload{TenantId: trans.Ptr(tid), Roles: []string{"tenant:reader"}})
		if err != nil {
			t.Fatal(err)
		}
		claims, ok := authz.FromContext(c)
		if !ok || claims.Project == nil {
			t.Fatal("missing trusted domain")
		}
		allowed, err := ce.IsAuthorized(c, "tenant:reader", *claims.Action, *claims.Resource, *claims.Project)
		if err != nil || allowed != (tid == 11) {
			t.Fatalf("tenant %d allowed=%v err=%v", tid, allowed, err)
		}
	}
}

package auth

import (
	"context"
	"testing"

	"github.com/tx7do/go-utils/trans"
	engine "github.com/tx7do/kratos-authz/engine"
	"github.com/tx7do/kratos-authz/engine/casbin"
	authz "github.com/tx7do/kratos-authz/middleware"
	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
)

func TestModelAuthorizationTenantDomain(t *testing.T) {
	ctx := context.Background()
	ce, err := casbin.NewEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = ce.SetPolicies(ctx, engine.PolicyMap{"policies": []casbin.PolicyRule{{PType: "p", V0: "tenant:reader", V1: "/api/v1/models", V2: "ANY", V3: "11"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tid := range []uint32{11, 12, 0} {
		ft := &fakeTransporter{hdr: fakeHeader{"X-Tenant-Id": "11"}, op: "/api/v1/models"}
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

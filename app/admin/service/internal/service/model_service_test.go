package service

import (
	"context"
	"github.com/go-kratos/kratos/v2/errors"
	"github.com/tx7do/go-utils/trans"
	modelv1 "github.com/zhangzhe-ctrl/ani-model-service/api/model/v1"
	authv1 "go-wind-admin/api/gen/go/authentication/service/v1"
	catalogv1 "go-wind-admin/api/gen/go/catalog/service/v1"
	"go-wind-admin/pkg/middleware/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
)

type modelProbe struct {
	calls       int
	tenant      string
	user, limit uint32
	err         error
}

func (p *modelProbe) ListModels(_ context.Context, tenant string, user, limit uint32, _ string) (*modelv1.ListModelsResponse, error) {
	p.calls++
	p.tenant = tenant
	p.user = user
	p.limit = limit
	return &modelv1.ListModelsResponse{Models: []*modelv1.Model{{Id: "m1", ModelId: "external", Name: "fixture", TenantId: "private-tenant"}}}, p.err
}

type tenantProbe struct{ calls int }

func (p *tenantProbe) ResourceTenantID(_ context.Context, id uint32) (string, error) {
	p.calls++
	if id != 5 {
		return "", errors.Forbidden("BAD_TENANT", "unexpected tenant")
	}
	return "11111111-1111-4111-8111-111111111111", nil
}
func TestModelListTrustedIdentity(t *testing.T) {
	p, tenant := &modelProbe{}, &tenantProbe{}
	s := NewModelService(p, tenant)
	for _, ctx := range []context.Context{context.Background(), auth.NewContext(context.Background(), &authv1.UserTokenPayload{TenantId: trans.Ptr(uint32(0)), UserId: 7})} {
		if _, err := s.ListModels(ctx, &catalogv1.ListModelsRequest{}); err == nil {
			t.Fatal("untrusted identity accepted")
		}
	}
	if p.calls != 0 || tenant.calls != 0 {
		t.Fatal("rejected identity reached dependency")
	}
	ctx := auth.NewContext(context.Background(), &authv1.UserTokenPayload{TenantId: trans.Ptr(uint32(5)), UserId: 7})
	out, err := s.ListModels(ctx, &catalogv1.ListModelsRequest{})
	if err != nil || len(out.Models) != 1 || p.user != 7 || p.limit != 100 || p.tenant != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("mapping: %v %v %+v", out, err, p)
	}
	for _, n := range []uint32{0, 101} {
		_, err = s.ListModels(ctx, &catalogv1.ListModelsRequest{Limit: &n})
		if errors.Code(err) != 400 {
			t.Fatalf("limit %d: %v", n, err)
		}
	}
	for _, code := range []codes.Code{codes.Unauthenticated, codes.Unavailable, codes.Internal} {
		p.err = status.Error(code, "internal detail")
		_, err = s.ListModels(ctx, &catalogv1.ListModelsRequest{})
		if errors.Code(err) != 503 {
			t.Fatalf("internal failure became user error: %v", err)
		}
	}
	p.err = status.Error(codes.DeadlineExceeded, "slow")
	_, err = s.ListModels(ctx, &catalogv1.ListModelsRequest{})
	if errors.Code(err) != 504 {
		t.Fatalf("timeout: %v", err)
	}
}
func TestModelQueryContract(t *testing.T) {
	for _, q := range []string{"limit=1&limit=2", "tenant_id=a", "user_id=7", "cursor=x", "keyword=x", "source=x", "capability=x", "status=", "limit=", "%zz=x", "status=ready;other=x"} {
		if validateModelQuery(q) == nil {
			t.Errorf("accepted %q", q)
		}
	}
	for _, q := range []string{"", "limit=1", "status=ready&limit=100"} {
		if err := validateModelQuery(q); err != nil {
			t.Errorf("rejected %q: %v", q, err)
		}
	}
}

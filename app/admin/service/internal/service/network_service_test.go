package service

import (
	"context"
	"github.com/go-kratos/kratos/v2/errors"
	"github.com/tx7do/go-utils/trans"
	networkv1 "github.com/zhangzhe-ctrl/ani-network-service/api/network/v1"
	authv1 "go-wind-admin/api/gen/go/authentication/service/v1"
	catalogv1 "go-wind-admin/api/gen/go/catalog/service/v1"
	"go-wind-admin/pkg/middleware/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
)

type vpcProbe struct {
	calls int
	wrong bool
	err   error
}

func (p *vpcProbe) GetVPC(_ context.Context, tenant string, user uint32, id string) (*networkv1.GetVPCResponse, error) {
	p.calls++
	if user != 7 || tenant != "11111111-1111-4111-8111-111111111111" {
		panic("untrusted scope")
	}
	if p.wrong {
		tenant = "other"
	}
	return &networkv1.GetVPCResponse{Vpc: &networkv1.VPC{Id: id, TenantId: tenant, Name: "vpc", State: networkv1.ResourceState_RESOURCE_STATE_AVAILABLE}}, p.err
}

// tenantProbe 原先定义在已删除的 model_service_test.go 中，是 package service
// 共用的 ResourceTenantResolver 测试替身。
type tenantProbe struct{ calls int }

func (p *tenantProbe) ResourceTenantID(_ context.Context, id uint32) (string, error) {
	p.calls++
	if id != 5 {
		return "", errors.Forbidden("BAD_TENANT", "unexpected tenant")
	}
	return "11111111-1111-4111-8111-111111111111", nil
}
func TestNetworkTrustedScope(t *testing.T) {
	p, resolver := &vpcProbe{}, &tenantProbe{}
	s := NewNetworkService(p, resolver)
	req := &catalogv1.GetVPCRequest{VpcId: "vpc_11111111111111111111111111111111"}
	for _, ctx := range []context.Context{context.Background(), auth.NewContext(context.Background(), &authv1.UserTokenPayload{UserId: 7})} {
		if _, err := s.GetVPC(ctx, req); err == nil {
			t.Fatal("unauthenticated/platform accepted")
		}
	}
	if p.calls != 0 || resolver.calls != 0 {
		t.Fatal("rejected scope called dependencies")
	}
	ctx := auth.NewContext(context.Background(), &authv1.UserTokenPayload{TenantId: trans.Ptr(uint32(5)), UserId: 7})
	for _, id := range []string{"", "vpc_x", "vpc_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "../x"} {
		if _, err := s.GetVPC(ctx, &catalogv1.GetVPCRequest{VpcId: id}); errors.Code(err) != 400 {
			t.Fatalf("id %q: %v", id, err)
		}
	}
	if p.calls != 0 {
		t.Fatal("invalid ID called backend")
	}
	out, err := s.GetVPC(ctx, req)
	if err != nil || out.Vpc.State != "available" || p.calls != 1 {
		t.Fatalf("result %v %v", out, err)
	}
	p.wrong = true
	if _, err := s.GetVPC(ctx, req); errors.Code(err) != 503 {
		t.Fatalf("foreign response accepted: %v", err)
	}
	p.wrong = false
	for code, want := range map[codes.Code]int{codes.NotFound: 404, codes.InvalidArgument: 400, codes.PermissionDenied: 503, codes.Unauthenticated: 503, codes.Unavailable: 503, codes.DeadlineExceeded: 504} {
		p.err = status.Error(code, "private dependency detail")
		if _, err := s.GetVPC(ctx, req); errors.Code(err) != want {
			t.Fatalf("%v: %v", code, err)
		}
	}
	if _, err := NewNetworkService(nil, resolver).GetVPC(ctx, req); errors.Code(err) != 503 {
		t.Fatal("disabled dependency accepted")
	}
}

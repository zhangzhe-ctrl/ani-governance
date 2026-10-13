package data

import (
	"context"
	"testing"
	"time"

	modelv1 "github.com/liangzai006/ani-model-service/api/model/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type modelClientProbe struct {
	modelv1.ModelServiceClient
	calls int
}

func (p *modelClientProbe) CreateModel(ctx context.Context, request *modelv1.CreateModelRequest, _ ...grpc.CallOption) (*modelv1.Model, error) {
	p.calls++
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok || md.Get("x-ani-tenant-id")[0] != request.TenantId || md.Get("x-ani-actor")[0] != "governance:user:7" || len(md.Get("authorization")) != 0 {
		return nil, status.Error(codes.Internal, "identity metadata was not rebuilt")
	}
	if request.TenantId != "11111111-1111-4111-8111-111111111111" {
		return nil, status.Error(codes.Internal, "request tenant was not replaced")
	}
	return &modelv1.Model{TenantId: request.TenantId, Id: "model-1", Name: request.Name}, nil
}

func TestModelClientRebuildsTrustedIdentity(t *testing.T) {
	c := &ModelClient{client: &modelClientProbe{}, timeout: time.Second}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		"x-ani-tenant-id", "forged", "x-ani-actor", "forged", "authorization", "secret",
	))
	reply, err := c.CreateModel(ctx, "11111111-1111-4111-8111-111111111111", "governance:user:7", &modelv1.CreateModelRequest{TenantId: "body-tenant", Name: "qwen"})
	if err != nil {
		t.Fatal(err)
	}
	if reply == nil || reply.TenantId != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("unexpected reply: %#v", reply)
	}
	if c.client.(*modelClientProbe).calls != 1 {
		t.Fatal("model call was not made exactly once")
	}
}

func TestModelClientRejectsInvalidTrustedIdentity(t *testing.T) {
	c := &ModelClient{client: &modelClientProbe{}, timeout: time.Second}
	for _, tc := range []struct{ tenant, actor string }{
		{"", "governance:user:7"},
		{"11111111-1111-4111-8111-111111111111", "governance:user:0"},
		{"11111111-1111-4111-8111-111111111111", "governance:machine:7"},
	} {
		if _, err := c.CreateModel(context.Background(), tc.tenant, tc.actor, &modelv1.CreateModelRequest{}); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("identity (%q,%q) accepted: %v", tc.tenant, tc.actor, err)
		}
	}
	if _, _, err := NewModelClient(ModelClientConfig{}); err == nil {
		t.Fatal("insecure model client configuration accepted")
	}
}

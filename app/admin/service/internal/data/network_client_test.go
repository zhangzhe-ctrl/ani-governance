package data

import (
	"context"
	"github.com/google/uuid"
	networkv1 "github.com/zhangzhe-ctrl/ani-network-service/api/network/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"testing"
	"time"
)

type timedOutNetworkRPC struct{ networkv1.NetworkServiceClient }

func (timedOutNetworkRPC) GetVPC(context.Context, *networkv1.GetVPCRequest, ...grpc.CallOption) (*networkv1.GetVPCResponse, error) {
	return nil, status.Error(codes.DeadlineExceeded, "deadline")
}
func TestNetworkClientTransportOutage(t *testing.T) {
	for _, state := range []connectivity.State{connectivity.Ready, connectivity.Connecting, connectivity.TransientFailure} {
		c := &NetworkClient{client: timedOutNetworkRPC{}, timeout: time.Second, connectionState: func() connectivity.State { return state }}
		_, err := c.GetVPC(context.Background(), "11111111-1111-4111-8111-111111111111", "governance:user:7", "vpc_11111111111111111111111111111111")
		want := codes.Unavailable
		if state == connectivity.Ready {
			want = codes.DeadlineExceeded
		}
		if status.Code(err) != want {
			t.Fatalf("state %v: got %v want %v", state, err, want)
		}
	}
}

type networkRPCProbe struct {
	networkv1.NetworkServiceClient
	t    *testing.T
	wait bool
}

func (p networkRPCProbe) GetVPC(ctx context.Context, in *networkv1.GetVPCRequest, _ ...grpc.CallOption) (*networkv1.GetVPCResponse, error) {
	md, _ := metadata.FromOutgoingContext(ctx)
	if len(md.Get("evil")) != 0 || len(md.Get("x-ani-tenant-id")) != 1 || md.Get("x-ani-tenant-id")[0] != in.TenantId || md.Get("x-ani-actor")[0] != "governance:user:7" {
		p.t.Fatalf("metadata not rebuilt: %v", md)
	}
	if _, err := uuid.Parse(md.Get("x-ani-request-id")[0]); err != nil {
		p.t.Fatal(err)
	}
	if _, ok := ctx.Deadline(); !ok {
		p.t.Fatal("missing bounded deadline")
	}
	if p.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &networkv1.GetVPCResponse{}, nil
}
func TestNetworkClientIdentityAndCancellation(t *testing.T) {
	c := &NetworkClient{client: networkRPCProbe{t: t}, timeout: 20 * time.Millisecond}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("evil", "forwarded", "x-ani-tenant-id", "forged"))
	if _, err := c.GetVPC(ctx, "11111111-1111-4111-8111-111111111111", "governance:user:7", "vpc_11111111111111111111111111111111"); err != nil {
		t.Fatal(err)
	}
	c.client = networkRPCProbe{t: t, wait: true}
	if _, err := c.GetVPC(ctx, "11111111-1111-4111-8111-111111111111", "governance:user:7", "vpc_11111111111111111111111111111111"); err != context.DeadlineExceeded {
		t.Fatalf("timeout: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.GetVPC(canceled, "11111111-1111-4111-8111-111111111111", "governance:user:7", "vpc_11111111111111111111111111111111"); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
	if _, _, err := NewNetworkClient(NetworkClientConfig{}); err == nil {
		t.Fatal("missing TLS configuration accepted")
	}
}

type actorNetworkRPCProbe struct {
	networkv1.NetworkServiceClient
	actor string
	t     *testing.T
}

func (p actorNetworkRPCProbe) GetVPC(ctx context.Context, in *networkv1.GetVPCRequest, _ ...grpc.CallOption) (*networkv1.GetVPCResponse, error) {
	md, _ := metadata.FromOutgoingContext(ctx)
	if md.Get("x-ani-actor")[0] != p.actor || md.Get("x-ani-tenant-id")[0] != in.TenantId {
		p.t.Fatal("actor or tenant mismatch")
	}
	if len(md.Get("authorization")) != 0 || len(md.Get("x-signature")) != 0 {
		p.t.Fatal("public credentials forwarded")
	}
	return &networkv1.GetVPCResponse{}, nil
}
func TestNetworkClientKeyActor(t *testing.T) {
	c := &NetworkClient{client: actorNetworkRPCProbe{actor: "governance:access-key:42", t: t}, timeout: time.Second}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "secret", "x-signature", "secret"))
	if _, err := c.GetVPC(ctx, "11111111-1111-4111-8111-111111111111", "governance:access-key:42", "vpc_11111111111111111111111111111111"); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"governance:user:0", "governance:access-key:0", "governance:user:01", "governance:evil:1", "user:7"} {
		if _, err := c.GetVPC(ctx, "11111111-1111-4111-8111-111111111111", actor, "vpc_11111111111111111111111111111111"); err == nil {
			t.Fatalf("accepted %s", actor)
		}
	}
}

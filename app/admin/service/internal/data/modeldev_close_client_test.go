package data

import (
	"context"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	modeldevv1 "github.com/zhangzhe-ctrl/ani-modeldev-service/api/ani/modeldev/v1"
	trainingv1 "github.com/zhangzhe-ctrl/ani-modeldev-service/api/ani/training/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The controlled peer substitutes only the external ModelDev RPC boundary.
// This test proves sender transport and ACK handling, not ModelDev stop behavior.
func TestModelDevCloseClientDeliversSavedIntentWithFreshDelegation(t *testing.T) {
	intent, response := modelDevCloseClientFixture()
	type observation struct {
		request *modeldevv1.ApplyCloseIntentRequest
		md      metadata.MD
	}
	observed := make(chan observation, 2)
	config := startModelDevClosePeer(t, response, func(ctx context.Context, request *modeldevv1.ApplyCloseIntentRequest) (*modeldevv1.ApplyCloseIntentResponse, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		select {
		case observed <- observation{proto.Clone(request).(*modeldevv1.ApplyCloseIntentRequest), md.Copy()}:
		case <-ctx.Done():
			return nil, status.FromContextError(ctx.Err()).Err()
		}
		return proto.Clone(response).(*modeldevv1.ApplyCloseIntentResponse), nil
	})
	client := newModelDevBoundaryClient(t, config)
	poison := metadata.Pairs("x-ani-tenant-id", uuid.NewString(), "x-ani-actor", "governance:user:8",
		"x-ani-request-id", uuid.NewString(), "authorization", "private-close-sentinel", "cookie", "private-close-sentinel",
		"ani-workload-token", "private-close-sentinel", "ani-delegation", "private-close-sentinel",
		"x-ani-authorized-method", "forged", "x-ani-data-scope", "forged", "x-secret-injected", "private-close-sentinel")
	before := poison.Copy()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(metadata.NewIncomingContext(ctx, poison), poison)
	wantWire := &modeldevv1.ApplyCloseIntentRequest{
		Identity: &trainingv1.ExecutionIdentity{OperationId: intent.OperationID, ExecutionId: intent.ExecutionID, ExecutionSpecHash: intent.ExecutionSpecHash},
		ResourceTenantId: intent.ResourceTenantID, IntentGeneration: 7, Reason: modeldevv1.CloseReason_CLOSE_REASON_USER_STOP,
		RequestedAt: timestamppb.New(intent.RequestedAt), RequestedActorId: "governance:user:73",
	}
	lastID := ""
	for i := 0; i < 2; i++ {
		got, err := client.ApplyCloseIntent(ctx, intent)
		want := ModelDevCloseReceipt{OperationID: intent.OperationID, ExecutionID: intent.ExecutionID, ExecutionSpecHash: intent.ExecutionSpecHash,
			CloseGeneration: 19, CloseState: "CLOSING", Replayed: true}
		if err != nil || got != want {
			if err != nil && err.Error() == "modeldev close client not implemented" {
				t.Fatal("MODELDEV_CLOSE_CLIENT_BEHAVIOR: sender not implemented after real TLS preflight")
			}
			t.Fatal("sender did not preserve the durable owner receipt")
		}
		var seen observation
		select {
		case seen = <-observed:
		case <-ctx.Done():
			t.Fatal("sender returned without the bounded external RPC")
		}
		if !proto.Equal(seen.request, wantWire) {
			t.Error("sender changed the saved Stop identity, source generation, actor or time")
		}
		if !reflect.DeepEqual(seen.md.Get("x-ani-tenant-id"), []string{intent.ResourceTenantID}) || !reflect.DeepEqual(seen.md.Get("x-ani-actor"), []string{"governance:user:73"}) {
			t.Error("saved current Stop actor and tenant did not replace ambient delegation")
		}
		ids := seen.md.Get("x-ani-request-id")
		if len(ids) != 1 || !modelDevCanonicalUUID(ids[0]) || ids[0] == lastID || ids[0] == poison.Get("x-ani-request-id")[0] {
			t.Fatal("Stop delivery request identity is not fresh and canonical")
		}
		lastID = ids[0]
		for _, key := range []string{"authorization", "cookie", "ani-workload-token", "ani-delegation", "x-ani-authorized-method", "x-ani-data-scope", "x-secret-injected"} {
			if len(seen.md.Get(key)) != 0 {
				t.Error("ambient metadata reached the close peer")
			}
		}
	}
	incoming, _ := metadata.FromIncomingContext(ctx)
	outgoing, _ := metadata.FromOutgoingContext(ctx)
	if !reflect.DeepEqual(before, poison) || !reflect.DeepEqual(before, incoming) || !reflect.DeepEqual(before, outgoing) {
		t.Error("sender mutated caller metadata")
	}
}

func TestModelDevCloseClientRejectsUnsafeAcknowledgment(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*modeldevv1.ApplyCloseIntentResponse)
	}{
		{"missing identity", func(r *modeldevv1.ApplyCloseIntentResponse) { r.Identity = nil }},
		{"different operation", func(r *modeldevv1.ApplyCloseIntentResponse) { r.Identity.OperationId = uuid.NewString() }},
		{"different execution", func(r *modeldevv1.ApplyCloseIntentResponse) { r.Identity.ExecutionId = uuid.NewString() }},
		{"different hash", func(r *modeldevv1.ApplyCloseIntentResponse) { r.Identity.ExecutionSpecHash = strings.Repeat("b", 64) }},
		{"not durable", func(r *modeldevv1.ApplyCloseIntentResponse) { r.DurablyRecorded = false }},
		{"zero fence", func(r *modeldevv1.ApplyCloseIntentResponse) { r.CloseGeneration = 0 }},
		{"open", func(r *modeldevv1.ApplyCloseIntentResponse) { r.CloseState = modeldevv1.CloseState_CLOSE_STATE_OPEN }},
		{"unspecified", func(r *modeldevv1.ApplyCloseIntentResponse) { r.CloseState = modeldevv1.CloseState_CLOSE_STATE_UNSPECIFIED }},
		{"unknown state", func(r *modeldevv1.ApplyCloseIntentResponse) { r.CloseState = modeldevv1.CloseState(999) }},
		{"unknown response", func(r *modeldevv1.ApplyCloseIntentResponse) { r.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01}) }},
		{"unknown identity", func(r *modeldevv1.ApplyCloseIntentResponse) { r.Identity.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01}) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			intent, response := modelDevCloseClientFixture()
			bad := proto.Clone(response).(*modeldevv1.ApplyCloseIntentResponse)
			test.change(bad)
			config := startModelDevClosePeer(t, response, func(context.Context, *modeldevv1.ApplyCloseIntentRequest) (*modeldevv1.ApplyCloseIntentResponse, error) { return bad, nil })
			got, err := newModelDevBoundaryClient(t, config).ApplyCloseIntent(context.Background(), intent)
			var failure *ModelDevDeliveryFailure
			if got != (ModelDevCloseReceipt{}) || !errors.As(err, &failure) || failure.Code != "INVALID_ACK" || failure.Permanent {
				t.Fatal("MODELDEV_CLOSE_CLIENT_BEHAVIOR: unsafe ACK must remain retryable with no receipt")
			}
		})
	}
}

func modelDevCloseClientFixture() (ModelDevStopIntent, *modeldevv1.ApplyCloseIntentResponse) {
	intent := ModelDevStopIntent{TenantID: 42, ResourceTenantID: uuid.NewString(), OperationID: uuid.NewString(), ExecutionID: uuid.NewString(),
		ExecutionSpecHash: strings.Repeat("a", 64), Generation: 7, RequestedActor: "governance:user:73", RequestedAt: time.Date(2026, 10, 3, 1, 2, 3, 456000, time.UTC)}
	return intent, &modeldevv1.ApplyCloseIntentResponse{Identity: &trainingv1.ExecutionIdentity{OperationId: intent.OperationID, ExecutionId: intent.ExecutionID, ExecutionSpecHash: intent.ExecutionSpecHash},
		CloseGeneration: 19, CloseState: modeldevv1.CloseState_CLOSE_STATE_CLOSING, Replayed: true, DurablyRecorded: true}
}

type modelDevClosePeer struct {
	modeldevv1.UnimplementedModelDevCommandServiceServer
	controlID string
	baseline  *modeldevv1.ApplyCloseIntentResponse
	handle    func(context.Context, *modeldevv1.ApplyCloseIntentRequest) (*modeldevv1.ApplyCloseIntentResponse, error)
}

func (peer *modelDevClosePeer) ApplyCloseIntent(ctx context.Context, request *modeldevv1.ApplyCloseIntentRequest) (*modeldevv1.ApplyCloseIntentResponse, error) {
	if request.GetIdentity().GetOperationId() == peer.controlID {
		return proto.Clone(peer.baseline).(*modeldevv1.ApplyCloseIntentResponse), nil
	}
	return peer.handle(ctx, request)
}

func startModelDevClosePeer(t *testing.T, response *modeldevv1.ApplyCloseIntentResponse, handle func(context.Context, *modeldevv1.ApplyCloseIntentRequest) (*modeldevv1.ApplyCloseIntentResponse, error)) ModelDevClientConfig {
	t.Helper()
	config, security := modelDevBoundaryTLS(t, []string{"ani-modeldev-service"}, []string{"ani-governance"})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("MODELDEV_CLOSE_CLIENT_PREFLIGHT: TLS socket unavailable")
	}
	config.Address = listener.Addr().String()
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(security)))
	peer := &modelDevClosePeer{controlID: uuid.NewString(), baseline: response, handle: handle}
	modeldevv1.RegisterModelDevCommandServiceServer(server, peer)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
				t.Error("close TLS peer did not stop cleanly")
			}
		case <-time.After(2 * time.Second):
			t.Error("close TLS peer stop exceeded bound")
		}
	})
	connection := modelDevDirectTLSConnection(t, config, "ani-modeldev-service")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	control, err := modeldevv1.NewModelDevCommandServiceClient(connection).ApplyCloseIntent(ctx, &modeldevv1.ApplyCloseIntentRequest{Identity: &trainingv1.ExecutionIdentity{OperationId: peer.controlID}}, grpc.WaitForReady(true))
	if err != nil || !proto.Equal(control, response) {
		t.Fatal("MODELDEV_CLOSE_CLIENT_PREFLIGHT: generated TLS control failed; behavior NOT_RUN")
	}
	t.Log("MODELDEV_CLOSE_CLIENT_PREFLIGHT PASS: actual mTLS socket and generated control ACK; external RPC fixture, not owner stop logic")
	return config
}

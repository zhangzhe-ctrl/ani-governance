//go:build modeldev_pg && modeldev_original_retirement

package data

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	modeldevv1 "github.com/zhangzhe-ctrl/ani-modeldev-service/api/ani/modeldev/v1"
	trainingv1 "github.com/zhangzhe-ctrl/ani-modeldev-service/api/ani/training/v1"
	"go-wind-admin/app/admin/service/internal/data/ent/modeldevacceptance"
	appViewer "go-wind-admin/pkg/entgo/viewer"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
)

// Only the external owner RPC is substituted. Actual captured original bytes,
// Ent/PostgreSQL, queue claims, TLS sender, receipt validation and ACK commits run.
// The separate native MD Reserve tests establish owner-side zero send permits.
func originalRetirementFixture(t *testing.T) (context.Context, *ModelDevAcceptanceRepo) {
	t.Helper()
	client := newModelDevPGClient(t)
	ctx := appViewer.NewSystemViewerContext(context.Background())
	p := os.Getenv("OWNER_QUALIFICATION_PRIVATE_INPUT")
	info, e := os.Lstat(p)
	require.NoError(t, e)
	require.True(t, info.Mode().IsRegular() && info.Mode().Perm() == 0600)
	b, e := os.ReadFile(p)
	require.NoError(t, e)
	var capture struct {
		Governance struct {
			Acceptances []map[string]json.RawMessage `json:"acceptances"`
		} `json:"governance"`
	}
	require.NoError(t, json.Unmarshal(b, &capture))
	_, e = client.Client().Tenant.Create().SetID(2).SetName("original command retirement isolated PG").SetCode("original-command-retirement").SetResourceTenantID("64c5c35a-f554-432c-b4bc-7b4f36f08420").Save(ctx)
	require.NoError(t, e)
	t.Cleanup(func() {
		_, e := client.Client().ModelDevAcceptance.Delete().Where(modeldevacceptance.TenantIDEQ(2)).Exec(ctx)
		require.NoError(t, e)
		require.NoError(t, client.Client().Tenant.DeleteOneID(2).Exec(ctx))
	})
	n := 0
	for _, row := range capture.Governance.Acceptances {
		var id uint32
		require.NoError(t, json.Unmarshal(row["id"], &id))
		if _, ok := originalCommands[id]; !ok {
			continue
		}
		raw, e := json.Marshal(row)
		require.NoError(t, e)
		_, e = client.DB().ExecContext(ctx, `INSERT INTO sys_modeldev_acceptances SELECT (json_populate_record(NULL::sys_modeldev_acceptances,$1::json)).*`, raw)
		require.NoError(t, e)
		n++
	}
	require.Equal(t, 4, n)
	return ctx, NewModelDevAcceptanceRepo(client)
}
func originalRetirementProof(p OriginalCommandPlan) OriginalCommandProof {
	return OriginalCommandProof{Schema: "ani.modeldev.original-command-retirement-proof.v1", PlanSHA256: OriginalCommandPlanSHA(p), ConfigurationSHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", SourceProofSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", GovernanceSource: OriginalCommandMaintenanceSource, ObservedAt: time.Now().UTC(), Entry: "kubercloud@100.64.128.12", Context: "kubernetes-admin@ani-kcn-full", APIServer: "https://lb.kubesphere.local:6443", ClusterID: "f39bf6e2-f3ed-4191-ada3-caeebf86c220", KubeSystemUID: "3eeb5379-8748-4bf8-8f6d-d7a48ce3bc24", HTTPMethod: "POST", HTTPPath: "/api/v1/modeldev/executions", HTTPStatus: 503, HTTPReason: "MODELDEV_CREATE_MAINTENANCE", HTTPResponseSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", StopActor: "governance:user:1"}
}

type originalRetirementPeer struct {
	modeldevv1.UnimplementedModelDevCommandServiceServer
	mu       sync.Mutex
	stops    map[string]bool
	creates  int
	accepted []*modeldevv1.AcceptExecutionRequest
	omitACK  bool
}

func (p *originalRetirementPeer) ApplyCloseIntent(_ context.Context, r *modeldevv1.ApplyCloseIntentRequest) (*modeldevv1.ApplyCloseIntentResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stops[r.Identity.ExecutionId] = true
	return &modeldevv1.ApplyCloseIntentResponse{Identity: proto.Clone(r.Identity).(*trainingv1.ExecutionIdentity), CloseGeneration: 1, CloseState: modeldevv1.CloseState_CLOSE_STATE_CLOSING, DurablyRecorded: !p.omitACK}, nil
}
func (p *originalRetirementPeer) AcceptExecution(_ context.Context, r *modeldevv1.AcceptExecutionRequest) (*modeldevv1.AcceptExecutionResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.stops[r.Identity.ExecutionId] {
		return nil, errors.New("peer: stop must precede original create")
	}
	p.creates++
	p.accepted = append(p.accepted, proto.Clone(r).(*modeldevv1.AcceptExecutionRequest))
	return &modeldevv1.AcceptExecutionResponse{Identity: proto.Clone(r.Identity).(*trainingv1.ExecutionIdentity), States: &modeldevv1.ExecutionStates{ComputeState: modeldevv1.ComputeState_COMPUTE_STATE_ACCEPTED, DeliveryState: modeldevv1.DeliveryState_DELIVERY_STATE_PENDING, ResourceState: modeldevv1.ResourceState_RESOURCE_STATE_NOT_APPLICABLE, CloseState: modeldevv1.CloseState_CLOSE_STATE_CLOSING}, Revision: 2}, nil
}
func originalRetirementSender(t *testing.T, p *originalRetirementPeer) *ModelDevClient {
	t.Helper()
	config, security := modelDevBoundaryTLS(t, []string{"ani-modeldev-service"}, []string{"ani-governance"})
	l, e := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, e)
	config.Address = l.Addr().String()
	s := grpc.NewServer(grpc.Creds(credentials.NewTLS(security)))
	modeldevv1.RegisterModelDevCommandServiceServer(s, p)
	done := make(chan error, 1)
	go func() { done <- s.Serve(l) }()
	t.Cleanup(func() {
		s.Stop()
		select {
		case e := <-done:
			if e != nil && !errors.Is(e, grpc.ErrServerStopped) {
				t.Error("peer stop failed")
			}
		case <-time.After(2 * time.Second):
			t.Error("peer stop timed out")
		}
	})
	return newModelDevBoundaryClient(t, config)
}
func TestOriginalFourRetirementUsesOriginalStopAndCreateProtocols(t *testing.T) {
	ctx, r := originalRetirementFixture(t)
	peer := &originalRetirementPeer{stops: map[string]bool{}}
	sender := originalRetirementSender(t, peer)
	before, e := r.PlanOriginalCommandRetirement(ctx)
	require.NoError(t, e)
	for _, id := range []uint32{55, 57, 59, 60} {
		p, e := r.PlanOriginalCommandRetirement(ctx)
		require.NoError(t, e)
		proof := originalRetirementProof(p)
		_, e = r.DeliverOriginalCommand(ctx, p, id, proof, sender)
		require.Error(t, e, "no Create before durable original Stop ACK")
		stop, e := r.StopOriginalCommand(ctx, p, id, proof, sender)
		require.NoError(t, e)
		require.Equal(t, "CLOSING", stop.CloseState)
		p, e = r.PlanOriginalCommandRetirement(ctx)
		require.NoError(t, e)
		receipt, e := r.DeliverOriginalCommand(ctx, p, id, originalRetirementProof(p), sender)
		require.NoError(t, e)
		require.Equal(t, "CLOSING", receipt.CloseState)
		fresh, e := r.PlanOriginalCommandRetirement(ctx)
		require.NoError(t, e)
		_, e = r.DeliverOriginalCommand(ctx, fresh, id, originalRetirementProof(fresh), sender)
		require.Error(t, e, "ACKED command cannot send twice")
	}
	after, e := r.PlanOriginalCommandRetirement(ctx)
	require.NoError(t, e)
	peer.mu.Lock()
	defer peer.mu.Unlock()
	require.Equal(t, 4, peer.creates)
	for i, old := range before.Targets {
		got := after.Targets[i]
		require.Equal(t, old.Actor, got.Actor)
		require.Equal(t, old.AcceptedAt, got.AcceptedAt)
		require.Equal(t, old.IntentSHA256, got.IntentSHA256)
		require.Equal(t, old.SpecSHA256, got.SpecSHA256)
		require.Equal(t, "ACKED", got.DispatchState)
		row, e := r.entClient.Client().ModelDevAcceptance.Get(ctx, old.ID)
		require.NoError(t, e)
		accepted, e := modelDevAcceptanceFromRow(row)
		require.NoError(t, e)
		want, e := modelDevDispatchRequest(accepted.Envelope())
		require.NoError(t, e)
		require.True(t, proto.Equal(want, peer.accepted[i]), "sender must preserve complete original canonical command/time")
	}
}
func TestOriginalRetirementRefusesMissingACKDriftScopeAndLiveLease(t *testing.T) {
	ctx, r := originalRetirementFixture(t)
	peer := &originalRetirementPeer{stops: map[string]bool{}, omitACK: true}
	sender := originalRetirementSender(t, peer)
	p, e := r.PlanOriginalCommandRetirement(ctx)
	require.NoError(t, e)
	_, e = r.StopOriginalCommand(ctx, p, 55, originalRetirementProof(p), sender)
	require.Error(t, e)
	p, e = r.PlanOriginalCommandRetirement(ctx)
	require.NoError(t, e)
	_, e = r.DeliverOriginalCommand(ctx, p, 55, originalRetirementProof(p), sender)
	require.Error(t, e)
	_, e = r.ClaimOriginalCommandRetirement(ctx, p, 74, originalRetirementProof(p))
	require.Error(t, e)
	bad := originalRetirementProof(p)
	bad.ObservedAt = time.Now().Add(-3 * time.Minute)
	_, e = r.ClaimOriginalCommandRetirement(ctx, p, 57, bad)
	require.Error(t, e)
	_, e = r.entClient.Client().ModelDevAcceptance.UpdateOneID(57).SetLastErrorCode("COMMAND_CONFLICT").Save(ctx)
	require.NoError(t, e)
	_, e = r.ClaimOriginalCommandRetirement(ctx, p, 57, originalRetirementProof(p))
	require.Error(t, e)
	peer.mu.Lock()
	require.Zero(t, peer.creates)
	peer.mu.Unlock()
}

func TestOriginalRetirementRefusesOtherActiveDeliveryLease(t *testing.T) {
	ctx, r := originalRetirementFixture(t)
	peer := &originalRetirementPeer{stops: map[string]bool{}}
	sender := originalRetirementSender(t, peer)
	p, e := r.PlanOriginalCommandRetirement(ctx)
	require.NoError(t, e)
	_, e = r.StopOriginalCommand(ctx, p, 55, originalRetirementProof(p), sender)
	require.NoError(t, e)
	_, e = r.entClient.Client().ModelDevAcceptance.UpdateOneID(57).SetDispatchState(modeldevacceptance.DispatchStateDISPATCHING).SetRetryBlocked(false).ClearLastErrorCode().SetLeaseOwner("other-active").SetLeaseGeneration(1).SetAttemptCount(1).SetLeaseUntil(time.Now().UTC().Add(time.Minute)).Save(ctx)
	require.NoError(t, e)
	p, e = r.PlanOriginalCommandRetirement(ctx)
	require.NoError(t, e)
	_, e = r.ClaimOriginalCommandRetirement(ctx, p, 55, originalRetirementProof(p))
	require.ErrorContains(t, e, "LIVE_LEASES")
	peer.mu.Lock()
	require.Zero(t, peer.creates)
	peer.mu.Unlock()
}

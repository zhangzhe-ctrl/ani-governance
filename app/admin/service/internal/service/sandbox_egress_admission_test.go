package service

import (
	"context"
	"testing"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/stretchr/testify/require"
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	view "go-wind-admin/api/gen/go/sandbox/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/pkg/middleware/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

type racingEgressAdmission struct {
	sandboxTestAdmission
	reads int
}

func (a *racingEgressAdmission) FindSandboxCreateIntent(_ context.Context, _ uint32, id string) (*data.SandboxCreateIntent, error) {
	a.reads++
	if a.reads == 1 {
		return nil, nil
	}
	return a.intents[id], nil
}

func TestRestrictedEgressRejectsNewWithLegacyResource(t *testing.T) {
	for _, policy := range []string{"allowlist", "deny_all"} {
		t.Run(policy, func(t *testing.T) {
			s, client, ctx, request := sandboxReplayFixture()
			request.Data.EgressIntent = &view.SandboxEgressIntent{NetworkEgressPolicy: policy}
			_, err := s.CreateSandbox(ctx, request)
			require.Equal(t, codes.Unimplemented, status.Code(err))
			require.Equal(t, "SANDBOX_EGRESS_ENFORCEMENT_UNSUPPORTED", status.Convert(err).Message())
			require.Empty(t, s.admission.(*sandboxTestAdmission).intents)
			require.Zero(t, client.resolveCalls)
			require.Zero(t, client.acceptCalls)

			owner := &cloneOwner{sandboxTestClient: &sandboxTestClient{resolved: &res.ResolveCreateSpecResponse{Name: "legacy-accepts"}}}
			admission := new(sandboxTestAdmission)
			cloneService := NewSandboxService(owner, new(cloneAuth), sandboxTenantResolver{}, admission)
			cloneCtx := auth.NewPrincipalContext(context.Background(), &auth.Principal{Type: auth.SubjectUser, ID: 7, TenantID: 42})
			clone := &view.CloneSandboxCheckpointRequest{Data: &view.CloneSandboxCheckpointData{SourceSandboxId: "33333333-3333-4333-8333-333333333333", CheckpointId: "44444444-4444-4444-8444-444444444444", IdempotencyKey: "egress-clone", ExpectedVersion: 1, Name: "target", PresetId: sandboxTestPreset, ImageReference: "example/image:tag", EgressIntent: &view.SandboxEgressIntent{NetworkEgressPolicy: policy}}}
			_, err = cloneService.CloneSandboxCheckpoint(cloneCtx, clone)
			require.Equal(t, codes.Unimplemented, status.Code(err))
			require.Equal(t, "SANDBOX_EGRESS_ENFORCEMENT_UNSUPPORTED", status.Convert(err).Message())
			require.Empty(t, admission.intents)
			require.Zero(t, owner.resolveCalls)
			require.Empty(t, owner.requests)
		})
	}
}

func TestRestrictedEgressExistingSandboxReplaysBeforeAdmissionGuard(t *testing.T) {
	s, client, ctx, request := sandboxReplayFixture()
	request.Data.EgressIntent = &view.SandboxEgressIntent{NetworkEgressPolicy: "allowlist", EgressAllowlist: []string{"api.example"}}
	input := &res.CreateSandboxData{Name: request.Data.Name, ImageReference: request.Data.ImageReference, PresetId: request.Data.PresetId, EgressIntent: &res.SandboxEgressIntent{NetworkEgressPolicy: "allowlist", EgressAllowlist: []string{"api.example"}}}
	digest, err := res.CreateIntentDigest("", input)
	require.NoError(t, err)
	resourceID := stableSandboxID(sandboxTestTenant, request.Data.IdempotencyKey, "resource")
	operationID := stableSandboxID(sandboxTestTenant, request.Data.IdempotencyKey, "operation")
	client.stored = &res.Sandbox{TenantId: sandboxTestTenant, Id: resourceID, CreateOperationId: operationID, CreateIntentDigest: digest}
	client.resolveErr = status.Error(codes.Unavailable, "must not re-resolve")
	out, err := s.CreateSandbox(ctx, request)
	require.NoError(t, err)
	require.True(t, out.Replayed)
	require.Zero(t, client.resolveCalls)
	require.Zero(t, client.acceptCalls)
}

func TestRestrictedEgressConcurrentJournalReplayOrConflict(t *testing.T) {
	for _, matching := range []bool{true, false} {
		t.Run(map[bool]string{true: "same_command", false: "different_command"}[matching], func(t *testing.T) {
			s, client, ctx, request := sandboxReplayFixture()
			request.Data.EgressIntent = &view.SandboxEgressIntent{NetworkEgressPolicy: "deny_all"}
			input := &res.CreateSandboxData{Name: request.Data.Name, ImageReference: request.Data.ImageReference, PresetId: request.Data.PresetId, EgressIntent: &res.SandboxEgressIntent{NetworkEgressPolicy: "deny_all"}}
			intentDigest, err := res.CreateIntentDigest("", input)
			require.NoError(t, err)
			resourceID := stableSandboxID(sandboxTestTenant, request.Data.IdempotencyKey, "resource")
			operationID := stableSandboxID(sandboxTestTenant, request.Data.IdempotencyKey, "operation")
			spec := &res.ResolveCreateSpecResponse{Name: input.Name, ImageReference: input.ImageReference, PresetId: input.PresetId, Workspace: &res.WorkspaceSpec{Size: "10Gi"}, EgressIntent: input.EgressIntent}
			command := &res.AcceptCreateRequest{Context: &res.CommandContext{TenantId: sandboxTestTenant, ResourceId: resourceID, OperationId: operationID}, Spec: spec, CreateIntentDigest: intentDigest}
			command.Context.RequestDigest, err = res.CommandDigest(command.Context, "", spec, intentDigest)
			require.NoError(t, err)
			raw, err := protojson.Marshal(command)
			require.NoError(t, err)
			if !matching {
				intentDigest = "different"
			}
			intent := &data.SandboxCreateIntent{TenantID: 42, ResourceTenantID: sandboxTestTenant, SandboxID: resourceID, OperationID: operationID, IntentDigest: intentDigest, Actor: "governance:user:7", Command: raw}
			ledger := &racingEgressAdmission{sandboxTestAdmission: sandboxTestAdmission{intents: map[string]*data.SandboxCreateIntent{resourceID: intent}}}
			s.admission = ledger
			out, err := s.CreateSandbox(ctx, request)
			require.Equal(t, 2, ledger.reads)
			require.Zero(t, client.resolveCalls)
			if matching {
				require.Nil(t, out)
				require.Equal(t, codes.Unavailable, status.Code(err))
				require.Equal(t, "SANDBOX_EGRESS_OUTCOME_UNCONFIRMED", status.Convert(err).Message())
				require.False(t, ledger.intents[resourceID].Acknowledged)
				require.Zero(t, client.acceptCalls)
				worker := &SandboxDispatchWorker{sandbox: s}
				err = worker.deliver(ctx, intent)
				require.Equal(t, codes.Unavailable, status.Code(err))
				require.Equal(t, "SANDBOX_EGRESS_OUTCOME_UNCONFIRMED", status.Convert(err).Message())
				require.False(t, intent.Acknowledged)
				require.Zero(t, client.acceptCalls)
				// A later read-only observation can prove this same command was
				// accepted before rollout; the worker may then acknowledge it.
				client.stored = &res.Sandbox{TenantId: sandboxTestTenant, Id: resourceID, CreateOperationId: operationID, CreateIntentDigest: intentDigest}
				err = worker.deliver(ctx, intent)
				require.NoError(t, err)
				require.True(t, intent.Acknowledged)
				require.Zero(t, client.acceptCalls)
			} else {
				require.EqualValues(t, 409, kratoserrors.Code(err))
				require.Zero(t, client.acceptCalls)
			}
		})
	}
}

func TestRestrictedEgressUnconfirmedCloneIsNotDispatched(t *testing.T) {
	owner := &cloneOwner{sandboxTestClient: &sandboxTestClient{}}
	admission := new(sandboxTestAdmission)
	svc := NewSandboxService(owner, new(cloneAuth), sandboxTenantResolver{}, admission)
	ctx := auth.NewPrincipalContext(context.Background(), &auth.Principal{Type: auth.SubjectUser, ID: 7, TenantID: 42})
	request := &view.CloneSandboxCheckpointRequest{Data: &view.CloneSandboxCheckpointData{SourceSandboxId: "33333333-3333-4333-8333-333333333333", CheckpointId: "44444444-4444-4444-8444-444444444444", IdempotencyKey: "pending-restricted", ExpectedVersion: 1, Name: "target", PresetId: sandboxTestPreset, ImageReference: "example/image:tag", EgressIntent: &view.SandboxEgressIntent{NetworkEgressPolicy: "deny_all"}}}
	input := &res.CreateSandboxData{Name: request.Data.Name, ImageReference: request.Data.ImageReference, PresetId: request.Data.PresetId, EgressIntent: &res.SandboxEgressIntent{NetworkEgressPolicy: "deny_all"}}
	digest, err := res.CloneIntentDigest(request.Data.SourceSandboxId, request.Data.CheckpointId, request.Data.ExpectedVersion, input)
	require.NoError(t, err)
	resourceID := stableSandboxID(sandboxTestTenant, request.Data.IdempotencyKey, "checkpoint-clone-resource")
	operationID := stableSandboxID(sandboxTestTenant, request.Data.IdempotencyKey, "checkpoint-clone-operation")
	spec := &res.ResolveCreateSpecResponse{Name: input.Name, ImageReference: input.ImageReference, PresetId: input.PresetId, Workspace: &res.WorkspaceSpec{Size: "10Gi"}, EgressIntent: input.EgressIntent}
	command := &res.AcceptSandboxCloneRequest{Context: &res.CommandContext{TenantId: sandboxTestTenant, ResourceId: resourceID, OperationId: operationID}, Spec: spec, SourceSandboxId: request.Data.SourceSandboxId, CheckpointId: request.Data.CheckpointId, ExpectedVersion: request.Data.ExpectedVersion, CreateIntentDigest: digest}
	command.Context.RequestDigest, err = res.CloneCommandDigest(command.Context, command.SourceSandboxId, command.CheckpointId, command.ExpectedVersion, spec, digest)
	require.NoError(t, err)
	raw, err := protojson.Marshal(command)
	require.NoError(t, err)
	intent := &data.SandboxCreateIntent{TenantID: 42, ResourceTenantID: sandboxTestTenant, SandboxID: resourceID, OperationID: operationID, IntentDigest: digest, Actor: "governance:user:7", Command: raw, CommandKind: "checkpoint_clone"}
	admission.intents = map[string]*data.SandboxCreateIntent{resourceID: intent}
	worker := &SandboxDispatchWorker{sandbox: svc}
	err = worker.deliver(ctx, intent)
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Equal(t, "SANDBOX_EGRESS_OUTCOME_UNCONFIRMED", status.Convert(err).Message())
	_, err = svc.CloneSandboxCheckpoint(ctx, request)
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Equal(t, "SANDBOX_EGRESS_OUTCOME_UNCONFIRMED", status.Convert(err).Message())
	require.Empty(t, owner.requests)
	require.False(t, intent.Acknowledged)
	intent.Acknowledged = true
	out, err := svc.CloneSandboxCheckpoint(ctx, request)
	require.NoError(t, err)
	require.True(t, out.Replayed)
	require.Empty(t, owner.requests)
}

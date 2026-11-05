package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	view "go-wind-admin/api/gen/go/sandbox/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/pkg/middleware/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type cloneOwner struct {
	*sandboxTestClient
	requests []*res.AcceptSandboxCloneRequest
	fail     bool
	reject   error
}

func (c *cloneOwner) AcceptSandboxClone(_ context.Context, tenant, actor string, req *res.AcceptSandboxCloneRequest) (*res.AcceptSandboxCloneResponse, error) {
	if tenant != sandboxTestTenant || actor != "governance:user:7" || req.Context.TenantId != tenant {
		panic("untrusted clone scope")
	}
	c.requests = append(c.requests, proto.Clone(req).(*res.AcceptSandboxCloneRequest))
	if c.reject != nil {
		return nil, c.reject
	}
	if c.fail {
		return nil, status.Error(codes.Unavailable, "SANDBOX_DEPENDENCY_UNAVAILABLE")
	}
	return &res.AcceptSandboxCloneResponse{Ack: &res.OperationAck{ResourceId: req.Context.ResourceId, OperationId: req.Context.OperationId, Accepted: true, Replayed: len(c.requests) > 1}}, nil
}

type cloneAuth struct {
	deny  string
	calls []string
}

func (a *cloneAuth) Authorize(_ context.Context, _, _ uint32, path, method string) error {
	a.calls = append(a.calls, method+" "+path)
	if path == a.deny {
		return data.ErrActionAuthorizationDenied
	}
	return nil
}
func TestCloneUsesBothPermissionsAndRetriesFrozenCommand(t *testing.T) {
	owner := &cloneOwner{sandboxTestClient: &sandboxTestClient{resolved: &res.ResolveCreateSpecResponse{Name: "target", ImageReference: "example/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PresetId: sandboxTestPreset, Workspace: &res.WorkspaceSpec{Size: "10Gi"}}}, fail: true}
	authorizer := new(cloneAuth)
	admission := new(sandboxTestAdmission)
	svc := NewSandboxService(owner, authorizer, sandboxTenantResolver{}, admission)
	ctx := auth.NewPrincipalContext(context.Background(), &auth.Principal{Type: auth.SubjectUser, ID: 7, TenantID: 42})
	r := &view.CloneSandboxCheckpointRequest{Data: &view.CloneSandboxCheckpointData{SourceSandboxId: "33333333-3333-4333-8333-333333333333", CheckpointId: "44444444-4444-4444-8444-444444444444", IdempotencyKey: "clone-1", ExpectedVersion: 2, Name: "target", PresetId: sandboxTestPreset, ImageReference: "example/image:tag"}}
	_, err := svc.CloneSandboxCheckpoint(ctx, r)
	require.Error(t, err)
	require.Len(t, owner.requests, 1)
	require.Contains(t, authorizer.calls, "POST "+data.SandboxCheckpointClonePath)
	require.Contains(t, authorizer.calls, "POST "+data.SandboxCreatePath)
	require.Equal(t, 1, owner.resolveCalls)
	require.False(t, admission.intents[owner.requests[0].Context.ResourceId].Acknowledged)
	owner.fail = false
	out, err := svc.CloneSandboxCheckpoint(ctx, r)
	require.NoError(t, err)
	require.True(t, out.Accepted)
	require.True(t, out.Replayed)
	require.Len(t, owner.requests, 2)
	require.True(t, proto.Equal(owner.requests[0], owner.requests[1]))
	require.Equal(t, 1, owner.resolveCalls, "replay must not re-resolve the catalogue")
	require.NotEqual(t, r.Data.SourceSandboxId, out.ResourceId)
	require.True(t, admission.intents[out.ResourceId].Acknowledged)
	r.Data.CheckpointId = "55555555-5555-4555-8555-555555555555"
	_, err = svc.CloneSandboxCheckpoint(ctx, r)
	require.Error(t, err)
	require.Len(t, owner.requests, 2)
}

func TestCloneCannotBypassCreateOrSourcePermission(t *testing.T) {
	for _, path := range []string{data.SandboxCreatePath, data.SandboxCheckpointClonePath} {
		owner := &cloneOwner{sandboxTestClient: new(sandboxTestClient)}
		authorizer := &cloneAuth{deny: path}
		svc := NewSandboxService(owner, authorizer, sandboxTenantResolver{}, new(sandboxTestAdmission))
		ctx := auth.NewPrincipalContext(context.Background(), &auth.Principal{Type: auth.SubjectUser, ID: 7, TenantID: 42})
		_, err := svc.CloneSandboxCheckpoint(ctx, &view.CloneSandboxCheckpointRequest{Data: &view.CloneSandboxCheckpointData{SourceSandboxId: "33333333-3333-4333-8333-333333333333", CheckpointId: "44444444-4444-4444-8444-444444444444", IdempotencyKey: "k", ExpectedVersion: 1, Name: "target", PresetId: sandboxTestPreset, ImageReference: "example/image:tag"}})
		require.Error(t, err)
		require.Empty(t, owner.requests)
		require.Zero(t, owner.resolveCalls)
	}
}

func TestCloneRequiresWiringBeforeNewIntentButReplaysAcknowledged(t *testing.T) {
	ctx := auth.NewPrincipalContext(context.Background(), &auth.Principal{Type: auth.SubjectUser, ID: 7, TenantID: 42})
	r := &view.CloneSandboxCheckpointRequest{Data: &view.CloneSandboxCheckpointData{SourceSandboxId: "33333333-3333-4333-8333-333333333333", CheckpointId: "44444444-4444-4444-8444-444444444444", IdempotencyKey: "wiring", ExpectedVersion: 1, Name: "target", PresetId: sandboxTestPreset, ImageReference: "example/image:tag"}}
	admission := new(sandboxTestAdmission)
	base := &sandboxTestClient{resolved: &res.ResolveCreateSpecResponse{Name: "target", ImageReference: "example/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PresetId: sandboxTestPreset, Workspace: &res.WorkspaceSpec{Size: "10Gi"}}}
	withoutClone := NewSandboxService(base, new(cloneAuth), sandboxTenantResolver{}, admission)
	_, err := withoutClone.CloneSandboxCheckpoint(ctx, r)
	require.Error(t, err)
	require.Empty(t, admission.intents)
	require.Zero(t, base.resolveCalls)

	withClone := NewSandboxService(&cloneOwner{sandboxTestClient: base}, new(cloneAuth), sandboxTenantResolver{}, admission)
	first, err := withClone.CloneSandboxCheckpoint(ctx, r)
	require.NoError(t, err)
	replay, err := withoutClone.CloneSandboxCheckpoint(ctx, r)
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.Equal(t, first.OperationId, replay.OperationId)
	require.Equal(t, first.ResourceId, replay.ResourceId)
}

func TestSandboxOperationReasonIsExplicitlyAllowlisted(t *testing.T) {
	require.Equal(t, "", sandboxPublicOperationReason(""))
	require.Equal(t, "SANDBOX_TRANSFER_OUTCOME_UNKNOWN", sandboxPublicOperationReason("SANDBOX_TRANSFER_OUTCOME_UNKNOWN"))
	for _, reason := range []string{"SANDBOX_TRANSFER_WFFC_UNSUPPORTED", "CSI_SNAPSHOT_LAST_ERROR", "SANDBOX_CHECKPOINT_PROVIDER_ACCESS_DENIED", "SANDBOX_CHECKPOINT_PROVIDER_IDENTITY_MISMATCH", "CHECKPOINT_IDENTITY_INCOMPLETE", "CHECKPOINT_PROVIDER_MISSING", "CHECKPOINT_IDENTITY_MISMATCH", "CHECKPOINT_PROVIDER_DELETING", "CHECKPOINT_PROVIDER_NOT_READY", "CHECKPOINT_REASON_UNAVAILABLE"} {
		require.Equal(t, reason, sandboxPublicOperationReason(reason))
	}
	require.Equal(t, "SANDBOX_OPERATION_REASON_UNAVAILABLE", sandboxPublicOperationReason("csi://secret-volume"))
	require.Equal(t, "SANDBOX_OPERATION_REASON_UNAVAILABLE", sandboxPublicOperationReason("SANDBOX_UNREVIEWED_SECRET"))
}

func TestCloneDurableWFFCRejectionStopsReplayAfterConfigurationChanges(t *testing.T) {
	owner := &cloneOwner{sandboxTestClient: &sandboxTestClient{resolved: &res.ResolveCreateSpecResponse{Name: "target", ImageReference: "example/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PresetId: sandboxTestPreset, Workspace: &res.WorkspaceSpec{Size: "10Gi"}}}, reject: status.Error(codes.Unimplemented, "SANDBOX_TRANSFER_WFFC_UNSUPPORTED")}
	admission := new(sandboxTestAdmission)
	svc := NewSandboxService(owner, new(cloneAuth), sandboxTenantResolver{}, admission)
	ctx := auth.NewPrincipalContext(context.Background(), &auth.Principal{Type: auth.SubjectUser, ID: 7, TenantID: 42})
	r := &view.CloneSandboxCheckpointRequest{Data: &view.CloneSandboxCheckpointData{SourceSandboxId: "33333333-3333-4333-8333-333333333333", CheckpointId: "44444444-4444-4444-8444-444444444444", IdempotencyKey: "wffc-reject", ExpectedVersion: 2, Name: "target", PresetId: sandboxTestPreset, ImageReference: "example/image:tag"}}
	_, err := svc.CloneSandboxCheckpoint(ctx, r)
	require.Equal(t, codes.Unimplemented, status.Code(err))
	require.Equal(t, "SANDBOX_TRANSFER_WFFC_UNSUPPORTED", status.Convert(err).Message())
	require.Len(t, owner.requests, 1)
	intent := admission.intents[owner.requests[0].Context.ResourceId]
	require.True(t, intent.Acknowledged)
	require.Equal(t, "SANDBOX_TRANSFER_WFFC_UNSUPPORTED", intent.LastErrorCode)
	owner.reject = nil
	_, err = svc.CloneSandboxCheckpoint(ctx, r)
	require.Equal(t, codes.Unimplemented, status.Code(err))
	require.Len(t, owner.requests, 1, "durably rejected command must not dispatch after config change or restart")
}

func TestCloneRejectionDoesNotReleaseUnknownIntent(t *testing.T) {
	owner := &cloneOwner{sandboxTestClient: &sandboxTestClient{resolved: &res.ResolveCreateSpecResponse{Name: "target", ImageReference: "example/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PresetId: sandboxTestPreset, Workspace: &res.WorkspaceSpec{Size: "10Gi"}}}, reject: status.Error(codes.FailedPrecondition, "SANDBOX_VERSION_CONFLICT")}
	admission := new(sandboxTestAdmission)
	svc := NewSandboxService(owner, new(cloneAuth), sandboxTenantResolver{}, admission)
	ctx := auth.NewPrincipalContext(context.Background(), &auth.Principal{Type: auth.SubjectUser, ID: 7, TenantID: 42})
	r := &view.CloneSandboxCheckpointRequest{Data: &view.CloneSandboxCheckpointData{SourceSandboxId: "33333333-3333-4333-8333-333333333333", CheckpointId: "44444444-4444-4444-8444-444444444444", IdempotencyKey: "reject", ExpectedVersion: 1, Name: "target", PresetId: sandboxTestPreset, ImageReference: "example/image:tag"}}
	_, err := svc.CloneSandboxCheckpoint(ctx, r)
	require.Error(t, err)
	require.Len(t, owner.requests, 1)
	require.False(t, admission.intents[owner.requests[0].Context.ResourceId].Acknowledged)
	owner.reject = status.Error(codes.NotFound, "SANDBOX_NOT_FOUND")
	_, err = svc.CloneSandboxCheckpoint(ctx, r)
	require.Error(t, err)
	require.False(t, admission.intents[owner.requests[0].Context.ResourceId].Acknowledged, "generic not-found does not prove a committed clone rejection")
}

func TestCloneFreezesEgressIntent(t *testing.T) {
	want := &res.SandboxEgressIntent{NetworkEgressPolicy: "internet"}
	base := &sandboxTestClient{resolved: &res.ResolveCreateSpecResponse{Name: "target", ImageReference: "example/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PresetId: sandboxTestPreset, Workspace: &res.WorkspaceSpec{Size: "10Gi"}, EgressIntent: want}}
	owner := &cloneOwner{sandboxTestClient: base}
	admission := new(sandboxTestAdmission)
	svc := NewSandboxService(owner, new(cloneAuth), sandboxTenantResolver{}, admission)
	ctx := auth.NewPrincipalContext(context.Background(), &auth.Principal{Type: auth.SubjectUser, ID: 7, TenantID: 42})
	r := &view.CloneSandboxCheckpointRequest{Data: &view.CloneSandboxCheckpointData{SourceSandboxId: "33333333-3333-4333-8333-333333333333", CheckpointId: "44444444-4444-4444-8444-444444444444", IdempotencyKey: "egress", ExpectedVersion: 1, Name: "target", PresetId: sandboxTestPreset, ImageReference: "example/image:tag", EgressIntent: &view.SandboxEgressIntent{NetworkEgressPolicy: "internet"}}}
	out, err := svc.CloneSandboxCheckpoint(ctx, r)
	require.NoError(t, err)
	require.Equal(t, "not_applied_by_sandbox", out.IntentEnforcement)
	require.True(t, proto.Equal(want, base.resolveRequest.Data.EgressIntent))
	require.True(t, proto.Equal(want, owner.requests[0].Spec.EgressIntent))
	r.Data.EgressIntent.NetworkEgressPolicy = "deny_all"
	_, err = svc.CloneSandboxCheckpoint(ctx, r)
	require.Error(t, err)
	require.Len(t, owner.requests, 1)
}

func TestCloneRejectsChangedResolvedEgressIntent(t *testing.T) {
	base := &sandboxTestClient{resolved: &res.ResolveCreateSpecResponse{Name: "target", PresetId: sandboxTestPreset, Workspace: &res.WorkspaceSpec{Size: "10Gi"}}}
	owner := &cloneOwner{sandboxTestClient: base}
	admission := new(sandboxTestAdmission)
	svc := NewSandboxService(owner, new(cloneAuth), sandboxTenantResolver{}, admission)
	ctx := auth.NewPrincipalContext(context.Background(), &auth.Principal{Type: auth.SubjectUser, ID: 7, TenantID: 42})
	r := &view.CloneSandboxCheckpointRequest{Data: &view.CloneSandboxCheckpointData{SourceSandboxId: "33333333-3333-4333-8333-333333333333", CheckpointId: "44444444-4444-4444-8444-444444444444", IdempotencyKey: "egress-mismatch", ExpectedVersion: 1, Name: "target", PresetId: sandboxTestPreset, ImageReference: "example/image:tag", EgressIntent: &view.SandboxEgressIntent{NetworkEgressPolicy: "internet"}}}
	_, err := svc.CloneSandboxCheckpoint(ctx, r)
	require.Error(t, err)
	require.Empty(t, admission.intents)
	require.Empty(t, owner.requests)
}

func TestCloneDedicatedRejectionReceiptsEndIntentAndReplay(t *testing.T) {
	for _, tc := range []struct {
		reason string
		code   codes.Code
	}{
		{"SANDBOX_CLONE_REJECTED_NOT_FOUND", codes.NotFound},
		{"SANDBOX_CLONE_REJECTED_VERSION", codes.FailedPrecondition},
		{"SANDBOX_CLONE_REJECTED_CONFLICT", codes.FailedPrecondition},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			owner := &cloneOwner{sandboxTestClient: &sandboxTestClient{resolved: &res.ResolveCreateSpecResponse{Name: "target", ImageReference: "example/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PresetId: sandboxTestPreset, Workspace: &res.WorkspaceSpec{Size: "10Gi"}}}, reject: status.Error(tc.code, tc.reason)}
			admission := new(sandboxTestAdmission)
			svc := NewSandboxService(owner, new(cloneAuth), sandboxTenantResolver{}, admission)
			ctx := auth.NewPrincipalContext(context.Background(), &auth.Principal{Type: auth.SubjectUser, ID: 7, TenantID: 42})
			r := &view.CloneSandboxCheckpointRequest{Data: &view.CloneSandboxCheckpointData{SourceSandboxId: "33333333-3333-4333-8333-333333333333", CheckpointId: "44444444-4444-4444-8444-444444444444", IdempotencyKey: "dedicated", ExpectedVersion: 1, Name: "target", PresetId: sandboxTestPreset, ImageReference: "example/image:tag"}}
			_, err := svc.CloneSandboxCheckpoint(ctx, r)
			require.Equal(t, tc.code, status.Code(err))
			require.Equal(t, tc.reason, status.Convert(err).Message())
			intent := admission.intents[owner.requests[0].Context.ResourceId]
			require.True(t, intent.Acknowledged)
			require.Equal(t, tc.reason, intent.LastErrorCode)
			owner.reject = nil
			_, err = svc.CloneSandboxCheckpoint(ctx, r)
			require.Equal(t, tc.code, status.Code(err))
			require.Equal(t, tc.reason, status.Convert(err).Message())
			require.Len(t, owner.requests, 1)
		})
	}
}

func TestCloneWrongReceiptCodeRemainsPending(t *testing.T) {
	owner := &cloneOwner{sandboxTestClient: &sandboxTestClient{resolved: &res.ResolveCreateSpecResponse{Name: "target", ImageReference: "example/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PresetId: sandboxTestPreset, Workspace: &res.WorkspaceSpec{Size: "10Gi"}}}, reject: status.Error(codes.Unavailable, "SANDBOX_CLONE_REJECTED_VERSION")}
	admission := new(sandboxTestAdmission)
	svc := NewSandboxService(owner, new(cloneAuth), sandboxTenantResolver{}, admission)
	ctx := auth.NewPrincipalContext(context.Background(), &auth.Principal{Type: auth.SubjectUser, ID: 7, TenantID: 42})
	r := &view.CloneSandboxCheckpointRequest{Data: &view.CloneSandboxCheckpointData{SourceSandboxId: "33333333-3333-4333-8333-333333333333", CheckpointId: "44444444-4444-4444-8444-444444444444", IdempotencyKey: "bad-code", ExpectedVersion: 1, Name: "target", PresetId: sandboxTestPreset, ImageReference: "example/image:tag"}}
	_, err := svc.CloneSandboxCheckpoint(ctx, r)
	require.Error(t, err)
	require.False(t, admission.intents[owner.requests[0].Context.ResourceId].Acknowledged)
}

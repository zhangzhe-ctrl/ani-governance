package service

// Candidate only: depends on Resource's atomic AcceptSandboxClone contract,
// canonical digest helpers and the matching dispatcher migration. Do not wire
// or deploy until the cross-service transaction and recovery tests pass.
import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	view "go-wind-admin/api/gen/go/sandbox/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type sandboxCloneClient interface {
	AcceptSandboxClone(context.Context, string, string, *res.AcceptSandboxCloneRequest) (*res.AcceptSandboxCloneResponse, error)
}
type sandboxCloneRejectionLedger interface {
	RejectSandboxClone(context.Context, uint32, string, string, string) error
}

func (s *SandboxService) CloneSandboxCheckpoint(ctx context.Context, r *view.CloneSandboxCheckpointRequest) (*view.SandboxAcceptance, error) {
	if r == nil || r.Data == nil || !sandboxUUID(r.Data.SourceSandboxId) || !sandboxUUID(r.Data.CheckpointId) || r.Data.ExpectedVersion < 1 || r.Data.Name == "" || r.Data.PresetId == "" || sandboxKey(r.Data.IdempotencyKey) != nil || !utf8.ValidString(r.Data.IdempotencyKey) || strings.ContainsRune(r.Data.IdempotencyKey, 0) {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid checkpoint clone request")
	}
	if r.Data.ImageId != "" || r.Data.ImageScope != "" {
		if r.Data.ImageId == "" || (r.Data.ImageScope != "tenant" && r.Data.ImageScope != "platform") || r.Data.ImageReference != "" {
			return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "image_id and image_scope must identify one registered image")
		}
	} else if r.Data.ImageReference == "" {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "registered image is required")
	}
	tenant, actor, err := s.scope(ctx, data.SandboxCheckpointClonePath, "POST")
	if err != nil {
		return nil, err
	}
	p, err := gpuPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	// Reading a checkpoint's contents to create a new Sandbox requires both
	// permissions; the clone route alone cannot bypass create authorization.
	if err = instanceAuthorizationError(s.authorization.Authorize(ctx, p.TenantID, p.ID, data.SandboxCreatePath, "POST")); err != nil {
		return nil, err
	}
	if s.admission == nil {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "sandbox admission is not configured")
	}
	input := &res.CreateSandboxData{Name: r.Data.Name, ImageId: r.Data.ImageId, ImageScope: r.Data.ImageScope, ImageReference: r.Data.ImageReference, PresetId: r.Data.PresetId, WorkspaceSize: r.Data.WorkspaceSize}
	if r.Data.EgressIntent != nil {
		candidate := &res.SandboxEgressIntent{NetworkEgressPolicy: r.Data.EgressIntent.NetworkEgressPolicy, EgressAllowlist: append([]string{}, r.Data.EgressIntent.EgressAllowlist...)}
		input.EgressIntent, err = res.NormalizeEgressIntent(candidate)
		if err != nil {
			return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid sandbox egress intent")
		}
	}
	intentDigest, err := res.CloneIntentDigest(r.Data.SourceSandboxId, r.Data.CheckpointId, r.Data.ExpectedVersion, input)
	if err != nil {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid clone intent")
	}
	resourceID := stableSandboxID(tenant, r.Data.IdempotencyKey, "checkpoint-clone-resource")
	operationID := stableSandboxID(tenant, r.Data.IdempotencyKey, "checkpoint-clone-operation")
	if resourceID == r.Data.SourceSandboxId {
		return nil, kratoserrors.Conflict("SANDBOX_IDEMPOTENCY_CONFLICT", "clone destination conflicts with source")
	}
	intent, err := s.admission.FindSandboxCreateIntent(ctx, p.TenantID, resourceID)
	if err != nil {
		return nil, err
	}
	if intent != nil {
		if intent.CommandKind != "checkpoint_clone" || intent.ResourceTenantID != tenant || intent.OperationID != operationID || intent.IntentDigest != intentDigest {
			return nil, kratoserrors.Conflict("SANDBOX_IDEMPOTENCY_CONFLICT", "clone intent changed")
		}
		return s.deliverSandboxClone(ctx, intent, true)
	}
	// Reject absent local wiring before making a new durable intent. Previously
	// acknowledged intents can be replayed even during a partial rollout.
	if _, ok := s.client.(sandboxCloneClient); !ok {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "sandbox clone is not configured")
	}
	// Another request with this key may register while this request is still
	// resolving or while the registration response is lost. Recover only the
	// matching durable command and never fall back to ordinary Sandbox create.
	replayIntent := func(cause error) (*view.SandboxAcceptance, error) {
		if ctx.Err() != nil {
			return nil, cause
		}
		existing, lookupErr := s.admission.FindSandboxCreateIntent(ctx, p.TenantID, resourceID)
		if lookupErr != nil {
			return nil, lookupErr
		}
		if existing == nil {
			return nil, cause
		}
		if existing.CommandKind != "checkpoint_clone" || existing.ResourceTenantID != tenant || existing.OperationID != operationID || existing.IntentDigest != intentDigest {
			return nil, kratoserrors.Conflict("SANDBOX_IDEMPOTENCY_CONFLICT", "clone intent changed")
		}
		return s.deliverSandboxClone(ctx, existing, true)
	}
	if err := rejectUnsupportedSandboxEgress(input.EgressIntent); err != nil {
		return replayIntent(err)
	}
	resolved, err := s.client.ResolveSandbox(ctx, tenant, actor, &res.ResolveCreateSpecRequest{TenantId: tenant, ContractVersion: 1, Data: input})
	if err != nil {
		return replayIntent(err)
	}
	if resolved == nil || !proto.Equal(resolved.EgressIntent, input.EgressIntent) {
		return replayIntent(kratoserrors.ServiceUnavailable("SANDBOX_INVALID_RESPONSE", "invalid sandbox resolution"))
	}
	command := &res.CommandContext{TenantId: tenant, ResourceId: resourceID, OperationId: operationID}
	command.RequestDigest, err = res.CloneCommandDigest(command, r.Data.SourceSandboxId, r.Data.CheckpointId, r.Data.ExpectedVersion, resolved, intentDigest)
	if err != nil {
		return nil, kratoserrors.InternalServer("SANDBOX_DIGEST_FAILED", "clone command digest failed")
	}
	req := &res.AcceptSandboxCloneRequest{Context: command, Spec: resolved, SourceSandboxId: r.Data.SourceSandboxId, CheckpointId: r.Data.CheckpointId, ExpectedVersion: r.Data.ExpectedVersion, CreateIntentDigest: intentDigest}
	raw, err := protojson.Marshal(req)
	if err != nil {
		return replayIntent(err)
	}
	intent, replayed, err := s.admission.RegisterSandboxCreateIntent(ctx, data.SandboxCreateIntent{TenantID: p.TenantID, ResourceTenantID: tenant, SandboxID: resourceID, OperationID: operationID, IntentDigest: intentDigest, Actor: actor, Command: raw, CommandKind: "checkpoint_clone"})
	if err != nil {
		return replayIntent(err)
	}
	return s.deliverSandboxClone(ctx, intent, replayed)
}

// The persisted command is delivered with exactly the same tenant, target,
// operation and source on every attempt. A timeout never clears the journal.
func (s *SandboxService) deliverSandboxClone(ctx context.Context, intent *data.SandboxCreateIntent, replay bool) (*view.SandboxAcceptance, error) {
	if intent == nil || intent.CommandKind != "checkpoint_clone" || intent.InstanceID != "" {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_INTENT", "clone intent missing")
	}
	var req res.AcceptSandboxCloneRequest
	if err := protojson.Unmarshal(intent.Command, &req); err != nil {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_INTENT", "invalid clone intent")
	}
	if req.Context == nil || req.Spec == nil || req.Context.TenantId != intent.ResourceTenantID || req.Context.ResourceId != intent.SandboxID || req.Context.OperationId != intent.OperationID || req.CreateIntentDigest != intent.IntentDigest || !sandboxUUID(req.SourceSandboxId) || !sandboxUUID(req.CheckpointId) || req.ExpectedVersion < 1 {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_INTENT", "clone intent identity mismatch")
	}
	digest, err := res.CloneCommandDigest(req.Context, req.SourceSandboxId, req.CheckpointId, req.ExpectedVersion, req.Spec, req.CreateIntentDigest)
	if err != nil || digest != req.Context.RequestDigest {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_INTENT", "clone intent digest mismatch")
	}
	if intent.Acknowledged {
		if rejected := sandboxCloneRejection(intent.LastErrorCode); rejected != nil {
			return nil, rejected
		}
		if intent.LastErrorCode != "" {
			return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_INTENT", "unknown clone terminal outcome")
		}
		return &view.SandboxAcceptance{OperationId: intent.OperationID, ResourceId: intent.SandboxID, Accepted: true, Replayed: true, IntentEnforcement: sandboxEgressIntentEnforcement(req.Spec.EgressIntent)}, nil
	}
	// No read-only clone proof is currently available. Keep an unconfirmed
	// restricted historical intent pending instead of sending it to an older
	// Resource that might create an unrestricted Sandbox.
	if err := rejectUnsupportedSandboxEgress(req.Spec.EgressIntent); err != nil {
		return nil, status.Error(codes.Unavailable, "SANDBOX_EGRESS_OUTCOME_UNCONFIRMED")
	}
	client, ok := s.client.(sandboxCloneClient)
	if !ok {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "sandbox clone is not configured")
	}
	out, err := client.AcceptSandboxClone(ctx, intent.ResourceTenantID, intent.Actor, &req)
	if err != nil {
		st, ok := status.FromError(err)
		if ok && sandboxCloneRejection(st.Message()) != nil && status.Code(sandboxCloneRejection(st.Message())) == st.Code() {
			ledger, ok := s.admission.(sandboxCloneRejectionLedger)
			if !ok {
				return nil, kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "sandbox clone rejection could not be recorded")
			}
			recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if recordErr := ledger.RejectSandboxClone(recordCtx, intent.TenantID, intent.SandboxID, intent.OperationID, st.Message()); recordErr != nil {
				return nil, kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "sandbox clone rejection could not be recorded")
			}
		}
		return nil, err
	}
	if out == nil || out.Ack == nil || !out.Ack.Accepted || out.Ack.ResourceId != intent.SandboxID || out.Ack.OperationId != intent.OperationID {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_ACK", "invalid clone acceptance")
	}
	ackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.admission.AcknowledgeSandboxCreate(ackCtx, intent.TenantID, intent.SandboxID, intent.OperationID); err != nil {
		return nil, err
	}
	return &view.SandboxAcceptance{OperationId: intent.OperationID, ResourceId: intent.SandboxID, Accepted: true, Replayed: replay || out.Ack.Replayed, IntentEnforcement: sandboxEgressIntentEnforcement(req.Spec.EgressIntent)}, nil
}

// Only exact reason/code pairs are trusted as committed Resource receipts.
func sandboxCloneRejection(reason string) error {
	switch reason {
	case "SANDBOX_TRANSFER_WFFC_UNSUPPORTED":
		return status.Error(codes.Unimplemented, reason)
	case "SANDBOX_CLONE_REJECTED_NOT_FOUND":
		return status.Error(codes.NotFound, reason)
	case "SANDBOX_CLONE_REJECTED_VERSION", "SANDBOX_CLONE_REJECTED_CONFLICT":
		return status.Error(codes.FailedPrecondition, reason)
	default:
		return nil
	}
}

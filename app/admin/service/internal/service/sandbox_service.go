package service

import (
	"context"
	"strconv"
	"time"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/google/uuid"
	instancepb "github.com/zhangzhe-ctrl/ani-resource-service/api/instance/v1"
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	admin "go-wind-admin/api/gen/go/admin/service/v1"
	view "go-wind-admin/api/gen/go/sandbox/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type SandboxOwnerClient interface {
	ResolveSandbox(context.Context, string, string, *res.ResolveCreateSpecRequest) (*res.ResolveCreateSpecResponse, error)
	AcceptSandboxCreate(context.Context, string, string, *res.AcceptCreateRequest) (*res.AcceptCreateResponse, error)
	GetSandbox(context.Context, string, string, *res.GetSandboxRequest) (*res.GetSandboxResponse, error)
	ListSandboxes(context.Context, string, string, *res.ListSandboxesRequest) (*res.ListSandboxesResponse, error)
	GetSandboxOperation(context.Context, string, string, *res.GetOperationRequest) (*res.GetOperationResponse, error)
	PauseSandbox(context.Context, string, string, *res.PauseRequest) (*res.PauseResponse, error)
	ResumeSandbox(context.Context, string, string, *res.ResumeRequest) (*res.ResumeResponse, error)
	DeleteSandbox(context.Context, string, string, *res.DeleteRequest) (*res.DeleteResponse, error)
}

type SandboxParentAdmission interface {
	CanCreateSandbox(context.Context, string) error
	FindSandboxCreateIntent(context.Context, uint32, string) (*data.SandboxCreateIntent, error)
	RegisterSandboxCreateIntent(context.Context, data.SandboxCreateIntent) (*data.SandboxCreateIntent, bool, error)
	AcknowledgeSandboxCreate(context.Context, uint32, string, string) error
}

func (s *InstanceService) FindSandboxCreateIntent(ctx context.Context, tenant uint32, id string) (*data.SandboxCreateIntent, error) {
	if s == nil || s.ledger == nil {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "instance admission is not configured")
	}
	return s.ledger.FindSandboxCreateIntent(ctx, tenant, id)
}
func (s *InstanceService) RegisterSandboxCreateIntent(ctx context.Context, in data.SandboxCreateIntent) (*data.SandboxCreateIntent, bool, error) {
	if s == nil || s.ledger == nil {
		return nil, false, kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "instance admission is not configured")
	}
	return s.ledger.RegisterSandboxCreateIntent(ctx, in)
}
func (s *InstanceService) AcknowledgeSandboxCreate(ctx context.Context, tenant uint32, id, operation string) error {
	if s == nil || s.ledger == nil {
		return kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "instance admission is not configured")
	}
	return s.ledger.AcknowledgeSandboxCreate(ctx, tenant, id, operation)
}

// CanCreateSandbox reuses the Instance admission fence, including a durable
// DELETE accepted by Governance but not yet delivered to Resource.
func (s *InstanceService) CanCreateSandbox(ctx context.Context, instanceID string) error {
	if s == nil || s.ledger == nil {
		return kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "instance admission is not configured")
	}
	return s.canOperate(ctx, instanceID)
}

type SandboxService struct {
	admin.UnimplementedSandboxServiceServer
	client        SandboxOwnerClient
	parent        InstanceOwnerClient
	authorization InstanceActionAuthorizer
	tenants       ResourceTenantResolver
	admission     SandboxParentAdmission
}

func NewSandboxService(client SandboxOwnerClient, parent InstanceOwnerClient, authorization InstanceActionAuthorizer, tenants ResourceTenantResolver, admission SandboxParentAdmission) *SandboxService {
	return &SandboxService{client: client, parent: parent, authorization: authorization, tenants: tenants, admission: admission}
}

func (s *SandboxService) scope(ctx context.Context, path, method, instanceID string) (string, string, *instancepb.Instance, error) {
	p, err := gpuPrincipal(ctx)
	if err != nil {
		return "", "", nil, err
	}
	if s.client == nil || s.parent == nil || s.authorization == nil || s.tenants == nil {
		return "", "", nil, kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "sandbox service is not configured")
	}
	if err = instanceAuthorizationError(s.authorization.Authorize(ctx, p.TenantID, p.ID, path, method)); err != nil {
		return "", "", nil, err
	}
	tenant, err := s.tenants.ResourceTenantID(ctx, p.TenantID)
	if err != nil {
		return "", "", nil, err
	}
	actor := "governance:user:" + strconv.FormatUint(uint64(p.ID), 10)
	parent, err := s.parent.Get(ctx, tenant, actor, instanceID)
	if err != nil {
		return "", "", nil, err
	}
	if parent == nil || parent.Id != instanceID {
		return "", "", nil, kratoserrors.NotFound("INSTANCE_NOT_FOUND", "instance not found")
	}
	return tenant, actor, parent, nil
}

func (s *SandboxService) admitCreate(ctx context.Context, instanceID string, parent *instancepb.Instance) error {
	if parent == nil || parent.Id != instanceID {
		return kratoserrors.NotFound("INSTANCE_NOT_FOUND", "instance not found")
	}
	for _, state := range []string{parent.DesiredState, parent.ObservedState} {
		if state == "deleted" || state == "deleting" {
			return kratoserrors.Conflict("INSTANCE_DELETING", "instance final deletion already accepted")
		}
	}
	if s.admission == nil {
		return kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "instance admission is not configured")
	}
	return s.admission.CanCreateSandbox(ctx, instanceID)
}

func sandboxKey(v string) error {
	if len(v) == 0 || len([]rune(v)) > 256 {
		return kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "idempotency_key is required")
	}
	return nil
}

func stableSandboxID(tenant, key, kind string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("ani-governance/sandbox/"+kind+"/"+tenant+"/"+key)).String()
}

func (s *SandboxService) CreateSandbox(ctx context.Context, r *view.CreateSandboxRequest) (*view.SandboxAcceptance, error) {
	if r == nil || r.Data == nil || r.Data.InstanceId == "" || r.Data.Name == "" || r.Data.ImageReference == "" || r.Data.PresetId == "" {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "sandbox data is required")
	}
	if err := sandboxKey(r.Data.IdempotencyKey); err != nil {
		return nil, err
	}
	tenant, actor, parent, err := s.scope(ctx, data.SandboxCreatePath, "POST", r.Data.InstanceId)
	if err != nil {
		return nil, err
	}
	resourceID := stableSandboxID(tenant, r.Data.IdempotencyKey, "resource")
	operationID := stableSandboxID(tenant, r.Data.IdempotencyKey, "operation")
	input := &res.CreateSandboxData{Name: r.Data.Name, ImageReference: r.Data.ImageReference, PresetId: r.Data.PresetId, WorkspaceSize: r.Data.WorkspaceSize}
	intentDigest, err := res.CreateIntentDigest(r.Data.InstanceId, input)
	if err != nil {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid sandbox create intent")
	}
	p, err := gpuPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if s.admission == nil {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "sandbox admission is not configured")
	}
	intent, err := s.admission.FindSandboxCreateIntent(ctx, p.TenantID, resourceID)
	if err != nil {
		return nil, err
	}
	if intent != nil {
		if intent.ResourceTenantID != tenant || intent.InstanceID != r.Data.InstanceId || intent.OperationID != operationID || intent.IntentDigest != intentDigest {
			return nil, kratoserrors.Conflict("SANDBOX_IDEMPOTENCY_CONFLICT", "sandbox create intent changed")
		}
		return s.deliverSandboxCreate(ctx, intent, true)
	}
	previous, err := s.client.GetSandbox(ctx, tenant, actor, &res.GetSandboxRequest{TenantId: tenant, InstanceId: r.Data.InstanceId, SandboxId: resourceID})
	if err == nil {
		return sandboxCreateReplay(previous, r.Data.InstanceId, resourceID, operationID, input, intentDigest)
	}
	if status.Code(err) != codes.NotFound {
		return nil, err
	}
	// Another delivery can be accepted after the absent read, including before
	// a catalog failure or a lost acceptance response. Only a matching durable
	// record can turn that failed attempt into a replay.
	replayAccepted := func(cause error) (*view.SandboxAcceptance, error) {
		if ctx.Err() != nil {
			return nil, cause
		}
		previous, lookupErr := s.client.GetSandbox(ctx, tenant, actor, &res.GetSandboxRequest{TenantId: tenant, InstanceId: r.Data.InstanceId, SandboxId: resourceID})
		if lookupErr == nil {
			return sandboxCreateReplay(previous, r.Data.InstanceId, resourceID, operationID, input, intentDigest)
		}
		if status.Code(lookupErr) != codes.NotFound {
			return nil, lookupErr
		}
		return nil, cause
	}
	if err = s.admitCreate(ctx, r.Data.InstanceId, parent); err != nil {
		return replayAccepted(err)
	}
	resolved, err := s.client.ResolveSandbox(ctx, tenant, actor, &res.ResolveCreateSpecRequest{TenantId: tenant, InstanceId: r.Data.InstanceId, ContractVersion: 1, Data: input})
	if err != nil {
		return replayAccepted(err)
	}
	if resolved == nil {
		return replayAccepted(kratoserrors.ServiceUnavailable("SANDBOX_INVALID_RESPONSE", "invalid sandbox resolution"))
	}
	command := &res.CommandContext{TenantId: tenant, ResourceId: resourceID, OperationId: operationID}
	command.RequestDigest, err = res.CommandDigest(command, r.Data.InstanceId, resolved, intentDigest)
	if err != nil {
		return nil, kratoserrors.InternalServer("SANDBOX_DIGEST_FAILED", "sandbox command digest failed")
	}
	// Recheck after resolution so an already-visible parent DELETE cannot admit
	// a new child. This check and the owner RPC are not an atomic cross-service lock.
	parent, err = s.parent.Get(ctx, tenant, actor, r.Data.InstanceId)
	if err != nil {
		return replayAccepted(err)
	}
	if err = s.admitCreate(ctx, r.Data.InstanceId, parent); err != nil {
		return replayAccepted(err)
	}
	request := &res.AcceptCreateRequest{Context: command, Spec: resolved, InstanceId: r.Data.InstanceId, CreateIntentDigest: intentDigest}
	raw, err := protojson.Marshal(request)
	if err != nil {
		return nil, err
	}
	intent, replayed, err := s.admission.RegisterSandboxCreateIntent(ctx, data.SandboxCreateIntent{TenantID: p.TenantID, ResourceTenantID: tenant, InstanceID: r.Data.InstanceId, SandboxID: resourceID, OperationID: operationID, IntentDigest: intentDigest, Actor: actor, Command: raw})
	if err != nil {
		return replayAccepted(err)
	}
	return s.deliverSandboxCreate(ctx, intent, replayed)
}

func (s *SandboxService) deliverSandboxCreate(ctx context.Context, intent *data.SandboxCreateIntent, replay bool) (*view.SandboxAcceptance, error) {
	if intent == nil {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_INTENT", "sandbox intent missing")
	}
	var req res.AcceptCreateRequest
	if err := protojson.Unmarshal(intent.Command, &req); err != nil {
		return nil, err
	}
	if req.Context == nil || req.Spec == nil || req.Context.TenantId != intent.ResourceTenantID || req.Context.ResourceId != intent.SandboxID || req.Context.OperationId != intent.OperationID || req.InstanceId != intent.InstanceID || req.CreateIntentDigest != intent.IntentDigest {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_INTENT", "sandbox intent identity mismatch")
	}
	digest, err := res.CommandDigest(req.Context, req.InstanceId, req.Spec, req.CreateIntentDigest)
	if err != nil || digest != req.Context.RequestDigest {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_INTENT", "sandbox intent digest mismatch")
	}
	lookup := func() (*view.SandboxAcceptance, error) {
		previous, lookupErr := s.client.GetSandbox(ctx, intent.ResourceTenantID, intent.Actor, &res.GetSandboxRequest{TenantId: intent.ResourceTenantID, InstanceId: intent.InstanceID, SandboxId: intent.SandboxID})
		if lookupErr != nil {
			return nil, lookupErr
		}
		if previous == nil || previous.Sandbox == nil || previous.Sandbox.TenantId != intent.ResourceTenantID || previous.Sandbox.Id != intent.SandboxID || previous.Sandbox.InstanceId != intent.InstanceID || previous.Sandbox.CreateOperationId != intent.OperationID {
			return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_RESPONSE", "invalid sandbox response")
		}
		stored := previous.Sandbox
		matches := stored.CreateIntentDigest == intent.IntentDigest
		if stored.CreateIntentDigest == "" && stored.Snapshot != nil && stored.Snapshot.Workspace != nil {
			for _, size := range []string{"", stored.Snapshot.Workspace.Size} {
				legacy, e := res.CreateIntentDigest(intent.InstanceID, &res.CreateSandboxData{Name: stored.Snapshot.Name, ImageReference: stored.Snapshot.ImageReference, PresetId: stored.Snapshot.PresetId, WorkspaceSize: size})
				matches = matches || e == nil && legacy == intent.IntentDigest
			}
		}
		if !matches {
			return nil, kratoserrors.Conflict("SANDBOX_IDEMPOTENCY_CONFLICT", "sandbox create intent changed")
		}
		ackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := s.admission.AcknowledgeSandboxCreate(ackCtx, intent.TenantID, intent.SandboxID, intent.OperationID); err != nil {
			return nil, err
		}
		return &view.SandboxAcceptance{OperationId: intent.OperationID, ResourceId: intent.SandboxID, Accepted: true, Replayed: true}, nil
	}
	if replay {
		if prior, lookupErr := lookup(); lookupErr == nil {
			return prior, nil
		} else if status.Code(lookupErr) != codes.NotFound {
			return nil, lookupErr
		}
	}
	ack, err := s.client.AcceptSandboxCreate(ctx, intent.ResourceTenantID, intent.Actor, &req)
	if err != nil {
		if ctx.Err() == nil {
			prior, lookupErr := lookup()
			if lookupErr == nil {
				return prior, nil
			}
			if status.Code(lookupErr) != codes.NotFound {
				return nil, lookupErr
			}
		}
		return nil, err
	}
	if ack == nil || ack.Ack == nil || !ack.Ack.Accepted || ack.Ack.ResourceId != intent.SandboxID || ack.Ack.OperationId != intent.OperationID {
		if ctx.Err() == nil {
			prior, lookupErr := lookup()
			if lookupErr == nil {
				return prior, nil
			}
			if status.Code(lookupErr) != codes.NotFound {
				return nil, lookupErr
			}
		}
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_ACK", "invalid sandbox acceptance")
	}
	ackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err = s.admission.AcknowledgeSandboxCreate(ackCtx, intent.TenantID, intent.SandboxID, intent.OperationID); err != nil {
		return nil, err
	}
	return &view.SandboxAcceptance{OperationId: intent.OperationID, ResourceId: intent.SandboxID, Accepted: true, Replayed: replay || ack.Ack.Replayed}, nil
}

func sandboxCreateReplay(previous *res.GetSandboxResponse, instanceID, resourceID, operationID string, input *res.CreateSandboxData, intentDigest string) (*view.SandboxAcceptance, error) {
	if previous == nil || previous.Sandbox == nil || previous.Sandbox.Id != resourceID || previous.Sandbox.InstanceId != instanceID || previous.Sandbox.CreateOperationId != operationID {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_RESPONSE", "invalid sandbox response")
	}
	stored := previous.Sandbox
	matches := stored.CreateIntentDigest == intentDigest
	if stored.CreateIntentDigest == "" {
		matches = res.MatchLegacyCreateIntent(input, stored.Snapshot)
	}
	if !matches {
		return nil, kratoserrors.Conflict("SANDBOX_IDEMPOTENCY_CONFLICT", "sandbox create intent changed")
	}
	return &view.SandboxAcceptance{OperationId: operationID, ResourceId: resourceID, Accepted: true, Replayed: true}, nil
}

func wireSandbox[T proto.Message, U proto.Message](in T, out U) error {
	raw, err := protojson.Marshal(in)
	if err != nil {
		return err
	}
	return (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(raw, out)
}

func (s *SandboxService) ListSandboxes(ctx context.Context, r *view.ListSandboxesRequest) (*view.ListSandboxesResponse, error) {
	if r == nil || r.InstanceId == "" {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "instance_id is required")
	}
	page, size, err := resolveComputePage(r.Page, r.PageSize)
	if err != nil {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid page or pageSize")
	}
	tenant, actor, _, err := s.scope(ctx, data.SandboxListPath, "GET", r.InstanceId)
	if err != nil {
		return nil, err
	}
	out, err := s.client.ListSandboxes(ctx, tenant, actor, &res.ListSandboxesRequest{TenantId: tenant, InstanceId: r.InstanceId, Page: page, PageSize: size})
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_RESPONSE", "invalid sandbox list")
	}
	result := &view.ListSandboxesResponse{Items: []*view.Sandbox{}, Total: out.GetTotal()}
	for _, item := range out.GetItems() {
		v := new(view.Sandbox)
		if err = wireSandbox(item, v); err != nil {
			return nil, err
		}
		result.Items = append(result.Items, v)
	}
	return result, nil
}

func (s *SandboxService) GetSandbox(ctx context.Context, r *view.GetSandboxRequest) (*view.Sandbox, error) {
	if r == nil || r.InstanceId == "" || r.SandboxId == "" {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "sandbox identity is required")
	}
	tenant, actor, _, err := s.scope(ctx, data.SandboxGetPath, "GET", r.InstanceId)
	if err != nil {
		return nil, err
	}
	out, err := s.client.GetSandbox(ctx, tenant, actor, &res.GetSandboxRequest{TenantId: tenant, InstanceId: r.InstanceId, SandboxId: r.SandboxId})
	if err != nil {
		return nil, err
	}
	result := new(view.Sandbox)
	if out == nil || out.Sandbox == nil {
		return nil, kratoserrors.NotFound("SANDBOX_NOT_FOUND", "sandbox not found")
	}
	if err = wireSandbox(out.Sandbox, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *SandboxService) GetSandboxOperation(ctx context.Context, r *view.GetSandboxOperationRequest) (*view.SandboxOperation, error) {
	if r == nil || r.InstanceId == "" || r.OperationId == "" {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "operation identity is required")
	}
	tenant, actor, _, err := s.scope(ctx, data.SandboxOperationPath, "GET", r.InstanceId)
	if err != nil {
		return nil, err
	}
	out, err := s.client.GetSandboxOperation(ctx, tenant, actor, &res.GetOperationRequest{TenantId: tenant, InstanceId: r.InstanceId, OperationId: r.OperationId})
	if err != nil {
		return nil, err
	}
	result := new(view.SandboxOperation)
	if out == nil || out.Operation == nil {
		return nil, kratoserrors.NotFound("SANDBOX_OPERATION_NOT_FOUND", "operation not found")
	}
	if err = wireSandbox(out.Operation, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *SandboxService) action(ctx context.Context, instanceID, sandboxID, key string, version int64, path string, call func(string, string) (*res.OperationAck, error)) (*view.SandboxAcceptance, error) {
	if instanceID == "" || sandboxID == "" || version < 0 {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid sandbox action")
	}
	if err := sandboxKey(key); err != nil {
		return nil, err
	}
	method := "POST"
	if path == data.SandboxDeletePath {
		method = "DELETE"
	}
	tenant, actor, _, err := s.scope(ctx, path, method, instanceID)
	if err != nil {
		return nil, err
	}
	ack, err := call(tenant, actor)
	if err != nil {
		return nil, err
	}
	if ack == nil || !ack.Accepted || ack.ResourceId != sandboxID || ack.OperationId == "" {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_ACK", "invalid sandbox acceptance")
	}
	return &view.SandboxAcceptance{OperationId: ack.OperationId, ResourceId: ack.ResourceId, Accepted: true, Replayed: ack.Replayed}, nil
}

func (s *SandboxService) PauseSandbox(ctx context.Context, r *view.PauseSandboxRequest) (*view.SandboxAcceptance, error) {
	if r == nil || r.Data == nil {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "sandbox action is required")
	}
	d := r.Data
	return s.action(ctx, d.InstanceId, d.SandboxId, d.IdempotencyKey, d.ExpectedVersion, data.SandboxPausePath, func(t, a string) (*res.OperationAck, error) {
		out, err := s.client.PauseSandbox(ctx, t, a, &res.PauseRequest{TenantId: t, SandboxId: d.SandboxId, InstanceId: d.InstanceId, IdempotencyKey: d.IdempotencyKey, ExpectedVersion: d.ExpectedVersion})
		if err != nil || out == nil {
			return nil, err
		}
		return out.Ack, nil
	})
}
func (s *SandboxService) ResumeSandbox(ctx context.Context, r *view.ResumeSandboxRequest) (*view.SandboxAcceptance, error) {
	if r == nil || r.Data == nil {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "sandbox action is required")
	}
	d := r.Data
	return s.action(ctx, d.InstanceId, d.SandboxId, d.IdempotencyKey, d.ExpectedVersion, data.SandboxResumePath, func(t, a string) (*res.OperationAck, error) {
		out, err := s.client.ResumeSandbox(ctx, t, a, &res.ResumeRequest{TenantId: t, SandboxId: d.SandboxId, InstanceId: d.InstanceId, IdempotencyKey: d.IdempotencyKey, ExpectedVersion: d.ExpectedVersion})
		if err != nil || out == nil {
			return nil, err
		}
		return out.Ack, nil
	})
}
func (s *SandboxService) DeleteSandbox(ctx context.Context, r *view.DeleteSandboxRequest) (*view.SandboxAcceptance, error) {
	if r == nil || r.Data == nil {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "sandbox action is required")
	}
	d := r.Data
	return s.action(ctx, d.InstanceId, d.SandboxId, d.IdempotencyKey, d.ExpectedVersion, data.SandboxDeletePath, func(t, a string) (*res.OperationAck, error) {
		out, err := s.client.DeleteSandbox(ctx, t, a, &res.DeleteRequest{TenantId: t, SandboxId: d.SandboxId, InstanceId: d.InstanceId, IdempotencyKey: d.IdempotencyKey, ExpectedVersion: d.ExpectedVersion})
		if err != nil || out == nil {
			return nil, err
		}
		return out.Ack, nil
	})
}

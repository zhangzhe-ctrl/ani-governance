package service

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/google/uuid"
	modeldevcontractv1 "github.com/zhangzhe-ctrl/ani-modeldev-service/api/ani/modeldev/v1"
	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01"
	modeldevv1 "go-wind-admin/api/gen/go/modeldev/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/pkg/middleware/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ModelDevCreateInput preserves the parsed user intent, including the
// distinction between omitted general_parameters and an explicit empty array.
// Identity is obtained separately from the trusted request context.
type ModelDevCreateInput struct {
	Intent         cpup01.Intent
	IdempotencyKey string
}

// ModelDevService owns the current authorization, original-key lookup and
// durable Governance acceptance sequence. Resolving a candidate does not
// deliver an execution command or start computation.
type ModelDevService struct {
	authorization *data.ModelDevAuthorizationRepo
	tenants       ResourceTenantResolver
	acceptances   *data.ModelDevAcceptanceRepo
	bindings      *data.ModelDevReleaseBindingRepo
	resolver      *data.ModelDevClient
}

func NewModelDevService(
	authorization *data.ModelDevAuthorizationRepo,
	tenants ResourceTenantResolver,
	acceptances *data.ModelDevAcceptanceRepo,
	bindings *data.ModelDevReleaseBindingRepo,
	resolver *data.ModelDevClient,
) *ModelDevService {
	return &ModelDevService{
		authorization: authorization,
		tenants:       tenants,
		acceptances:   acceptances,
		bindings:      bindings,
		resolver:      resolver,
	}
}

// CreateExecution is called by the strict JSON HTTP adapter. It intentionally
// does not accept a protobuf request: repeated fields cannot retain whether an
// empty general_parameters array was present in the original user request.
func (s *ModelDevService) CreateExecution(ctx context.Context, in ModelDevCreateInput) (reply *modeldevv1.CreateExecutionResponse, result error) {
	defer func() {
		if err := ctx.Err(); err != nil {
			reply, result = nil, modelDevCreateFailure(err)
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, modelDevCreateFailure(err)
	}
	principal, err := auth.PrincipalFromContext(ctx)
	if err != nil || principal == nil || principal.ID == 0 {
		return nil, errors.Unauthorized("INVALID_LOGIN", "user login required")
	}
	if principal.Type != auth.SubjectUser || principal.TenantID == 0 {
		return nil, errors.Forbidden("FORBIDDEN", "tenant user required")
	}
	if s == nil || s.authorization == nil || s.tenants == nil || s.acceptances == nil {
		return nil, modelDevCreateUnavailable()
	}
	// A still-valid JWT or historical acceptance never substitutes for the
	// current database grant, including on original-key replay.
	if err := s.authorization.AuthorizeCreate(ctx, principal.TenantID, principal.ID); err != nil {
		return nil, modelDevCreateFailure(err)
	}
	tenant, err := s.tenants.ResourceTenantID(ctx, principal.TenantID)
	if err != nil {
		return nil, modelDevCreateFailure(err)
	}
	tenantID, err := uuid.Parse(tenant)
	if err != nil || tenantID == uuid.Nil || tenantID.String() != tenant {
		return nil, modelDevCreateUnavailable()
	}
	actor, err := principal.Actor()
	if err != nil {
		return nil, errors.Forbidden("FORBIDDEN", "tenant user required")
	}
	if in.IdempotencyKey == "" || !utf8.ValidString(in.IdempotencyKey) {
		return nil, modelDevCreateInvalid()
	}
	for _, character := range in.IdempotencyKey {
		if unicode.IsControl(character) {
			return nil, modelDevCreateInvalid()
		}
	}
	canonical, _, err := cpup01.CanonicalIntent(in.Intent)
	if err != nil {
		return nil, modelDevCreateInvalid()
	}
	// Use a fresh normalized value for both lookups and resolution. The shared
	// canonical contract preserves optional-field presence and normalizes values.
	var normalized struct {
		Schema string `json:"schema"`
		cpup01.Intent
	}
	if json.Unmarshal(canonical, &normalized) != nil {
		return nil, modelDevCreateUnavailable()
	}
	if normalized.SourceExecutionID != nil {
		return nil, errors.Forbidden("SOURCE_EXECUTION_UNAVAILABLE", "source execution is unavailable")
	}
	scope := data.ModelDevAdmissionScope{
		TenantID: principal.TenantID, ResourceTenantID: tenant, Actor: actor,
		Action: data.ModelDevCreateAction, IdempotencyKey: in.IdempotencyKey,
	}
	candidate := data.ModelDevFrozenCandidate{
		Intent: normalized.Intent, AcceptedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, modelDevCreateFailure(err)
		}
		accepted, err := s.acceptances.FindAccepted(ctx, scope, normalized.Intent)
		if err == nil {
			return modelDevCreateReply(accepted, true)
		}
		if !stderrors.Is(err, data.ErrModelDevAcceptanceNotFound) {
			return nil, modelDevCreateFailure(err)
		}
		// Only a new key needs the current binding and a configured resolver.
		if s.bindings == nil || s.resolver == nil {
			return nil, modelDevCreateUnavailable()
		}
		binding, err := s.bindings.Get(ctx, data.ModelDevReleaseBindingScope{
			TenantID: principal.TenantID, ResourceTenantID: tenant, PresetID: normalized.PresetID,
		})
		if err != nil {
			return nil, modelDevCreateFailure(err)
		}
		if binding == nil {
			return nil, modelDevCreateUnavailable()
		}
		if !binding.Target.NewSubmissionsEnabled {
			return nil, modelDevCreateFailure(data.ErrModelDevBindingDisabled)
		}
		resolved, err := s.resolver.Resolve(ctx, data.ModelDevResolveScope{ResourceTenantID: tenant, Actor: actor}, normalized.Intent,
			data.ModelDevReleaseSelection{ReleaseID: binding.Target.ReleaseID, ReleaseDigest: binding.Target.ReleaseDigest, BindingGeneration: binding.Generation}, candidate.AcceptedAt)
		if err != nil {
			return nil, modelDevCreateResolutionFailure(err)
		}
		if candidate.OperationID == "" {
			operationID, err := uuid.NewRandom()
			if err != nil {
				return nil, modelDevCreateUnavailable()
			}
			executionID, err := uuid.NewRandom()
			if err != nil {
				return nil, modelDevCreateUnavailable()
			}
			candidate.OperationID, candidate.ExecutionID = operationID.String(), executionID.String()
		}
		candidate.Snapshot = resolved.Snapshot
		// Network work is complete before this transaction. AcceptFrozen checks
		// the key and current generation again, then commits the queued command.
		accepted, replayed, err := s.acceptances.AcceptFrozen(ctx, scope, candidate)
		if stderrors.Is(err, data.ErrModelDevBindingGenerationConflict) && attempt == 0 {
			// One fresh binding observation is allowed, starting with the original
			// key again. IDs and accepted_at remain fixed across this retry.
			continue
		}
		if err != nil {
			return nil, modelDevCreateFailure(err)
		}
		return modelDevCreateReply(accepted, replayed)
	}
	return nil, modelDevCreateFailure(data.ErrModelDevBindingGenerationConflict)
}

func modelDevCreateReply(accepted *data.ModelDevAcceptance, replayed bool) (*modeldevv1.CreateExecutionResponse, error) {
	if accepted == nil {
		return nil, modelDevCreateUnavailable()
	}
	reply := &modeldevv1.CreateExecutionResponse{
		OperationId: accepted.OperationID, ExecutionId: accepted.ExecutionID,
		ResolvedReleaseId: accepted.Snapshot.Release.ReleaseID, Replayed: replayed,
	}
	switch accepted.DispatchState {
	case "QUEUED", "DISPATCHING", "UNKNOWN":
		if accepted.OwnerReceipt != nil {
			return nil, modelDevCreateUnavailable()
		}
		// Until a durable owner ACK exists, Governance knows only that it
		// accepted the original. Transport progress is not execution progress.
		reply.ComputeState, reply.DeliveryState = "ACCEPTED", "PENDING"
		reply.CloseState, reply.ResourceState = "OPEN", "NOT_APPLICABLE"
	case "ACKED":
		if accepted.OwnerReceipt == nil || accepted.OwnerReceipt.Validate(accepted.Envelope()) != nil {
			return nil, modelDevCreateUnavailable()
		}
		// These four states and their revision came from the same persisted
		// receipt as this admission. No separate owner query is composed here.
		reply.ComputeState = accepted.OwnerReceipt.ComputeState
		reply.DeliveryState = accepted.OwnerReceipt.DeliveryState
		reply.CloseState = accepted.OwnerReceipt.CloseState
		reply.ResourceState = accepted.OwnerReceipt.ResourceState
	default:
		return nil, modelDevCreateUnavailable()
	}
	return reply, nil
}

func modelDevCreateInvalid() error {
	return errors.BadRequest("INVALID_MODELDEV_CREATE", "invalid modeldev create request")
}

func modelDevCreateUnavailable() error {
	return errors.ServiceUnavailable("MODELDEV_CREATE_UNAVAILABLE", "modeldev create unavailable")
}

func modelDevCreateFailure(err error) error {
	switch {
	case stderrors.Is(err, context.Canceled):
		return errors.New(499, "MODELDEV_CREATE_CANCELED", "modeldev create canceled").WithCause(context.Canceled)
	case stderrors.Is(err, context.DeadlineExceeded):
		return errors.New(504, "MODELDEV_CREATE_TIMEOUT", "modeldev create timed out").WithCause(context.DeadlineExceeded)
	case stderrors.Is(err, data.ErrModelDevAuthorizationDenied):
		return errors.Forbidden("FORBIDDEN", "modeldev create forbidden")
	case stderrors.Is(err, data.ErrModelDevIdempotencyConflict):
		return errors.Conflict("IDEMPOTENCY_CONFLICT", "idempotency key has a different intent")
	case stderrors.Is(err, data.ErrModelDevBindingNotFound):
		return errors.New(412, "NO_COMPATIBLE_RELEASE", "no compatible release is enabled")
	case stderrors.Is(err, data.ErrModelDevBindingDisabled):
		return errors.New(412, "SUBMISSIONS_PAUSED", "new submissions are paused")
	case stderrors.Is(err, data.ErrModelDevBindingGenerationConflict):
		return errors.Conflict("MODELDEV_BINDING_CHANGED", "release binding changed during acceptance")
	default:
		return modelDevCreateUnavailable()
	}
}

func modelDevCreateResolutionFailure(err error) error {
	switch status.Code(err) {
	case codes.Canceled:
		return modelDevCreateFailure(context.Canceled)
	case codes.DeadlineExceeded:
		return modelDevCreateFailure(context.DeadlineExceeded)
	}
	// The typed client has already validated and sanitized provider details.
	// Only fixed code/reason pairs cross the public HTTP boundary; neither its
	// message nor metadata, correlation ID or other details are copied.
	failure, ok := status.FromError(err)
	if !ok || len(failure.Details()) != 1 {
		return modelDevCreateUnavailable()
	}
	detail, ok := failure.Details()[0].(*modeldevcontractv1.ErrorDetail)
	if !ok || detail == nil {
		return modelDevCreateUnavailable()
	}
	switch {
	case failure.Code() == codes.InvalidArgument && detail.Reason == modeldevcontractv1.ErrorReason_ERROR_REASON_INVALID_ARGUMENT:
		return modelDevCreateInvalid()
	case failure.Code() == codes.FailedPrecondition && detail.Reason == modeldevcontractv1.ErrorReason_ERROR_REASON_NO_COMPATIBLE_RELEASE:
		return errors.New(412, "NO_COMPATIBLE_RELEASE", "selected release is unavailable or incompatible")
	case failure.Code() == codes.NotFound && detail.Reason == modeldevcontractv1.ErrorReason_ERROR_REASON_RESOURCE_NOT_FOUND:
		return errors.NotFound("RESOURCE_NOT_FOUND", "input version unavailable")
	case failure.Code() == codes.FailedPrecondition && detail.Reason == modeldevcontractv1.ErrorReason_ERROR_REASON_INPUT_NOT_READY:
		return errors.New(412, "INPUT_NOT_READY", "input version is not ready")
	case failure.Code() == codes.Unavailable && detail.Reason == modeldevcontractv1.ErrorReason_ERROR_REASON_ENVIRONMENT_NOT_READY:
		return errors.ServiceUnavailable("ENVIRONMENT_NOT_READY", "managed admission environment is not ready")
	case failure.Code() == codes.Unavailable && detail.Reason == modeldevcontractv1.ErrorReason_ERROR_REASON_UPSTREAM_UNAVAILABLE:
		return errors.ServiceUnavailable("UPSTREAM_UNAVAILABLE", "admission resolution unavailable")
	default:
		return modelDevCreateUnavailable()
	}
}

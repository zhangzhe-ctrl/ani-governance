package service

import (
	"context"
	stderrors "errors"
	"math"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/pkg/middleware/auth"
)

// ModelDevPauseInput fixes the existing release to pause. Identity comes from
// the verified Principal, and this request cannot enable or replace a release.
type ModelDevPauseInput struct {
	PresetID           string
	ReleaseID          string
	ReleaseDigest      string
	ExpectedGeneration uint64
	Reason             string
	EvidenceReference  string
}

type ModelDevBindingService struct {
	authorization *data.ModelDevAuthorizationRepo
	tenants       ResourceTenantResolver
	bindings      *data.ModelDevReleaseBindingRepo
	validator     ModelDevReleaseValidator
}

// Validation reads the immutable ModelDev catalogue and its current tenant
// environment. Client-supplied Release bytes are never a verification receipt.
type ModelDevReleaseValidator interface {
	ValidateRelease(context.Context, data.ModelDevResolveScope, string, string, string) error
}

func NewModelDevBindingService(authorization *data.ModelDevAuthorizationRepo, tenants ResourceTenantResolver, bindings *data.ModelDevReleaseBindingRepo, validator ...ModelDevReleaseValidator) *ModelDevBindingService {
	s := &ModelDevBindingService{authorization: authorization, tenants: tenants, bindings: bindings}
	if len(validator) == 1 {
		s.validator = validator[0]
	}
	return s
}

// Enable creates the sole binding or changes it with CAS. Rollback names a
// previously imported immutable Release and follows exactly this same path.
func (s *ModelDevBindingService) Enable(ctx context.Context, in ModelDevPauseInput) (*data.ModelDevReleaseBindingChange, error) {
	principal, err := auth.PrincipalFromContext(ctx)
	if err != nil || principal == nil || principal.ID == 0 {
		return nil, errors.Unauthorized("INVALID_LOGIN", "user login required")
	}
	if principal.Type != auth.SubjectUser || principal.TenantID == 0 {
		return nil, errors.Forbidden("FORBIDDEN", "tenant user required")
	}
	if s == nil || s.authorization == nil || s.tenants == nil || s.bindings == nil || s.validator == nil {
		return nil, modelDevPauseUnavailable()
	}
	if err := s.authorization.AuthorizeManageReleaseBinding(ctx, principal.TenantID, principal.ID); err != nil {
		return nil, modelDevPauseFailure(err)
	}
	tenant, err := s.tenants.ResourceTenantID(ctx, principal.TenantID)
	if err != nil || !modelDevQueryUUID(tenant) {
		return nil, modelDevPauseFailure(err)
	}
	actor, err := principal.Actor()
	if err != nil {
		return nil, errors.Forbidden("FORBIDDEN", "tenant user required")
	}
	if !modelDevQueryUUID(in.PresetID) || !modelDevQueryUUID(in.ReleaseID) || len(in.ReleaseDigest) != 64 || strings.Trim(in.ReleaseDigest, "0123456789abcdef") != "" || in.ExpectedGeneration > math.MaxInt64 {
		return nil, modelDevPauseFailure(cpup01.ErrInvalidArgument)
	}
	scope := data.ModelDevReleaseBindingScope{TenantID: principal.TenantID, ResourceTenantID: tenant, PresetID: in.PresetID}
	current, err := s.bindings.Get(ctx, scope)
	if err != nil && !stderrors.Is(err, data.ErrModelDevBindingNotFound) {
		return nil, modelDevPauseFailure(err)
	}
	if (current == nil && in.ExpectedGeneration != 0) || (current != nil && in.ExpectedGeneration > current.Generation) {
		return nil, modelDevPauseFailure(data.ErrModelDevBindingGenerationConflict)
	}
	if err := s.validator.ValidateRelease(ctx, data.ModelDevResolveScope{ResourceTenantID: tenant, Actor: actor}, in.PresetID, in.ReleaseID, in.ReleaseDigest); err != nil {
		return nil, modelDevQueryFailure(err)
	}
	// The remote check happens outside the transaction. Recheck the current
	// grant before mutating, then let the existing binding CAS arbitrate races.
	if err := s.authorization.AuthorizeManageReleaseBinding(ctx, principal.TenantID, principal.ID); err != nil {
		return nil, modelDevPauseFailure(err)
	}
	change, err := s.bindings.CompareAndSwap(ctx, scope, data.ModelDevReleaseBindingUpdate{ExpectedGeneration: in.ExpectedGeneration, Target: data.ModelDevReleaseBindingTarget{ReleaseID: in.ReleaseID, ReleaseDigest: in.ReleaseDigest, NewSubmissionsEnabled: true}, Actor: actor, RequestedAt: time.Now().UTC().Truncate(time.Microsecond), Reason: in.Reason, EvidenceReference: in.EvidenceReference})
	if err != nil {
		return nil, modelDevPauseFailure(err)
	}
	return change, nil
}

// Pause changes only the gate of the requested current target. A replay means
// the same target is already paused; it does not recover a historical command.
// No ModelDev connection is required to stop new Governance admissions.
func (s *ModelDevBindingService) Pause(ctx context.Context, in ModelDevPauseInput) (change *data.ModelDevReleaseBindingChange, result error) {
	defer func() {
		if err := ctx.Err(); err != nil {
			change, result = nil, modelDevPauseFailure(err)
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, modelDevPauseFailure(err)
	}
	principal, err := auth.PrincipalFromContext(ctx)
	if err != nil || principal == nil || principal.ID == 0 {
		return nil, errors.Unauthorized("INVALID_LOGIN", "user login required")
	}
	if principal.Type != auth.SubjectUser || principal.TenantID == 0 {
		return nil, errors.Forbidden("FORBIDDEN", "tenant user required")
	}
	if s == nil || s.authorization == nil || s.tenants == nil || s.bindings == nil {
		return nil, modelDevPauseUnavailable()
	}
	// The current maintenance grant is required even when CAS will return an
	// unchanged target. JWT roles and a former successful pause do not grant it.
	if err := s.authorization.AuthorizeManageReleaseBinding(ctx, principal.TenantID, principal.ID); err != nil {
		return nil, modelDevPauseFailure(err)
	}
	resourceTenantID, err := s.tenants.ResourceTenantID(ctx, principal.TenantID)
	if err != nil {
		return nil, modelDevPauseFailure(err)
	}
	tenantID, err := uuid.Parse(resourceTenantID)
	if err != nil || tenantID == uuid.Nil || tenantID.String() != resourceTenantID {
		return nil, modelDevPauseUnavailable()
	}
	actor, err := principal.Actor()
	if err != nil {
		return nil, errors.Forbidden("FORBIDDEN", "tenant user required")
	}
	releaseID, err := uuid.Parse(in.ReleaseID)
	if err != nil || releaseID == uuid.Nil || len(in.ReleaseID) != 36 || in.ExpectedGeneration == 0 || in.ExpectedGeneration > math.MaxInt64 {
		return nil, modelDevPauseFailure(cpup01.ErrInvalidArgument)
	}
	scope := data.ModelDevReleaseBindingScope{
		TenantID: principal.TenantID, ResourceTenantID: resourceTenantID, PresetID: in.PresetID,
	}
	current, err := s.bindings.Get(ctx, scope)
	if err != nil {
		return nil, modelDevPauseFailure(err)
	}
	if current == nil {
		return nil, modelDevPauseUnavailable()
	}
	// Do not reinterpret a stale request as a pause of a newly read release.
	// Reject future generations before CAS so a concurrent target change cannot
	// make this request's generation valid for switching back to an old target.
	if current.Target.ReleaseID != releaseID.String() || current.Target.ReleaseDigest != in.ReleaseDigest || in.ExpectedGeneration > current.Generation {
		return nil, modelDevPauseFailure(data.ErrModelDevBindingGenerationConflict)
	}
	change, err = s.bindings.CompareAndSwap(ctx, scope, data.ModelDevReleaseBindingUpdate{
		ExpectedGeneration: in.ExpectedGeneration,
		Target: data.ModelDevReleaseBindingTarget{
			ReleaseID: releaseID.String(), ReleaseDigest: in.ReleaseDigest, NewSubmissionsEnabled: false,
		},
		Actor: actor, RequestedAt: time.Now().UTC().Truncate(time.Microsecond),
		Reason: in.Reason, EvidenceReference: in.EvidenceReference,
	})
	if err != nil {
		return nil, modelDevPauseFailure(err)
	}
	return change, nil
}

func modelDevPauseUnavailable() error {
	return errors.ServiceUnavailable("MODELDEV_PAUSE_UNAVAILABLE", "modeldev pause unavailable")
}

func modelDevPauseFailure(err error) error {
	switch {
	case stderrors.Is(err, context.Canceled):
		return errors.New(499, "MODELDEV_PAUSE_CANCELED", "modeldev pause canceled").WithCause(context.Canceled)
	case stderrors.Is(err, context.DeadlineExceeded):
		return errors.New(504, "MODELDEV_PAUSE_TIMEOUT", "modeldev pause timed out").WithCause(context.DeadlineExceeded)
	case stderrors.Is(err, data.ErrModelDevAuthorizationDenied):
		return errors.Forbidden("FORBIDDEN", "modeldev binding management forbidden")
	case stderrors.Is(err, cpup01.ErrInvalidArgument):
		return errors.BadRequest("INVALID_MODELDEV_PAUSE", "invalid modeldev pause request")
	case stderrors.Is(err, data.ErrModelDevBindingNotFound):
		return errors.NotFound("MODELDEV_BINDING_NOT_FOUND", "release binding not found")
	case stderrors.Is(err, data.ErrModelDevBindingGenerationConflict):
		return errors.Conflict("MODELDEV_BINDING_CHANGED", "release binding changed")
	case stderrors.Is(err, data.ErrModelDevBindingGenerationExhausted):
		return errors.Conflict("MODELDEV_BINDING_GENERATION_EXHAUSTED", "release binding generation exhausted")
	default:
		return modelDevPauseUnavailable()
	}
}

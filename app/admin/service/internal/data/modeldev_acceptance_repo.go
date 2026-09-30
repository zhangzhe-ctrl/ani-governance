package data

import (
	"context"
	"errors"
	"time"

	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01"
	"go-wind-admin/app/admin/service/internal/data/ent"
	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"
)

const ModelDevCreateAction = "modeldev.execution.create"

var ErrModelDevIdempotencyConflict = errors.New("modeldev idempotency conflict")

// ModelDevAdmissionScope is derived by Governance from the current verified
// Principal and its persisted resource tenant mapping, never from public JSON.
type ModelDevAdmissionScope struct {
	TenantID uint32
	ResourceTenantID string
	Actor string
	Action string
	IdempotencyKey string
}

// ModelDevFrozenCandidate is resolved before entering the database transaction.
// Current binding generation validation belongs to the admission coordinator;
// replay must return the first accepted value rather than use a new candidate.
type ModelDevFrozenCandidate struct {
	OperationID string
	ExecutionID string
	Intent cpup01.Intent
	Snapshot cpup01.Snapshot
	AcceptedAt time.Time
}

type ModelDevAcceptance struct {
	Scope ModelDevAdmissionScope
	OperationID string
	ExecutionID string
	Intent cpup01.Intent
	Snapshot cpup01.Snapshot
	IntentHash string
	ExecutionSpecHash string
	AcceptedAt time.Time
	DispatchState string
}

// ModelDevAcceptanceRepo persists CPU admission and its queued delivery intent.
// It has no dependency on the GPU quota ledger and does not start computation.
type ModelDevAcceptanceRepo struct {
	entClient *entCrud.EntClient[*ent.Client]
}

func NewModelDevAcceptanceRepo(client *entCrud.EntClient[*ent.Client]) *ModelDevAcceptanceRepo {
	return &ModelDevAcceptanceRepo{entClient: client}
}

// AcceptFrozen returns only after the acceptance transaction commits. The bool
// distinguishes a same-intent replay from this call's newly persisted record.
func (r *ModelDevAcceptanceRepo) AcceptFrozen(ctx context.Context, scope ModelDevAdmissionScope, candidate ModelDevFrozenCandidate) (*ModelDevAcceptance, bool, error) {
	return nil, false, errors.New("modeldev acceptance persistence not implemented")
}

package data

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/modeldevacceptance"
	"go-wind-admin/app/admin/service/internal/data/ent/tenant"
	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"
)

const ModelDevCreateAction = "modeldev.execution.create"

var ErrModelDevIdempotencyConflict = errors.New("modeldev idempotency conflict")

// ModelDevAdmissionScope is derived by Governance from the current verified
// Principal and its persisted resource tenant mapping, never from public JSON.
type ModelDevAdmissionScope struct {
	TenantID         uint32
	ResourceTenantID string
	Actor            string
	Action           string
	IdempotencyKey   string
}

// ModelDevFrozenCandidate is resolved before entering the database transaction.
// Current binding generation validation belongs to the admission coordinator;
// replay must return the first accepted value rather than use a new candidate.
type ModelDevFrozenCandidate struct {
	OperationID string
	ExecutionID string
	Intent      cpup01.Intent
	Snapshot    cpup01.Snapshot
	AcceptedAt  time.Time
}

type ModelDevAcceptance struct {
	Scope             ModelDevAdmissionScope
	OperationID       string
	ExecutionID       string
	Intent            cpup01.Intent
	Snapshot          cpup01.Snapshot
	IntentHash        string
	ExecutionSpecHash string
	AcceptedAt        time.Time
	DispatchState     string
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
	if scope.TenantID == 0 || scope.ResourceTenantID == "" || scope.Actor == "" || scope.Action != ModelDevCreateAction || scope.IdempotencyKey == "" {
		return nil, false, fmt.Errorf("%w: modeldev admission scope", cpup01.ErrInvalidArgument)
	}
	intentCanonical, intentHash, err := cpup01.CanonicalIntent(candidate.Intent)
	if err != nil {
		return nil, false, err
	}
	var accepted *ModelDevAcceptance
	var replayed bool
	err = r.acceptanceTransaction(ctx, func(tx *ent.Tx) error {
		// Serialize acceptance within this tenant before looking up the key. The
		// resource UUID must be the current persisted Governance tenant mapping.
		if _, err := tx.Tenant.Query().Where(tenant.IDEQ(scope.TenantID), tenant.ResourceTenantIDEQ(scope.ResourceTenantID)).ForUpdate().Only(ctx); err != nil {
			return err
		}
		row, err := tx.ModelDevAcceptance.Query().Where(
			modeldevacceptance.TenantIDEQ(scope.TenantID),
			modeldevacceptance.ResourceTenantIDEQ(scope.ResourceTenantID),
			modeldevacceptance.ActorEQ(scope.Actor),
			modeldevacceptance.ActionEQ(scope.Action),
			modeldevacceptance.IdempotencyKeyEQ(scope.IdempotencyKey),
		).Only(ctx)
		if err == nil {
			if row.IntentHash != intentHash || !bytes.Equal(row.IntentCanonical, intentCanonical) {
				return ErrModelDevIdempotencyConflict
			}
			accepted, err = modelDevAcceptanceFromRow(row)
			replayed = true
			return err
		}
		if !ent.IsNotFound(err) {
			return err
		}
		// Only a new key consumes the resolved candidate. Historical replay is
		// independent of new defaults and does not revalidate a new snapshot.
		if candidate.OperationID == "" || candidate.ExecutionID == "" || candidate.AcceptedAt.IsZero() || !candidate.Snapshot.DeadlineAt.After(candidate.AcceptedAt) {
			return fmt.Errorf("%w: modeldev frozen admission", cpup01.ErrInvalidArgument)
		}
		snapshotCanonical, err := candidate.Snapshot.Canonical()
		if err != nil {
			return err
		}
		specHash, err := candidate.Snapshot.Digest()
		if err != nil {
			return err
		}
		// PostgreSQL timestamps retain microseconds. Persist and return the same
		// accepted_at so a later reader cannot observe a different audit value.
		acceptedAt := candidate.AcceptedAt.UTC().Truncate(time.Microsecond)
		row, err = tx.ModelDevAcceptance.Create().
			SetTenantID(scope.TenantID).
			SetResourceTenantID(scope.ResourceTenantID).
			SetActor(scope.Actor).
			SetAction(scope.Action).
			SetIdempotencyKey(scope.IdempotencyKey).
			SetOperationID(candidate.OperationID).
			SetExecutionID(candidate.ExecutionID).
			SetIntentHash(intentHash).
			SetIntentCanonical(intentCanonical).
			SetExecutionSpecHash(specHash).
			SetSnapshotCanonical(snapshotCanonical).
			SetAcceptedAt(acceptedAt).
			SetDispatchState(modeldevacceptance.DispatchStateQUEUED).
			Save(ctx)
		if err != nil {
			return err
		}
		accepted, err = modelDevAcceptanceFromRow(row)
		return err
	})
	if err != nil {
		return nil, false, err
	}
	return accepted, replayed, nil
}

func (r *ModelDevAcceptanceRepo) acceptanceTransaction(ctx context.Context, work func(*ent.Tx) error) (err error) {
	tx, err := r.entClient.Client().Tx(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
				err = errors.Join(err, fmt.Errorf("rollback modeldev acceptance: %w", rollbackErr))
			}
		}
	}()
	if err = work(tx); err != nil {
		return err
	}
	err = tx.Commit()
	committed = err == nil
	return err
}

func modelDevAcceptanceFromRow(row *ent.ModelDevAcceptance) (*ModelDevAcceptance, error) {
	invalid := errors.New("invalid persisted modeldev acceptance")
	if row.TenantID == nil || *row.TenantID == 0 {
		return nil, invalid
	}
	var intentEnvelope struct {
		Schema string `json:"schema"`
		cpup01.Intent
	}
	var snapshot cpup01.Snapshot
	if json.Unmarshal(row.IntentCanonical, &intentEnvelope) != nil || json.Unmarshal(row.SnapshotCanonical, &snapshot) != nil {
		return nil, invalid
	}
	intentCanonical, intentHash, err := cpup01.CanonicalIntent(intentEnvelope.Intent)
	if err != nil || intentHash != row.IntentHash || !bytes.Equal(intentCanonical, row.IntentCanonical) {
		return nil, invalid
	}
	snapshotCanonical, err := snapshot.Canonical()
	if err != nil || !bytes.Equal(snapshotCanonical, row.SnapshotCanonical) {
		return nil, invalid
	}
	specHash, err := snapshot.Digest()
	if err != nil || specHash != row.ExecutionSpecHash {
		return nil, invalid
	}
	return &ModelDevAcceptance{
		Scope:       ModelDevAdmissionScope{TenantID: *row.TenantID, ResourceTenantID: row.ResourceTenantID, Actor: row.Actor, Action: row.Action, IdempotencyKey: row.IdempotencyKey},
		OperationID: row.OperationID, ExecutionID: row.ExecutionID,
		Intent: intentEnvelope.Intent, Snapshot: snapshot,
		IntentHash: row.IntentHash, ExecutionSpecHash: row.ExecutionSpecHash,
		AcceptedAt: row.AcceptedAt, DispatchState: string(row.DispatchState),
	}, nil
}

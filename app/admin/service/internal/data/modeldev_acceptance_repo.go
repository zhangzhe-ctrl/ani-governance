package data

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/modeldevacceptance"
	"go-wind-admin/app/admin/service/internal/data/ent/modeldevreleasebinding"
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
// New keys recheck the current binding within the acceptance transaction;
// replay returns the first accepted value rather than use a new candidate.
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
	if scope.TenantID == 0 || scope.ResourceTenantID == "" || !cpup01.ValidAuditActor(scope.Actor) || scope.Action != ModelDevCreateAction || scope.IdempotencyKey == "" {
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
			if ent.IsNotFound(err) {
				return fmt.Errorf("%w: modeldev tenant mapping", cpup01.ErrInvalidArgument)
			}
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
		specHash, err := candidate.Snapshot.Digest()
		if err != nil {
			return err
		}
		envelope := cpup01.AdmissionEnvelope{
			TenantID: scope.ResourceTenantID, Actor: scope.Actor,
			OperationID: candidate.OperationID, ExecutionID: candidate.ExecutionID,
			Intent: candidate.Intent, IntentHash: intentHash,
			Snapshot: candidate.Snapshot, SpecHash: specHash, AcceptedAt: candidate.AcceptedAt,
		}
		_, snapshotCanonical, err := envelope.CanonicalPayloads()
		if err != nil {
			return err
		}
		// Catalogue resolution happened outside this transaction. Serialize
		// against pointer CAS using the same tenant -> binding lock order;
		// only new keys consume the current target and admission gate.
		binding, err := tx.ModelDevReleaseBinding.Query().Where(
			modeldevreleasebinding.TenantIDEQ(scope.TenantID),
			modeldevreleasebinding.ResourceTenantIDEQ(scope.ResourceTenantID),
			modeldevreleasebinding.PresetIDEQ(strings.ToLower(candidate.Snapshot.Release.PresetID)),
		).ForUpdate().Only(ctx)
		if ent.IsNotFound(err) {
			return ErrModelDevBindingNotFound
		}
		if err != nil {
			return err
		}
		if !binding.NewSubmissionsEnabled {
			return ErrModelDevBindingDisabled
		}
		if binding.Generation != candidate.Snapshot.Release.AcceptedBindingGeneration ||
			binding.ReleaseID != strings.ToLower(candidate.Snapshot.Release.ReleaseID) ||
			binding.ReleaseDigest != candidate.Snapshot.Release.ReleaseDigest {
			return ErrModelDevBindingGenerationConflict
		}
		// The shared envelope rejects sub-microsecond timestamps instead of
		// silently changing the immutable accepted_at persisted by PostgreSQL.
		acceptedAt := candidate.AcceptedAt.UTC()
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
	envelope := cpup01.AdmissionEnvelope{
		TenantID: row.ResourceTenantID, Actor: row.Actor,
		OperationID: row.OperationID, ExecutionID: row.ExecutionID,
		Intent: intentEnvelope.Intent, IntentHash: row.IntentHash,
		Snapshot: snapshot, SpecHash: row.ExecutionSpecHash, AcceptedAt: row.AcceptedAt,
	}
	intentCanonical, snapshotCanonical, err := envelope.CanonicalPayloads()
	if err != nil || !bytes.Equal(intentCanonical, row.IntentCanonical) || !bytes.Equal(snapshotCanonical, row.SnapshotCanonical) {
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

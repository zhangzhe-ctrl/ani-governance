package data

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/modeldevreleasebinding"
	"go-wind-admin/app/admin/service/internal/data/ent/tenant"
	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"
)

var (
	ErrModelDevBindingGenerationConflict  = errors.New("modeldev release binding generation conflict")
	ErrModelDevBindingGenerationExhausted = errors.New("modeldev release binding generation exhausted")
	ErrModelDevBindingNotFound            = errors.New("modeldev release binding not found")
	ErrModelDevBindingDisabled            = errors.New("modeldev release binding disabled")
)

// PostgreSQL bigint bounds Governance's current-pointer generation. Shared
// snapshot and wire uint64 fields keep their separate contract.
const maxModelDevBindingGeneration uint64 = 1<<63 - 1

// ModelDevReleaseBindingScope identifies one tenant's managed preset binding.
// It is derived from trusted context and the persisted tenant mapping, not a
// user's choice of actor, namespace, backend endpoint, or global setting.
type ModelDevReleaseBindingScope struct {
	TenantID         uint32
	ResourceTenantID string
	PresetID         string
}

// ModelDevReleaseBindingTarget is an already resolved immutable catalogue
// reference. This repository does not establish that a release is VERIFIED;
// the managed use case must check catalogue evidence and current authorization.
type ModelDevReleaseBindingTarget struct {
	ReleaseID             string
	ReleaseDigest         string
	NewSubmissionsEnabled bool
}

type ModelDevReleaseBinding struct {
	Scope             ModelDevReleaseBindingScope
	Target            ModelDevReleaseBindingTarget
	Generation        uint64
	UpdatedBy         string
	UpdatedAt         time.Time
	Reason            string
	EvidenceReference string
}

type ModelDevReleaseBindingUpdate struct {
	ExpectedGeneration uint64
	Target             ModelDevReleaseBindingTarget
	Actor              string
	RequestedAt        time.Time
	Reason             string
	EvidenceReference  string
}

type ModelDevReleaseBindingChange struct {
	Before   *ModelDevReleaseBinding
	After    *ModelDevReleaseBinding
	Replayed bool
}

// ModelDevReleaseBindingRepo owns only Governance's current pointer and gate.
// Immutable release content stays in ModelDev. Network resolution is never
// performed inside a binding or acceptance transaction.
type ModelDevReleaseBindingRepo struct {
	entClient *entCrud.EntClient[*ent.Client]
}

func NewModelDevReleaseBindingRepo(client *entCrud.EntClient[*ent.Client]) *ModelDevReleaseBindingRepo {
	return &ModelDevReleaseBindingRepo{entClient: client}
}

func (r *ModelDevReleaseBindingRepo) Get(ctx context.Context, scope ModelDevReleaseBindingScope) (*ModelDevReleaseBinding, error) {
	scope, err := canonicalModelDevBindingScope(scope)
	if err != nil {
		return nil, err
	}
	row, err := r.entClient.Client().ModelDevReleaseBinding.Query().Where(
		modeldevreleasebinding.TenantIDEQ(scope.TenantID),
		modeldevreleasebinding.ResourceTenantIDEQ(scope.ResourceTenantID),
		modeldevreleasebinding.PresetIDEQ(scope.PresetID),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrModelDevBindingNotFound
	}
	if err != nil {
		return nil, err
	}
	return modelDevReleaseBindingFromRow(row), nil
}

// CompareAndSwap creates generation 1 only from expected generation 0. A new
// target requires the observed generation; the same target replays the stored
// generation and audit values. Replays do not replace current authorization.
func (r *ModelDevReleaseBindingRepo) CompareAndSwap(ctx context.Context, scope ModelDevReleaseBindingScope, update ModelDevReleaseBindingUpdate) (*ModelDevReleaseBindingChange, error) {
	if update.ExpectedGeneration > maxModelDevBindingGeneration {
		return nil, fmt.Errorf("%w: modeldev binding expected generation", cpup01.ErrInvalidArgument)
	}
	scope, err := canonicalModelDevBindingScope(scope)
	if err != nil {
		return nil, err
	}
	update, err = canonicalModelDevBindingUpdate(update)
	if err != nil {
		return nil, err
	}
	var change *ModelDevReleaseBindingChange
	err = r.bindingTransaction(ctx, func(tx *ent.Tx) error {
		// The tenant row also serializes first creation, when no binding row
		// exists yet. Acceptance uses this same tenant lock ordering.
		if _, err := tx.Tenant.Query().Where(tenant.IDEQ(scope.TenantID), tenant.ResourceTenantIDEQ(scope.ResourceTenantID)).ForUpdate().Only(ctx); err != nil {
			if ent.IsNotFound(err) {
				return fmt.Errorf("%w: modeldev tenant mapping", cpup01.ErrInvalidArgument)
			}
			return err
		}
		row, err := tx.ModelDevReleaseBinding.Query().Where(
			modeldevreleasebinding.TenantIDEQ(scope.TenantID),
			modeldevreleasebinding.ResourceTenantIDEQ(scope.ResourceTenantID),
			modeldevreleasebinding.PresetIDEQ(scope.PresetID),
		).ForUpdate().Only(ctx)
		if ent.IsNotFound(err) {
			if update.ExpectedGeneration != 0 {
				return ErrModelDevBindingGenerationConflict
			}
			row, err = tx.ModelDevReleaseBinding.Create().
				SetTenantID(scope.TenantID).
				SetResourceTenantID(scope.ResourceTenantID).
				SetPresetID(scope.PresetID).
				SetReleaseID(update.Target.ReleaseID).
				SetReleaseDigest(update.Target.ReleaseDigest).
				SetGeneration(1).
				SetNewSubmissionsEnabled(update.Target.NewSubmissionsEnabled).
				SetUpdatedBy(update.Actor).
				SetUpdatedAt(update.RequestedAt.UTC()).
				SetReason(update.Reason).
				SetEvidenceReference(update.EvidenceReference).
				Save(ctx)
			if err != nil {
				return err
			}
			change = &ModelDevReleaseBindingChange{After: modelDevReleaseBindingFromRow(row)}
			return nil
		}
		if err != nil {
			return err
		}
		before := modelDevReleaseBindingFromRow(row)
		if before.Target == update.Target {
			change = &ModelDevReleaseBindingChange{Before: before, After: before, Replayed: true}
			return nil
		}
		if row.Generation != update.ExpectedGeneration {
			return ErrModelDevBindingGenerationConflict
		}
		if row.Generation >= maxModelDevBindingGeneration {
			return ErrModelDevBindingGenerationExhausted
		}
		row, err = tx.ModelDevReleaseBinding.UpdateOneID(row.ID).Where(
			modeldevreleasebinding.TenantIDEQ(scope.TenantID),
			modeldevreleasebinding.ResourceTenantIDEQ(scope.ResourceTenantID),
			modeldevreleasebinding.PresetIDEQ(scope.PresetID),
			modeldevreleasebinding.GenerationEQ(update.ExpectedGeneration),
		).
			SetReleaseID(update.Target.ReleaseID).
			SetReleaseDigest(update.Target.ReleaseDigest).
			SetGeneration(row.Generation + 1).
			SetNewSubmissionsEnabled(update.Target.NewSubmissionsEnabled).
			SetUpdatedBy(update.Actor).
			SetUpdatedAt(update.RequestedAt.UTC()).
			SetReason(update.Reason).
			SetEvidenceReference(update.EvidenceReference).
			Save(ctx)
		if ent.IsNotFound(err) {
			return ErrModelDevBindingGenerationConflict
		}
		if err != nil {
			return err
		}
		change = &ModelDevReleaseBindingChange{Before: before, After: modelDevReleaseBindingFromRow(row)}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return change, nil
}

func canonicalModelDevBindingScope(scope ModelDevReleaseBindingScope) (ModelDevReleaseBindingScope, error) {
	if scope.TenantID == 0 {
		return ModelDevReleaseBindingScope{}, invalidModelDevBindingField("tenant ID")
	}
	var valid bool
	scope.ResourceTenantID, valid = canonicalModelDevBindingUUID(scope.ResourceTenantID)
	if !valid {
		return ModelDevReleaseBindingScope{}, invalidModelDevBindingField("resource tenant ID")
	}
	scope.PresetID, valid = canonicalModelDevBindingUUID(scope.PresetID)
	if !valid {
		return ModelDevReleaseBindingScope{}, invalidModelDevBindingField("preset ID")
	}
	return scope, nil
}

func canonicalModelDevBindingUpdate(update ModelDevReleaseBindingUpdate) (ModelDevReleaseBindingUpdate, error) {
	var valid bool
	update.Target.ReleaseID, valid = canonicalModelDevBindingUUID(update.Target.ReleaseID)
	if !valid {
		return ModelDevReleaseBindingUpdate{}, invalidModelDevBindingField("release ID")
	}
	digest := update.Target.ReleaseDigest
	if len(digest) != 64 || strings.ToLower(digest) != digest {
		return ModelDevReleaseBindingUpdate{}, invalidModelDevBindingField("release digest")
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return ModelDevReleaseBindingUpdate{}, invalidModelDevBindingField("release digest")
	}
	if !cpup01.ValidAuditActor(update.Actor) {
		return ModelDevReleaseBindingUpdate{}, invalidModelDevBindingField("audit actor")
	}
	update.RequestedAt = update.RequestedAt.UTC()
	if update.RequestedAt.IsZero() || update.RequestedAt.Year() < 1 || update.RequestedAt.Year() > 9999 || update.RequestedAt.Nanosecond()%1000 != 0 {
		return ModelDevReleaseBindingUpdate{}, invalidModelDevBindingField("audit time")
	}
	if !validModelDevBindingAuditText(update.Reason) {
		return ModelDevReleaseBindingUpdate{}, invalidModelDevBindingField("reason")
	}
	if !validModelDevBindingAuditText(update.EvidenceReference) {
		return ModelDevReleaseBindingUpdate{}, invalidModelDevBindingField("evidence reference")
	}
	return update, nil
}

func canonicalModelDevBindingUUID(value string) (string, bool) {
	if len(value) != 36 {
		return "", false
	}
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil {
		return "", false
	}
	return id.String(), true
}

func validModelDevBindingAuditText(value string) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && !strings.ContainsRune(value, 0)
}

func invalidModelDevBindingField(field string) error {
	return fmt.Errorf("%w: modeldev binding %s", cpup01.ErrInvalidArgument, field)
}

func (r *ModelDevReleaseBindingRepo) bindingTransaction(ctx context.Context, work func(*ent.Tx) error) (err error) {
	tx, err := r.entClient.Client().Tx(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
				err = errors.Join(err, fmt.Errorf("rollback modeldev release binding: %w", rollbackErr))
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

func modelDevReleaseBindingFromRow(row *ent.ModelDevReleaseBinding) *ModelDevReleaseBinding {
	return &ModelDevReleaseBinding{
		Scope:      ModelDevReleaseBindingScope{TenantID: *row.TenantID, ResourceTenantID: row.ResourceTenantID, PresetID: row.PresetID},
		Target:     ModelDevReleaseBindingTarget{ReleaseID: row.ReleaseID, ReleaseDigest: row.ReleaseDigest, NewSubmissionsEnabled: row.NewSubmissionsEnabled},
		Generation: row.Generation, UpdatedBy: row.UpdatedBy, UpdatedAt: row.UpdatedAt.UTC(),
		Reason: row.Reason, EvidenceReference: row.EvidenceReference,
	}
}

package data

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"go-wind-admin/app/admin/service/internal/data/ent"
	q "go-wind-admin/app/admin/service/internal/data/quotasql"
)

type GpuCreateCandidate struct {
	ID          int64
	TenantID    uint32
	OperationID string
}
type GpuUsageSyncRecord struct {
	TenantID                        uint32
	OperationID                     string
	Revision                        int64
	State, PayloadJSON, PayloadHash string
	AckedRevision, LeaseGeneration  int64
	AttemptCount                    int
}

func (r *QuotaLedgerRepo) ScanGpuCreatePage(ctx context.Context, afterID int64, limit int) (out []GpuCreateCandidate, err error) {
	if afterID < 0 || limit < 1 || limit > 1000 {
		return nil, QuotaErrInvalid("invalid scan cursor or limit")
	}
	err = r.transaction(ctx, func(tx *q.Queries) error {
		rows, e := tx.ScanGlobalGpuCreatePage(ctx, q.ScanGlobalGpuCreatePageParams{ID: afterID, Limit: int32(limit)})
		if e != nil {
			return e
		}
		for _, v := range rows {
			out = append(out, GpuCreateCandidate{v.ID, uint32(v.TenantID), v.OperationID})
		}
		return nil
	})
	return
}
func (r *QuotaLedgerRepo) UpsertGpuUsageProjection(ctx context.Context, tid uint32, id string, revision int64, state, payload, hash string) error {
	if tid == 0 || id == "" || revision < 1 || payload == "" || hash == "" || (state != "DECLARED" && state != "ENDED") {
		return QuotaErrInvalid("invalid usage projection")
	}
	return r.transaction(ctx, func(tx *q.Queries) error {
		if _, e := tx.LockTenant(ctx, int64(tid)); e != nil {
			return e
		}
		op, e := tx.GetOperation(ctx, q.GetOperationParams{TenantID: int64(tid), OperationID: id})
		if e != nil {
			return e
		}
		if op.CreateOperationID != nil {
			return QuotaErrInvalid("projection requires CREATE")
		}
		old, e := tx.GetGpuUsageSync(ctx, q.GetGpuUsageSyncParams{TenantID: int64(tid), OperationID: id})
		if e == nil && old.Revision == revision {
			if old.PayloadHash != hash || old.PayloadJson != payload || old.State != state {
				return QuotaErrIdempotencyConflict("same projection revision changed payload")
			}
			return nil
		}
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		_, e = tx.UpsertGpuUsageSync(ctx, q.UpsertGpuUsageSyncParams{TenantID: int64(tid), OperationID: id, Revision: revision, State: state, PayloadJson: payload, PayloadHash: hash, ResourceTenantID: op.ResourceTenantID, OwnerService: op.OwnerService, ResourceID: op.ResourceID})
		return e
	})
}
func (r *QuotaLedgerRepo) ClaimGpuUsageSync(ctx context.Context, worker string, lease time.Duration, limit int) (out []GpuUsageSyncRecord, err error) {
	if worker == "" || lease <= 0 || limit < 1 || limit > 100 {
		return nil, QuotaErrInvalid("invalid projection claim")
	}
	var candidates []q.ScanGlobalGpuSyncCandidatesRow
	if err = r.transaction(ctx, func(tx *q.Queries) error {
		var e error
		candidates, e = tx.ScanGlobalGpuSyncCandidates(ctx, int32(limit))
		return e
	}); err != nil {
		return
	}
	for _, c := range candidates {
		var record *GpuUsageSyncRecord
		e := r.transaction(ctx, func(tx *q.Queries) error {
			v, e := tx.ClaimGpuUsageSync(ctx, q.ClaimGpuUsageSyncParams{TenantID: c.TenantID, OperationID: c.OperationID, LeaseOwner: &worker, LeaseMicros: max(1, lease.Microseconds())})
			if e != nil {
				return e
			}
			record = &GpuUsageSyncRecord{uint32(v.TenantID), v.OperationID, v.Revision, v.State, v.PayloadJson, v.PayloadHash, v.AckedRevision, v.LeaseGeneration, int(v.AttemptCount)}
			return nil
		})
		if errors.Is(e, pgx.ErrNoRows) {
			continue
		}
		if e != nil {
			return out, e
		}
		out = append(out, *record)
	}
	return
}
func (r *QuotaLedgerRepo) AckGpuUsageSync(ctx context.Context, tid uint32, id string, revision, generation int64) (ok bool, err error) {
	err = r.transaction(ctx, func(tx *q.Queries) error {
		n, e := tx.AckGpuUsageSync(ctx, q.AckGpuUsageSyncParams{TenantID: int64(tid), OperationID: id, AckedRevision: revision, LeaseGeneration: generation})
		ok = n == 1
		return e
	})
	return
}
func (r *QuotaLedgerRepo) RetryGpuUsageSync(ctx context.Context, tid uint32, id string, revision, generation int64, next time.Time, code string) (ok bool, err error) {
	err = r.transaction(ctx, func(tx *q.Queries) error {
		n, e := tx.RetryGpuUsageSync(ctx, q.RetryGpuUsageSyncParams{TenantID: int64(tid), OperationID: id, Revision: revision, LeaseGeneration: generation, NextAttemptAt: &next, LastErrorCode: &code})
		ok = n == 1
		return e
	})
	return
}

// LoadGpuProjectionSource holds the original operation lock while reading all
// accounting facts, so concurrent release/delete cannot split the snapshot.
func (r *QuotaLedgerRepo) LoadGpuProjectionSource(ctx context.Context, tid uint32, id string) (op *ent.QuotaOperation, charges []*ent.QuotaCharge, hasDelete bool, err error) {
	err = r.transaction(ctx, func(tx *q.Queries) error {
		v, e := tx.LockOperation(ctx, q.LockOperationParams{TenantID: int64(tid), OperationID: id})
		if e != nil {
			return e
		}
		if v.CreateOperationID != nil {
			return QuotaErrInvalid("projection source must be CREATE")
		}
		rows, e := tx.ListCharges(ctx, q.ListChargesParams{TenantID: int64(tid), OperationID: id})
		if e != nil {
			return e
		}
		if e = validateFrozenCharges(v.CanonicalRequest, rows); e != nil {
			return e
		}
		hasDelete, e = tx.HasDeleteIntent(ctx, q.HasDeleteIntentParams{TenantID: int64(tid), CreateOperationID: &id, OwnerService: v.OwnerService})
		if e != nil {
			return e
		}
		op = operationDTO(v)
		for _, c := range rows {
			charges = append(charges, chargeDTO(c))
		}
		return nil
	})
	return
}

// BlockGpuUsageSync records a permanent payload/contract error without touching
// business dispatch or balances. A later source revision re-enables projection.
func (r *QuotaLedgerRepo) BlockGpuUsageSync(ctx context.Context, tid uint32, id string, revision, generation int64, code string) (ok bool, err error) {
	err = r.transaction(ctx, func(tx *q.Queries) error {
		n, e := tx.BlockGpuUsageSync(ctx, q.BlockGpuUsageSyncParams{TenantID: int64(tid), OperationID: id, Revision: revision, LeaseGeneration: generation, LastErrorCode: &code})
		ok = n == 1
		return e
	})
	return
}

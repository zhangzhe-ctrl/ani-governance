package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	entCrud "github.com/tx7do/go-crud/entgo"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/quotaoperation"
	q "go-wind-admin/app/admin/service/internal/data/quotasql"
)

type QuotaLedgerRepo struct {
	entClient *entCrud.EntClient[*ent.Client]
	log       *bLogger.Helper
}

func NewQuotaLedgerRepo(ctx *bootstrap.Context, c *entCrud.EntClient[*ent.Client]) *QuotaLedgerRepo {
	return &QuotaLedgerRepo{c, ctx.NewLoggerHelper("quota-ledger/repo/admin-service")}
}
func NewQuotaLedgerRepoForTest(c *entCrud.EntClient[*ent.Client], log *bLogger.Helper) *QuotaLedgerRepo {
	return &QuotaLedgerRepo{c, log}
}
func (r *QuotaLedgerRepo) DB() *sql.DB                   { return r.entClient.DB() }
func (r *QuotaLedgerRepo) EntClientForTest() *ent.Client { return r.entClient.Client() }
func (r *QuotaLedgerRepo) transaction(ctx context.Context, fn func(*q.Queries) error) error {
	return quotaTransaction(ctx, r.entClient.DB(), fn)
}
func generateUUID() string { return uuid.NewString() }
func ptr[T any](v T) *T    { return &v }

type QuotaOccupyItem struct {
	QuotaCode string `json:"quota_code"`
	Units     int64  `json:"units"`
}
type QuotaOccupyInput struct {
	TenantID                                                                                                              uint32
	ResourceTenantID, ResourceID, ActorType, ActorID, OwnerService, Action, IdempotencyKey, RequestHash, CanonicalRequest string
	Items                                                                                                                 []QuotaOccupyItem
}
type QuotaOccupyResult struct {
	OperationID, ResourceID string
	ChargeIDs               []string
	Replayed                bool
}
type QuotaReleaseInput struct {
	OwnerService, ReleaseEventID, OperationID, Reason, PayloadHash, PayloadJSON string
	Items                                                                       []QuotaReleaseItemInput
}
type QuotaReleaseItemInput struct {
	ChargeID, QuotaCode string
	ReleasedTotal       int64
}
type QuotaReleaseResult struct {
	ChargeID                    string
	AppliedDelta, ReleasedTotal int64
}
type QuotaDeleteInput struct {
	TenantID                                                                                            uint32
	ActorType, ActorID, OwnerService, Action, IdempotencyKey, RequestHash, CanonicalRequest, ResourceID string
}
type QuotaDeleteResult struct {
	OperationID, CreateOperationID, ChargeID, ResourceID string
	ChargeIDs                                            []string
	Replayed, LocalCanceled                              bool
}
type QuotaChargeRef struct {
	ChargeID, QuotaCode string
	ChargedUnits        int64
}
type ClaimedOperation struct {
	ID, TenantID                                                                                                                          uint32
	OperationID, ResourceTenantID, ResourceID, CreateOperationID, ActorType, ActorID, OwnerService, Action, RequestHash, CanonicalRequest string
	LeaseGeneration                                                                                                                       int64
	AttemptCount                                                                                                                          int
	Charges                                                                                                                               []QuotaChargeRef
}
type InvariantRow struct {
	TenantID                       uint32
	QuotaCode                      string
	OccupiedUnits, ChargeRemainder int64
	Balanced                       bool
}

func operationDTO(v q.SysQuotaOperation) *ent.QuotaOperation {
	return &ent.QuotaOperation{ID: uint32(v.ID), TenantID: ptr(uint32(v.TenantID)), CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, DeletedAt: v.DeletedAt, OperationID: v.OperationID, ResourceTenantID: v.ResourceTenantID, ResourceID: v.ResourceID, CreateOperationID: v.CreateOperationID, ActorType: v.ActorType, ActorID: v.ActorID, OwnerService: v.OwnerService, Action: v.Action, IdempotencyKey: v.IdempotencyKey, RequestHash: v.RequestHash, CanonicalRequest: v.CanonicalRequest, DispatchState: quotaoperation.DispatchState(v.DispatchState), AttemptCount: int(v.AttemptCount), LeaseGeneration: v.LeaseGeneration, RetryBlocked: v.RetryBlocked, LastErrorCode: v.LastErrorCode, NextAttemptAt: v.NextAttemptAt, LeaseOwner: v.LeaseOwner, LeaseUntil: v.LeaseUntil, AckJSON: v.AckJson}
}
func chargeDTO(v q.SysQuotaCharge) *ent.QuotaCharge {
	return &ent.QuotaCharge{ID: uint32(v.ID), TenantID: ptr(uint32(v.TenantID)), CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, DeletedAt: v.DeletedAt, ChargeID: v.ChargeID, OperationID: v.OperationID, QuotaCode: v.QuotaCode, OriginalUnits: v.OriginalUnits, ReleasedUnits: v.ReleasedUnits}
}
func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return &ent.NotFoundError{}
	}
	return err
}
func chargeIDs(charges []q.SysQuotaCharge) []string {
	out := make([]string, len(charges))
	for i, c := range charges {
		out[i] = c.ChargeID
	}
	return out
}
func isGPUCode(code string) bool {
	return code == "gpu.physical.count" || code == "gpu.shared_memory_mib"
}

// validateFrozenCharges checks the entire immutable business vector for GPU
// operations. Legacy operations retain their historical non-GPU semantics.
func validateFrozenCharges(canonical string, charges []q.SysQuotaCharge) error {
	gpu := false
	for _, c := range charges {
		gpu = gpu || isGPUCode(c.QuotaCode)
	}
	var frozen struct {
		SchemaVersion int               `json:"schema_version"`
		GpuPlan       json.RawMessage   `json:"gpu_plan"`
		QuotaItems    []QuotaOccupyItem `json:"quota_items"`
	}
	if err := json.Unmarshal([]byte(canonical), &frozen); err != nil {
		return QuotaErrInvalid("invalid immutable GPU request")
	}
	for _, item := range frozen.QuotaItems {
		gpu = gpu || isGPUCode(item.QuotaCode)
	}
	if !gpu && frozen.SchemaVersion != 2 && len(frozen.GpuPlan) == 0 {
		return nil
	}
	if len(frozen.QuotaItems) != len(charges) || len(charges) == 0 {
		return QuotaErrInvalid("incomplete immutable charge vector")
	}
	expected := map[string]int64{}
	for _, it := range frozen.QuotaItems {
		if it.Units <= 0 || expected[it.QuotaCode] != 0 {
			return QuotaErrInvalid("invalid immutable charge vector")
		}
		expected[it.QuotaCode] = it.Units
	}
	for _, c := range charges {
		if expected[c.QuotaCode] != c.OriginalUnits {
			return QuotaErrInvalid("immutable charge vector mismatch")
		}
	}
	return nil
}

func (r *QuotaLedgerRepo) Occupy(ctx context.Context, in *QuotaOccupyInput) (out *QuotaOccupyResult, err error) {
	if in == nil || in.TenantID == 0 || len(in.Items) == 0 || in.IdempotencyKey == "" || in.RequestHash == "" || in.CanonicalRequest == "" || in.ResourceTenantID == "" || in.ResourceID == "" || in.OwnerService == "" || in.Action == "" || in.ActorType == "" || in.ActorID == "" {
		return nil, QuotaErrInvalid("incomplete quota input")
	}
	items := append([]QuotaOccupyItem(nil), in.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].QuotaCode < items[j].QuotaCode })
	for i, it := range items {
		if it.Units <= 0 || it.QuotaCode == "" || (i > 0 && items[i-1].QuotaCode == it.QuotaCode) {
			return nil, QuotaErrInvalid("invalid or duplicate quota item")
		}
	}
	err = r.transaction(ctx, func(tx *q.Queries) error {
		tenant, err := tx.LockTenant(ctx, int64(in.TenantID))
		if errors.Is(err, pgx.ErrNoRows) {
			return QuotaErrAdmissionDenied("tenant not found")
		}
		if err != nil {
			return err
		}
		// Replay precedes current subscription/capability checks and reuses the first snapshot.
		op, err := tx.GetIdempotentOperation(ctx, q.GetIdempotentOperationParams{TenantID: int64(in.TenantID), ActorType: in.ActorType, ActorID: in.ActorID, Action: in.Action, IdempotencyKey: in.IdempotencyKey})
		if err == nil {
			if op.RequestHash != in.RequestHash || op.OwnerService != in.OwnerService {
				return QuotaErrIdempotencyConflict("idempotency payload mismatch")
			}
			charges, e := tx.ListCharges(ctx, q.ListChargesParams{TenantID: int64(in.TenantID), OperationID: op.OperationID})
			if e != nil {
				return e
			}
			if e = validateFrozenCharges(op.CanonicalRequest, charges); e != nil {
				return e
			}
			out = &QuotaOccupyResult{op.OperationID, op.ResourceID, chargeIDs(charges), true}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if tenant.ResourceTenantID != in.ResourceTenantID {
			return QuotaErrAdmissionDenied("resource tenant mapping mismatch")
		}
		if tenant.Status == nil || *tenant.Status != "ON" || (tenant.ExpiredAt != nil && !tenant.ExpiredAt.After(tenant.DatabaseNow)) {
			return QuotaErrAdmissionDenied("tenant is inactive or expired")
		}
		if tenant.PlanID == nil || *tenant.PlanID == 0 {
			return QuotaErrNotConfigured("tenant has no plan")
		}
		if _, err = tx.LockPlanShared(ctx, *tenant.PlanID); err != nil {
			return err
		}
		policies, err := tx.ListPlanPolicies(ctx, *tenant.PlanID)
		if err != nil {
			return err
		}
		limits := map[string]int64{}
		for _, p := range policies {
			limits[p.QuotaCode] = p.QuotaValue
		}
		for _, it := range items {
			limit, ok := limits[it.QuotaCode]
			if !ok {
				return QuotaErrNotConfigured("quota not configured: " + it.QuotaCode)
			}
			if err = tx.EnsureAccount(ctx, q.EnsureAccountParams{TenantID: int64(in.TenantID), QuotaCode: it.QuotaCode}); err != nil {
				return err
			}
			acc, e := tx.LockAccount(ctx, q.LockAccountParams{TenantID: int64(in.TenantID), QuotaCode: it.QuotaCode})
			if e != nil {
				return e
			}
			if it.Units > limit || acc.OccupiedUnits > limit-it.Units {
				return QuotaErrExceeded("quota exceeded: " + it.QuotaCode)
			}
		}
		op, err = tx.InsertOperation(ctx, q.InsertOperationParams{OperationID: generateUUID(), TenantID: int64(in.TenantID), ResourceTenantID: in.ResourceTenantID, ResourceID: in.ResourceID, ActorType: in.ActorType, ActorID: in.ActorID, OwnerService: in.OwnerService, Action: in.Action, IdempotencyKey: in.IdempotencyKey, RequestHash: in.RequestHash, CanonicalRequest: in.CanonicalRequest, DispatchState: "QUEUED"})
		if err != nil {
			return err
		}
		charges := make([]q.SysQuotaCharge, 0, len(items))
		byCode := map[string]string{}
		for _, it := range items {
			c, e := tx.InsertCharge(ctx, q.InsertChargeParams{ChargeID: generateUUID(), TenantID: int64(in.TenantID), OperationID: op.OperationID, QuotaCode: it.QuotaCode, OriginalUnits: it.Units})
			if e != nil {
				return e
			}
			charges = append(charges, c)
			byCode[it.QuotaCode] = c.ChargeID
			if e = changeAccount(ctx, tx, int64(in.TenantID), it.QuotaCode, it.Units); e != nil {
				return e
			}
		}
		if err = validateFrozenCharges(op.CanonicalRequest, charges); err != nil {
			return err
		}
		ids := make([]string, len(in.Items))
		for i, it := range in.Items {
			ids[i] = byCode[it.QuotaCode]
		}
		out = &QuotaOccupyResult{op.OperationID, op.ResourceID, ids, false}
		return nil
	})
	if err != nil {
		return nil, r.storageError(ctx, err)
	}
	return out, nil
}
func changeAccount(ctx context.Context, tx *q.Queries, tenant int64, code string, delta int64) error {
	n, e := tx.ChangeAccount(ctx, q.ChangeAccountParams{TenantID: tenant, QuotaCode: code, Delta: delta})
	if e != nil {
		return e
	}
	if n != 1 {
		return fmt.Errorf("quota account invariant violation")
	}
	return nil
}
func (r *QuotaLedgerRepo) storageError(ctx context.Context, err error) error {
	// Preserve typed business errors; never leak connection errors or storage
	// diagnostics through the BFF (including a closed pool/unreachable DB).
	var business *kratosErrors.Error
	if errors.As(err, &business) {
		return err
	}
	r.log.Errorf(ctx, "quota transaction: %v", err)
	return QuotaErrStorageUnavailable("quota transaction failed")
}

func (r *QuotaLedgerRepo) Release(ctx context.Context, in *QuotaReleaseInput) (out []QuotaReleaseResult, err error) {
	if in == nil || len(in.Items) == 0 || in.OwnerService == "" || in.OperationID == "" || in.ReleaseEventID == "" || in.PayloadHash == "" {
		return nil, releaseErrInvalid("incomplete release")
	}
	err = r.transaction(ctx, func(tx *q.Queries) error {
		// Sole owner-scoped locator: certificate owner plus original CREATE, before
		// explicit tenant locking. Caller-provided tenant never authorizes a refund.
		tid, e := tx.LocateReleaseOwnerOperation(ctx, q.LocateReleaseOwnerOperationParams{OperationID: in.OperationID, OwnerService: in.OwnerService})
		if errors.Is(e, pgx.ErrNoRows) {
			return releaseErrPermissionDenied("release owner or operation mismatch")
		}
		if e != nil {
			return e
		}
		if _, e = tx.LockTenant(ctx, tid); e != nil {
			return e
		}
		op, e := tx.LockOperation(ctx, q.LockOperationParams{TenantID: tid, OperationID: in.OperationID})
		if e != nil {
			return e
		}
		charges, e := tx.LockCharges(ctx, q.LockChargesParams{TenantID: tid, OperationID: in.OperationID})
		if e != nil {
			return e
		}
		if e = validateFrozenCharges(op.CanonicalRequest, charges); e != nil {
			return releaseErrConflict("invalid original charge vector")
		}
		byID := map[string]q.SysQuotaCharge{}
		for _, c := range charges {
			byID[c.ChargeID] = c
		}
		incoming := map[string]QuotaReleaseItemInput{}
		gpu := false
		for _, it := range in.Items {
			c, ok := byID[it.ChargeID]
			if !ok {
				return releaseErrNotFound("unknown charge")
			}
			if _, duplicate := incoming[it.ChargeID]; duplicate {
				return releaseErrInvalid("duplicate charge")
			}
			if c.QuotaCode != it.QuotaCode || it.ReleasedTotal < 0 || it.ReleasedTotal > c.OriginalUnits {
				return releaseErrConflict("invalid charge total or code")
			}
			incoming[it.ChargeID] = it
			gpu = gpu || isGPUCode(it.QuotaCode)
		}
		if gpu {
			for _, c := range charges {
				if isGPUCode(c.QuotaCode) {
					it, ok := incoming[c.ChargeID]
					if !ok || it.ReleasedTotal != c.OriginalUnits {
						return releaseErrConflict("GPU release requires complete original GPU vector and full cumulative totals")
					}
				}
			}
			has, e := tx.HasDeleteIntent(ctx, q.HasDeleteIntentParams{TenantID: tid, CreateOperationID: &in.OperationID, OwnerService: in.OwnerService})
			if e != nil {
				return e
			}
			if !has {
				return releaseErrConflict("GPU release requires persisted DELETE intent")
			}
			if in.Reason != "RESOURCE_RELEASED" && in.Reason != "ABORTED_CLEANED" {
				return releaseErrConflict("invalid GPU release reason")
			}
		}
		receipt, e := tx.GetReleaseReceipt(ctx, q.GetReleaseReceiptParams{TenantID: tid, OwnerService: in.OwnerService, ReleaseEventID: in.ReleaseEventID})
		replayed := e == nil
		if replayed && receipt.PayloadHash != in.PayloadHash {
			return releaseErrConflict("event payload mismatch")
		}
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		out = make([]QuotaReleaseResult, 0, len(in.Items))
		for _, c := range charges {
			it, ok := incoming[c.ChargeID]
			if !ok {
				continue
			}
			total := c.ReleasedUnits
			delta := int64(0)
			if !replayed && it.ReleasedTotal > total {
				delta = it.ReleasedTotal - total
				total = it.ReleasedTotal
				if _, e = tx.LockAccount(ctx, q.LockAccountParams{TenantID: tid, QuotaCode: c.QuotaCode}); e != nil {
					return e
				}
				if e = changeAccount(ctx, tx, tid, c.QuotaCode, -delta); e != nil {
					return e
				}
				if _, e = tx.SetReleasedTotal(ctx, q.SetReleasedTotalParams{TenantID: tid, ChargeID: c.ChargeID, ReleasedUnits: total}); e != nil {
					return e
				}
			}
			out = append(out, QuotaReleaseResult{c.ChargeID, delta, total})
		}
		if replayed {
			return nil
		}
		return tx.InsertReleaseReceipt(ctx, q.InsertReleaseReceiptParams{ReceiptID: generateUUID(), TenantID: tid, OwnerService: in.OwnerService, ReleaseEventID: in.ReleaseEventID, PayloadHash: in.PayloadHash, PayloadJson: in.PayloadJSON})
	})
	return out, err
}

func cancelLocked(ctx context.Context, tx *q.Queries, op q.SysQuotaOperation, charges []q.SysQuotaCharge) error {
	if op.DispatchState == "CANCELED_UNSENT" {
		if op.AttemptCount != 0 || len(charges) == 0 {
			return QuotaErrInvalid("inconsistent local cancellation")
		}
		if e := validateFrozenCharges(op.CanonicalRequest, charges); e != nil {
			return e
		}
		for _, charge := range charges {
			if charge.ReleasedUnits != charge.OriginalUnits {
				return QuotaErrInvalid("local cancellation has incomplete refunds")
			}
		}
		return nil
	}
	if op.DispatchState != "QUEUED" || op.AttemptCount != 0 {
		return QuotaErrInvalid("owner closure required after a send attempt")
	}
	if len(charges) == 0 {
		return QuotaErrInvalid("missing original charges")
	}
	if e := validateFrozenCharges(op.CanonicalRequest, charges); e != nil {
		return e
	}
	for _, c := range charges {
		delta := c.OriginalUnits - c.ReleasedUnits
		if delta == 0 {
			continue
		}
		if _, e := tx.LockAccount(ctx, q.LockAccountParams{TenantID: op.TenantID, QuotaCode: c.QuotaCode}); e != nil {
			return e
		}
		if e := changeAccount(ctx, tx, op.TenantID, c.QuotaCode, -delta); e != nil {
			return e
		}
		if _, e := tx.SetReleasedTotal(ctx, q.SetReleasedTotalParams{TenantID: op.TenantID, ChargeID: c.ChargeID, ReleasedUnits: c.OriginalUnits}); e != nil {
			return e
		}
	}
	n, e := tx.SetCanceledUnsent(ctx, q.SetCanceledUnsentParams{TenantID: op.TenantID, OperationID: op.OperationID})
	if e != nil {
		return e
	}
	if n != 1 {
		return QuotaErrInvalid("operation concurrently claimed")
	}
	return nil
}
func (r *QuotaLedgerRepo) CancelUnsent(ctx context.Context, tid uint32, id string) error {
	return r.transaction(ctx, func(tx *q.Queries) error {
		if _, e := tx.LockTenant(ctx, int64(tid)); e != nil {
			return e
		}
		op, e := tx.LockOperation(ctx, q.LockOperationParams{TenantID: int64(tid), OperationID: id})
		if e != nil {
			return e
		}
		charges, e := tx.LockCharges(ctx, q.LockChargesParams{TenantID: int64(tid), OperationID: id})
		if e != nil {
			return e
		}
		return cancelLocked(ctx, tx, op, charges)
	})
}
func (r *QuotaLedgerRepo) CreateDeleteOperation(ctx context.Context, in *QuotaDeleteInput) (out *QuotaDeleteResult, err error) {
	if in == nil || in.TenantID == 0 || in.IdempotencyKey == "" || in.RequestHash == "" || in.ResourceID == "" || in.OwnerService == "" || in.ActorType == "" || in.ActorID == "" || in.Action == "" || in.CanonicalRequest == "" {
		return nil, QuotaErrInvalid("incomplete delete input")
	}
	err = r.transaction(ctx, func(tx *q.Queries) error {
		tid := int64(in.TenantID)
		if _, e := tx.LockTenant(ctx, tid); e != nil {
			return e
		}
		existing, e := tx.GetIdempotentOperation(ctx, q.GetIdempotentOperationParams{TenantID: tid, ActorType: in.ActorType, ActorID: in.ActorID, Action: in.Action, IdempotencyKey: in.IdempotencyKey})
		replay := e == nil
		if replay && (existing.RequestHash != in.RequestHash || existing.OwnerService != in.OwnerService || existing.ResourceID != in.ResourceID) {
			return QuotaErrIdempotencyConflict("delete idempotency mismatch")
		}
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		original, e := tx.FindCreateOperation(ctx, q.FindCreateOperationParams{TenantID: tid, OwnerService: in.OwnerService, ResourceID: in.ResourceID})
		if errors.Is(e, pgx.ErrNoRows) {
			return QuotaErrNotFound("resource not found")
		}
		if e != nil {
			return e
		}
		original, e = tx.LockOperation(ctx, q.LockOperationParams{TenantID: tid, OperationID: original.OperationID})
		if e != nil {
			return e
		}
		charges, e := tx.LockCharges(ctx, q.LockChargesParams{TenantID: tid, OperationID: original.OperationID})
		if e != nil {
			return e
		}
		if len(charges) == 0 {
			return QuotaErrInvalid("missing original charge vector")
		}
		if e = validateFrozenCharges(original.CanonicalRequest, charges); e != nil {
			return e
		}
		gpu := false
		for _, charge := range charges {
			gpu = gpu || isGPUCode(charge.QuotaCode)
		}
		if replay {
			if existing.CreateOperationID == nil || *existing.CreateOperationID != original.OperationID {
				return QuotaErrIdempotencyConflict("delete origin mismatch")
			}
			if gpu {
				accepted, e := tx.GetGpuDeleteAcceptance(ctx, q.GetGpuDeleteAcceptanceParams{TenantID: tid, DeleteOperationID: existing.OperationID})
				if e != nil {
					return e
				}
				if accepted.CreateOperationID != original.OperationID || accepted.RequestHash != in.RequestHash {
					return QuotaErrIdempotencyConflict("GPU delete acceptance mismatch")
				}
			}
			out = deleteResult(existing, charges, true)
			return nil
		}
		local := original.DispatchState == "CANCELED_UNSENT" || (original.DispatchState == "QUEUED" && original.AttemptCount == 0)
		if local {
			if e = cancelLocked(ctx, tx, original, charges); e != nil {
				return e
			}
		}
		state := "QUEUED"
		if local {
			state = "CANCELED_UNSENT"
		}
		// DELETE carries the exact original frozen request, never a newly resolved plan.
		op, e := tx.InsertOperation(ctx, q.InsertOperationParams{OperationID: generateUUID(), TenantID: tid, ResourceTenantID: original.ResourceTenantID, ResourceID: original.ResourceID, CreateOperationID: &original.OperationID, ActorType: in.ActorType, ActorID: in.ActorID, OwnerService: in.OwnerService, Action: in.Action, IdempotencyKey: in.IdempotencyKey, RequestHash: in.RequestHash, CanonicalRequest: original.CanonicalRequest, DispatchState: state})
		if e != nil {
			return e
		}
		if gpu {
			result := "OWNER_DELETE"
			if local {
				result = "LOCAL_CANCELED"
			}
			if e = tx.InsertGpuDeleteAcceptance(ctx, q.InsertGpuDeleteAcceptanceParams{TenantID: tid, ActorType: in.ActorType, ActorID: in.ActorID, Action: in.Action, IdempotencyKey: in.IdempotencyKey, RequestHash: in.RequestHash, CreateOperationID: original.OperationID, DeleteOperationID: op.OperationID, Result: result}); e != nil {
				return e
			}
		}
		out = deleteResult(op, charges, false)
		return nil
	})
	return out, err
}
func deleteResult(op q.SysQuotaOperation, charges []q.SysQuotaCharge, replay bool) *QuotaDeleteResult {
	result := &QuotaDeleteResult{OperationID: op.OperationID, ResourceID: op.ResourceID, ChargeIDs: chargeIDs(charges), Replayed: replay, LocalCanceled: op.DispatchState == "CANCELED_UNSENT"}
	if op.CreateOperationID != nil {
		result.CreateOperationID = *op.CreateOperationID
	}
	if len(charges) == 1 {
		result.ChargeID = charges[0].ChargeID
	}
	return result
}

func (r *QuotaLedgerRepo) FindIdempotentOperation(ctx context.Context, tid uint32, actorType, actorID, action, key string) (out *ent.QuotaOperation, err error) {
	err = r.transaction(ctx, func(tx *q.Queries) error {
		v, e := tx.GetIdempotentOperation(ctx, q.GetIdempotentOperationParams{TenantID: int64(tid), ActorType: actorType, ActorID: actorID, Action: action, IdempotencyKey: key})
		if e == nil {
			out = operationDTO(v)
		}
		return e
	})
	return out, notFound(err)
}
func (r *QuotaLedgerRepo) GetOperationForUser(ctx context.Context, tid uint32, id string) (out *ent.QuotaOperation, err error) {
	err = r.transaction(ctx, func(tx *q.Queries) error {
		v, e := tx.GetOperation(ctx, q.GetOperationParams{TenantID: int64(tid), OperationID: id})
		if e == nil {
			out = operationDTO(v)
		}
		return e
	})
	return out, notFound(err)
}
func (r *QuotaLedgerRepo) GetCreateOperationByResource(ctx context.Context, tid uint32, owner, resource string) (out *ent.QuotaOperation, err error) {
	err = r.transaction(ctx, func(tx *q.Queries) error {
		v, e := tx.FindCreateOperation(ctx, q.FindCreateOperationParams{TenantID: int64(tid), OwnerService: owner, ResourceID: resource})
		if e == nil {
			out = operationDTO(v)
		}
		return e
	})
	return out, notFound(err)
}
func (r *QuotaLedgerRepo) GetChargesForOperation(ctx context.Context, tid uint32, id string) (out []*ent.QuotaCharge, err error) {
	err = r.transaction(ctx, func(tx *q.Queries) error {
		v, e := tx.ListCharges(ctx, q.ListChargesParams{TenantID: int64(tid), OperationID: id})
		if e != nil {
			return e
		}
		if len(v) == 0 {
			return &ent.NotFoundError{}
		}
		for _, c := range v {
			out = append(out, chargeDTO(c))
		}
		return nil
	})
	return out, err
}
func (r *QuotaLedgerRepo) GetChargeForOperation(ctx context.Context, tid uint32, id string) (*ent.QuotaCharge, error) {
	cs, e := r.GetChargesForOperation(ctx, tid, id)
	if e != nil {
		return nil, e
	}
	if len(cs) != 1 {
		return nil, QuotaErrInvalid("single-charge interface cannot represent the complete operation")
	}
	return cs[0], nil
}
func (r *QuotaLedgerRepo) FindCreateOperationByResource(ctx context.Context, tid uint32, owner, resource string) (*ent.QuotaOperation, *ent.QuotaCharge, error) {
	op, e := r.GetCreateOperationByResource(ctx, tid, owner, resource)
	if e != nil {
		return nil, nil, e
	}
	c, e := r.GetChargeForOperation(ctx, tid, op.OperationID)
	return op, c, e
}
func (r *QuotaLedgerRepo) RecomputeInvariants(ctx context.Context, tid uint32) (out []InvariantRow, err error) {
	if tid == 0 {
		return nil, QuotaErrInvalid("tenant is required")
	}
	err = r.transaction(ctx, func(tx *q.Queries) error {
		rows, e := tx.RecomputeTenantInvariants(ctx, int64(tid))
		if e != nil {
			return e
		}
		for _, v := range rows {
			out = append(out, InvariantRow{uint32(v.TenantID), v.QuotaCode, v.OccupiedUnits, v.ChargeRemainder, v.OccupiedUnits == v.ChargeRemainder})
		}
		return nil
	})
	return
}

// Global scanning is bounded and returns the tenant. Every claim, read and CAS
// after that scan is explicitly tenant scoped.
func (r *QuotaLedgerRepo) ClaimDispatchable(ctx context.Context, worker string, lease time.Duration, limit int) (out []ClaimedOperation, err error) {
	if limit < 1 || limit > 100 || worker == "" || lease <= 0 {
		return nil, QuotaErrInvalid("invalid worker claim")
	}
	var candidates []q.ScanGlobalDispatchCandidatesRow
	if err = r.transaction(ctx, func(tx *q.Queries) error {
		var e error
		candidates, e = tx.ScanGlobalDispatchCandidates(ctx, int32(limit))
		return e
	}); err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		var claimed *ClaimedOperation
		e := r.transaction(ctx, func(tx *q.Queries) error {
			// Tenant-first lock order serializes cancellation, release and worker claim.
			if _, e := tx.LockTenant(ctx, candidate.TenantID); e != nil {
				return e
			}
			op, e := tx.ClaimOperation(ctx, q.ClaimOperationParams{TenantID: candidate.TenantID, OperationID: candidate.OperationID, LeaseOwner: &worker, LeaseMicros: max(1, lease.Microseconds())})
			if e != nil {
				return e
			}
			origin := op.OperationID
			if op.CreateOperationID != nil {
				origin = *op.CreateOperationID
			}
			charges, e := tx.ListCharges(ctx, q.ListChargesParams{TenantID: op.TenantID, OperationID: origin})
			if e != nil {
				return e
			}
			if len(charges) == 0 || validateFrozenCharges(op.CanonicalRequest, charges) != nil {
				// Keep the failed claim and its diagnostic durable. Rolling this
				// transaction back would repeatedly select the same broken vector
				// without recording an attempt and could starve healthy operations.
				// A repaired original vector is retried without changing accounting.
				code := "ORIGINAL_CHARGES_INVALID"
				delay := min(30*time.Second, time.Second<<min(max(op.AttemptCount-1, 0), 5))
				next := time.Now().Add(delay)
				n, e := tx.MarkOperationUnknown(ctx, q.MarkOperationUnknownParams{TenantID: op.TenantID, OperationID: op.OperationID, LeaseGeneration: op.LeaseGeneration, NextAttemptAt: &next, LastErrorCode: &code})
				if e != nil {
					return e
				}
				if n != 1 {
					return QuotaErrInvalid("failed to retain invalid original charge claim")
				}
				return nil
			}
			v := ClaimedOperation{ID: uint32(op.ID), TenantID: uint32(op.TenantID), OperationID: op.OperationID, ResourceTenantID: op.ResourceTenantID, ResourceID: op.ResourceID, ActorType: op.ActorType, ActorID: op.ActorID, OwnerService: op.OwnerService, Action: op.Action, RequestHash: op.RequestHash, CanonicalRequest: op.CanonicalRequest, LeaseGeneration: op.LeaseGeneration, AttemptCount: int(op.AttemptCount)}
			if op.CreateOperationID != nil {
				v.CreateOperationID = *op.CreateOperationID
			}
			for _, c := range charges {
				v.Charges = append(v.Charges, QuotaChargeRef{c.ChargeID, c.QuotaCode, c.OriginalUnits})
			}
			claimed = &v
			return nil
		})
		if errors.Is(e, pgx.ErrNoRows) {
			continue
		}
		if e != nil {
			return out, e
		}
		if claimed != nil {
			out = append(out, *claimed)
		}
	}
	return out, nil
}
func (r *QuotaLedgerRepo) AckDispatched(ctx context.Context, tid uint32, id string, generation int64, ack string) (ok bool, err error) {
	err = r.transaction(ctx, func(tx *q.Queries) error {
		n, e := tx.AckOperation(ctx, q.AckOperationParams{TenantID: int64(tid), OperationID: id, LeaseGeneration: generation, AckJson: &ack})
		ok = n == 1
		return e
	})
	return
}
func (r *QuotaLedgerRepo) MarkUnknown(ctx context.Context, tid uint32, id string, generation int64, next time.Time, code string, blocked bool) (ok bool, err error) {
	err = r.transaction(ctx, func(tx *q.Queries) error {
		n, e := tx.MarkOperationUnknown(ctx, q.MarkOperationUnknownParams{TenantID: int64(tid), OperationID: id, LeaseGeneration: generation, NextAttemptAt: &next, LastErrorCode: &code, RetryBlocked: blocked})
		ok = n == 1
		return e
	})
	return
}
func (r *QuotaLedgerRepo) ResumeDispatch(ctx context.Context, tid uint32, id string) error {
	return r.transaction(ctx, func(tx *q.Queries) error {
		n, e := tx.ResumeOperation(ctx, q.ResumeOperationParams{TenantID: int64(tid), OperationID: id})
		if e != nil {
			return e
		}
		if n != 1 {
			return QuotaErrNotFound("blocked operation not found")
		}
		return nil
	})
}

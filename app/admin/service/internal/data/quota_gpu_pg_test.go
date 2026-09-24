//go:build quota_pg

package data

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	conf "github.com/tx7do/kratos-bootstrap/api/gen/go/conf/v1"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"go-wind-admin/app/admin/service/internal/data/ent"
	q "go-wind-admin/app/admin/service/internal/data/quotasql"
	appViewer "go-wind-admin/pkg/entgo/viewer"
	"google.golang.org/protobuf/encoding/protojson"
)

//go:embed testdata/gpu/role_and_rls.sql
var gpuRoleAndRLS string

//go:embed testdata/gpu/null_tenant.sql
var gpuNullTenant string

//go:embed testdata/gpu/temp_forbidden.sql
var gpuTempForbidden string

func TestQuotaGpuProductionPgxComposition(t *testing.T) {
	for _, trace := range []bool{false, true} {
		t.Run(fmt.Sprint(trace), func(t *testing.T) {
			dsn := os.Getenv("QUOTA_LAB_PG_DSN")
			require.NotEmpty(t, dsn)
			encoded, e := json.Marshal(map[string]any{"data": map[string]any{"database": map[string]any{"driver": "postgres", "source": dsn, "enableTrace": trace, "migrate": false}}})
			require.NoError(t, e)
			cfg := &conf.Bootstrap{}
			require.NoError(t, protojson.Unmarshal(encoded, cfg))
			app := bootstrap.NewContextWithParam(context.Background(), &conf.AppInfo{}, cfg, bLogger.NopLogger())
			client, cleanup, e := NewEntClient(app)
			require.NoError(t, e)
			defer cleanup()
			require.Equal(t, "postgres", cfg.Data.Database.GetDriver(), "composition must not mutate caller config")
			r := NewQuotaLedgerRepo(app, client)
			require.NoError(t, r.transaction(context.Background(), func(tx *q.Queries) error { _, e := tx.ListDefinitions(context.Background()); return e }))
		})
	}
}

func gpuLedgerInput(t *testing.T, c *ent.Client, tid, pid uint32) *QuotaOccupyInput {
	t.Helper()
	ctx := appViewer.NewSystemViewerContext(context.Background())
	for _, code := range []string{"gpu.physical.count", "storage.bytes"} {
		require.NoError(t, c.PlanQuota.Create().SetPlanID(pid).SetQuotaCode(code).SetQuotaValue(100).Exec(ctx))
	}
	in := occupyInput(tid, "actor", uuid.NewString(), 2)
	in.OwnerService = "ani-inference"
	in.Action = "GPU_CREATE"
	in.Items = []QuotaOccupyItem{{"gpu.physical.count", 2}, {"storage.bytes", 10}}
	blob, e := json.Marshal(map[string]any{"schema_version": 2, "quota_items": in.Items})
	require.NoError(t, e)
	in.CanonicalRequest = string(blob)
	return in
}
func gpuDelete(in *QuotaOccupyInput) *QuotaDeleteInput {
	return &QuotaDeleteInput{TenantID: in.TenantID, ActorType: in.ActorType, ActorID: in.ActorID, OwnerService: in.OwnerService, Action: "GPU_DELETE", IdempotencyKey: uuid.NewString(), RequestHash: "delete-" + in.ResourceID, ResourceID: in.ResourceID, CanonicalRequest: in.CanonicalRequest}
}
func gpuProjectionPayload(in *QuotaOccupyInput, id string, rev int64, hash string) string {
	b, _ := json.Marshal(map[string]any{"ref": map[string]string{"tenant_id": in.ResourceTenantID, "owner_service": in.OwnerService, "resource_id": in.ResourceID, "create_operation_id": id}, "revision": rev, "state": rev, "payload_digest": hash})
	return string(b)
}

func TestQuotaGpuRestrictedRoleAndExplicitIsolation(t *testing.T) {
	c := newLedgerPGClient(t)
	cleanLedger(t, c)
	r := newTestRepo(c)
	ctx := context.Background()
	var compliant bool
	require.NoError(t, c.DB().QueryRowContext(ctx, gpuRoleAndRLS).Scan(&compliant))
	require.True(t, compliant, "must use non-owner restricted runtime with no RLS/policies/TEMP/DDL")
	_, e := c.DB().ExecContext(ctx, gpuNullTenant)
	require.Error(t, e)
	_, e = c.DB().ExecContext(ctx, gpuTempForbidden)
	require.Error(t, e)
	a, p := seedTenantPlan(t, c, "gpu-isolation-a", 20)
	b, pb := seedTenantPlan(t, c, "gpu-isolation-b", 20)
	in := gpuLedgerInput(t, c.Client(), a, p)
	original, e := r.Occupy(ctx, in)
	require.NoError(t, e)
	other := gpuLedgerInput(t, c.Client(), b, pb)
	other.IdempotencyKey = in.IdempotencyKey
	other.ActorType = in.ActorType
	other.ActorID = in.ActorID
	other.ResourceID = in.ResourceID
	own, e := r.Occupy(ctx, other)
	require.NoError(t, e)
	require.NotEqual(t, original.OperationID, own.OperationID)
	_, e = r.GetOperationForUser(ctx, b, original.OperationID)
	require.True(t, ent.IsNotFound(e))
	_, e = r.GetChargesForOperation(ctx, b, original.OperationID)
	require.Error(t, e)
	_, e = r.FindIdempotentOperation(ctx, b, in.ActorType, in.ActorID, in.Action, uuid.NewString())
	require.True(t, ent.IsNotFound(e))
	charges, e := r.GetChargesForOperation(ctx, a, original.OperationID)
	require.NoError(t, e)
	e = r.transaction(ctx, func(tx *q.Queries) error {
		_, e := tx.InsertCharge(ctx, q.InsertChargeParams{ChargeID: uuid.NewString(), TenantID: int64(b), OperationID: original.OperationID, QuotaCode: charges[0].QuotaCode, OriginalUnits: 1})
		return e
	})
	require.Error(t, e, "cross-tenant FK must reject known other-tenant UUID")
	ok, e := r.AckDispatched(ctx, b, original.OperationID, 1, `{}`)
	require.NoError(t, e)
	require.False(t, ok)
	require.Error(t, r.CancelUnsent(ctx, b, original.OperationID))
	// The explicit tenant clause in GetOperation is also the mutation target.
}

func TestQuotaGpuDeleteReleaseAndProjectionRecovery(t *testing.T) {
	c := newLedgerPGClient(t)
	cleanLedger(t, c)
	r := newTestRepo(c)
	ctx := context.Background()
	tid, pid := seedTenantPlan(t, c, "gpu-recovery", 20)
	in := gpuLedgerInput(t, c.Client(), tid, pid)
	accepted, e := r.Occupy(ctx, in)
	require.NoError(t, e)
	charges, e := r.GetChargesForOperation(ctx, tid, accepted.OperationID)
	require.NoError(t, e)
	require.Len(t, charges, 2)
	rel := &QuotaReleaseInput{OwnerService: in.OwnerService, OperationID: accepted.OperationID, ReleaseEventID: uuid.NewString(), Reason: "RESOURCE_RELEASED", PayloadHash: "gpu-full", PayloadJSON: `{"fixture":"ledger-only"}`}
	for _, ch := range charges {
		if isGPUCode(ch.QuotaCode) {
			rel.Items = []QuotaReleaseItemInput{{ch.ChargeID, ch.QuotaCode, ch.OriginalUnits}}
		}
	}
	_, e = r.Release(ctx, rel)
	require.Error(t, e, "full GPU release without DELETE must fail")
	claimed, e := r.ClaimDispatchable(ctx, "owner-worker", time.Minute, 1)
	require.NoError(t, e)
	require.Len(t, claimed, 1)
	deleted, e := r.CreateDeleteOperation(ctx, gpuDelete(in))
	require.NoError(t, e)
	require.False(t, deleted.LocalCanceled)
	require.Len(t, deleted.ChargeIDs, 2)
	// DELETE does not have to be ACKED, but prior failed callback changed nothing.
	_, e = r.Release(ctx, rel)
	require.NoError(t, e)
	rows, e := r.RecomputeInvariants(ctx, tid)
	require.NoError(t, e)
	for _, row := range rows {
		require.True(t, row.Balanced)
		if isGPUCode(row.QuotaCode) {
			require.Zero(t, row.OccupiedUnits)
		} else if row.QuotaCode == "storage.bytes" {
			require.Equal(t, int64(10), row.OccupiedUnits)
		}
	}
	op, all, hasDelete, e := r.LoadGpuProjectionSource(ctx, tid, accepted.OperationID)
	require.NoError(t, e)
	require.True(t, hasDelete)
	require.Len(t, all, 2)
	require.Equal(t, 1, op.AttemptCount)
	// Rebuild a missing row directly at terminal revision; no business attempt changes.
	payload := gpuProjectionPayload(in, accepted.OperationID, 2, "terminal")
	require.NoError(t, r.UpsertGpuUsageProjection(ctx, tid, accepted.OperationID, 2, "ENDED", payload, "terminal"))
	syncs, e := r.ClaimGpuUsageSync(ctx, "sync-a", time.Minute, 1)
	require.NoError(t, e)
	require.Len(t, syncs, 1)
	ok, e := r.AckGpuUsageSync(ctx, tid, accepted.OperationID, 1, syncs[0].LeaseGeneration)
	require.NoError(t, e)
	require.False(t, ok)
	ok, e = r.AckGpuUsageSync(ctx, tid, accepted.OperationID, 2, syncs[0].LeaseGeneration)
	require.NoError(t, e)
	require.True(t, ok)
	require.NoError(t, r.UpsertGpuUsageProjection(ctx, tid, accepted.OperationID, 1, "DECLARED", gpuProjectionPayload(in, accepted.OperationID, 1, "declared"), "declared"))
	syncs, e = r.ClaimGpuUsageSync(ctx, "sync-b", time.Minute, 1)
	require.NoError(t, e)
	require.Empty(t, syncs)
	require.Error(t, r.UpsertGpuUsageProjection(ctx, tid, accepted.OperationID, 2, "ENDED", `{"ref":null}`, "terminal"))
	again, e := r.GetOperationForUser(ctx, tid, accepted.OperationID)
	require.NoError(t, e)
	require.Equal(t, op.AttemptCount, again.AttemptCount)
	// Non-GPU cumulative partial refund remains valid independently of GPU closure.
	for _, ch := range charges {
		if ch.QuotaCode == "storage.bytes" {
			rel.ReleaseEventID = uuid.NewString()
			rel.PayloadHash = "nongpu-partial"
			rel.Items = []QuotaReleaseItemInput{{ch.ChargeID, ch.QuotaCode, 3}}
			out, e := r.Release(ctx, rel)
			require.NoError(t, e)
			require.Equal(t, int64(3), out[0].AppliedDelta)
		}
	}
}

func TestQuotaGpuAtomicCancelAndSameKey(t *testing.T) {
	c := newLedgerPGClient(t)
	cleanLedger(t, c)
	r := newTestRepo(c)
	ctx := context.Background()
	tid, pid := seedTenantPlan(t, c, "gpu-same-key", 20)
	in := gpuLedgerInput(t, c.Client(), tid, pid)
	var wg sync.WaitGroup
	results := make(chan *QuotaOccupyResult, 12)
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); v, e := r.Occupy(ctx, in); results <- v; errs <- e }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	id := ""
	for v := range results {
		if id == "" {
			id = v.OperationID
		}
		require.Equal(t, id, v.OperationID)
	}
	del := gpuDelete(in)
	v, e := r.CreateDeleteOperation(ctx, del)
	require.NoError(t, e)
	require.True(t, v.LocalCanceled)
	replay, e := r.CreateDeleteOperation(ctx, del)
	require.NoError(t, e)
	require.True(t, replay.Replayed)
	require.Equal(t, v.OperationID, replay.OperationID)
	del.IdempotencyKey = uuid.NewString()
	second, e := r.CreateDeleteOperation(ctx, del)
	require.NoError(t, e)
	require.True(t, second.LocalCanceled)
	require.NotEqual(t, v.OperationID, second.OperationID)
	dispatch, e := r.ClaimDispatchable(ctx, "worker", time.Minute, 10)
	require.NoError(t, e)
	require.Empty(t, dispatch)
	balances, e := r.RecomputeInvariants(ctx, tid)
	require.NoError(t, e)
	for _, row := range balances {
		require.Zero(t, row.OccupiedUnits)
		require.True(t, row.Balanced)
	}
	// Current subscription revocation cannot invalidate an already-authorized replay.
	sys := appViewer.NewSystemViewerContext(ctx)
	require.NoError(t, c.Client().Tenant.UpdateOneID(tid).SetExpiredAt(time.Now().Add(-time.Hour)).Exec(sys))
	repeated, e := r.Occupy(ctx, in)
	require.NoError(t, e)
	require.True(t, repeated.Replayed)
	require.Equal(t, id, repeated.OperationID, fmt.Sprintf("original operation %s", id))
	// A missing sole GPU charge still appears in recovery scanning because the
	// immutable canonical marks GPU; source validation must report corruption.
	all, e := r.GetChargesForOperation(ctx, tid, id)
	require.NoError(t, e)
	for _, charge := range all {
		if isGPUCode(charge.QuotaCode) {
			require.NoError(t, c.Client().QuotaCharge.DeleteOneID(charge.ID).Exec(sys))
		}
	}
	candidates, e := r.ScanGpuCreatePage(ctx, 0, 100)
	require.NoError(t, e)
	require.NotEmpty(t, candidates)
	_, _, _, e = r.LoadGpuProjectionSource(ctx, tid, id)
	require.Error(t, e)
}

// TestQuotaGpuRestoredProjectionContinuation runs only against the separately
// restored populated database. It does not seed or clean the restored rows.
func TestQuotaGpuRestoredProjectionContinuation(t *testing.T) {
	if os.Getenv("QUOTA_RESTORE_VERIFY") != "1" {
		t.Skip("separate populated-restore gate")
	}
	c := newLedgerPGClient(t)
	r := newTestRepo(c)
	ctx := context.Background()
	pending, e := r.ClaimGpuUsageSync(ctx, "restore-worker", time.Minute, 10)
	require.NoError(t, e)
	require.NotEmpty(t, pending)
	for _, p := range pending {
		require.Equal(t, int64(2), p.Revision)
		require.Equal(t, "ENDED", p.State)
		ok, e := r.AckGpuUsageSync(ctx, p.TenantID, p.OperationID, p.Revision, p.LeaseGeneration)
		require.NoError(t, e)
		require.True(t, ok)
		rows, e := r.RecomputeInvariants(ctx, p.TenantID)
		require.NoError(t, e)
		for _, v := range rows {
			require.True(t, v.Balanced)
		}
	}
	again, e := r.ClaimGpuUsageSync(ctx, "restore-worker-2", time.Minute, 10)
	require.NoError(t, e)
	require.Empty(t, again)
}

func TestQuotaGpuProjectionNullAndForeignRefConstraints(t *testing.T) {
	c := newLedgerPGClient(t)
	cleanLedger(t, c)
	r := newTestRepo(c)
	ctx := context.Background()
	tid, pid := seedTenantPlan(t, c, "gpu-json-constraints", 20)
	in := gpuLedgerInput(t, c.Client(), tid, pid)
	created, e := r.Occupy(ctx, in)
	require.NoError(t, e)
	params := q.UpsertGpuUsageSyncParams{TenantID: int64(tid), OperationID: created.OperationID, Revision: 1, State: "DECLARED", PayloadHash: "digest", ResourceTenantID: in.ResourceTenantID, OwnerService: in.OwnerService, ResourceID: in.ResourceID}
	for _, payload := range []string{`null`, `{}`, `{"revision":1,"state":1,"payload_digest":"digest","ref":null}`, `{"revision":null,"state":1,"payload_digest":"digest","ref":{}}`} {
		params.PayloadJson = payload
		e = r.transaction(ctx, func(tx *q.Queries) error { _, e := tx.UpsertGpuUsageSync(ctx, params); return e })
		require.Error(t, e, "JSON NULL may not bypass a CHECK")
	}
	params.PayloadJson = gpuProjectionPayload(in, created.OperationID, 1, "digest")
	params.ResourceTenantID = uuid.NewString()
	e = r.transaction(ctx, func(tx *q.Queries) error { _, e := tx.UpsertGpuUsageSync(ctx, params); return e })
	require.Error(t, e, "JSON ref must match relational columns")
	params.ResourceTenantID = in.ResourceTenantID
	params.TenantID = int64(tid) + 100000
	e = r.transaction(ctx, func(tx *q.Queries) error { _, e := tx.UpsertGpuUsageSync(ctx, params); return e })
	require.Error(t, e, "original ref FK must retain tenant")
}

//go:build gpu_joint

package gpucontract

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	acc "github.com/zhangzhe-ctrl/ani-accelerator-service/api/gen/go/accelerator/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/ent/quotaoperation"
	"go-wind-admin/app/admin/service/internal/service"
	"go-wind-admin/pkg/localdeps/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/pkg/localdeps/kratos-bootstrap/logger"
)

func TestJointDispatchFailuresAndStop(t *testing.T) {
	ctx := context.Background()
	seed, e := readConfig[struct {
		Request *acc.GpuRequest `json:"request"`
	}]("seed.json")
	if e != nil || seed.Request == nil {
		t.Fatal(e)
	}
	startJointProcess(t, "owner", "TestJointOwnerProcess")
	startJointProcess(t, "governance-nosync", "TestJointGovernanceProcess")
	ledger, _ := jointLedger(t)
	create := func(name string) data.QuotaOccupyResult {
		var result data.QuotaOccupyResult
		if _, e := jointHTTP(ctx, "/create", map[string]any{"Key": uuid.NewString(), "Name": name, "Gpu": seed.Request}, &result); e != nil {
			t.Fatal(e)
		}
		return result
	}
	for _, mode := range []string{"operation", "resource", "accepted", "json"} {
		t.Run("invalid_ack_"+mode, func(t *testing.T) {
			if e := os.WriteFile(filepath.Join(os.Getenv("GOV_ACC_JOINT_DIR"), "owner-invalid-ack"), []byte(mode), 0600); e != nil {
				t.Fatal(e)
			}
			accepted := create("invalid-ack-" + mode)
			waitJoint(t, "invalid durable ACK is blocked and never ACKED", func() bool {
				op, e := ledger.GetOperationForUser(ctx, 1, accepted.OperationID)
				return e == nil && op.DispatchState == quotaoperation.DispatchStateUnknown && op.RetryBlocked && op.AttemptCount == 1
			})
			charges, e := ledger.GetChargesForOperation(ctx, 1, accepted.OperationID)
			if e != nil || len(charges) == 0 {
				t.Fatal(e)
			}
			for _, c := range charges {
				if c.ReleasedUnits != 0 || c.OriginalUnits <= 0 {
					t.Fatal("invalid ACK changed accounting")
				}
			}
			if e := ledger.ResumeDispatch(ctx, 1, accepted.OperationID); e != nil {
				t.Fatal(e)
			}
			waitJoint(t, "explicit repaired ACK recovery", func() bool {
				op, e := ledger.GetOperationForUser(ctx, 1, accepted.OperationID)
				return e == nil && op.DispatchState == quotaoperation.DispatchStateAcked && op.AttemptCount == 2
			})
		})
	}
	hold := filepath.Join(os.Getenv("GOV_ACC_JOINT_DIR"), "owner-hold-ack")
	_ = os.Remove(hold + ".entered")
	if e = os.WriteFile(hold, []byte("hold"), 0600); e != nil {
		t.Fatal(e)
	}
	defer os.Remove(hold)
	first := create("slow-owner-first")
	waitJoint(t, "real first owner RPC in flight", func() bool { b, e := os.ReadFile(hold + ".entered"); return e == nil && string(b) == first.OperationID })
	second := create("slow-owner-unclaimed-tail")
	// A durable second acceptance succeeds while the first RPC is held. Its
	// lease must not age behind that network call: the worker claims just one.
	for until := time.Now().Add(500 * time.Millisecond); time.Now().Before(until); time.Sleep(20 * time.Millisecond) {
		one, e := ledger.GetOperationForUser(ctx, 1, first.OperationID)
		if e != nil || one.DispatchState != quotaoperation.DispatchStateDispatching {
			t.Fatal("first RPC was not held", e)
		}
		two, e := ledger.GetOperationForUser(ctx, 1, second.OperationID)
		if e != nil || two.AttemptCount != 0 || two.DispatchState != quotaoperation.DispatchStateQueued || two.LeaseUntil != nil {
			t.Fatal("tail lease aged before its RPC", e)
		}
	}
	if e = os.Remove(hold); e != nil {
		t.Fatal(e)
	}
	for _, accepted := range []data.QuotaOccupyResult{first, second} {
		waitJoint(t, "each operation claimed only immediately before RPC", func() bool {
			op, e := ledger.GetOperationForUser(ctx, 1, accepted.OperationID)
			return e == nil && op.DispatchState == quotaoperation.DispatchStateAcked && op.AttemptCount == 1
		})
	}
	if _, e = jointHTTP(ctx, "/pause", map[string]bool{"Paused": true}, nil); e != nil {
		t.Fatal(e)
	}
	accepted := create("stop-while-postgres-blocked")
	dsn, e := secretFile("gov-dsn")
	if e != nil {
		t.Fatal(e)
	}
	db, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	tx, e := db.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, ownerSQL("HoldDispatchTableLock")); e != nil {
		t.Fatal(e)
	}
	cfg, e := readConfig[testGovConfig]("governance-config.json")
	if e != nil {
		t.Fatal(e)
	}
	ownerTLS, e := testTLS(cfg.CA, cfg.Cert, cfg.Key, "ani-inference")
	if e != nil {
		t.Fatal(e)
	}
	adapter := &ownerAdapter{client: &http.Client{Transport: &http.Transport{TLSClientConfig: ownerTLS}, Timeout: 3 * time.Second}, address: cfg.OwnerAddress}
	registry := service.NewQuotaAdapterRegistry()
	if e = registry.Register(adapter); e != nil {
		t.Fatal(e)
	}
	bctx := bootstrap.NewContextWithParam(ctx, nil, nil, bLogger.NopLogger())
	worker := service.NewQuotaDispatchWorker(bctx, ledger, registry)
	if e = worker.Start(ctx); e != nil {
		t.Fatal(e)
	}
	defer worker.Stop(ctx)
	waitJoint(t, "worker actually blocked in PostgreSQL", func() bool {
		var blocked bool
		e := db.QueryRow(ctx, ownerSQL("HasBlockedDispatchReader")).Scan(&blocked)
		return e == nil && blocked
	})
	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	started := time.Now()
	if e = worker.Stop(stopCtx); e != nil {
		t.Fatal("stop did not cancel blocked database operation", e)
	}
	if time.Since(started) >= 2*time.Second {
		t.Fatal("stop exceeded cancellation bound")
	}
	if e = tx.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	time.Sleep(500 * time.Millisecond)
	op, e := ledger.GetOperationForUser(ctx, 1, accepted.OperationID)
	if e != nil || op.AttemptCount != 0 || op.DispatchState != quotaoperation.DispatchStateQueued {
		t.Fatal("stopped worker resumed dispatch", e)
	}
	ownerDSN, e := secretFile("owner-dsn")
	if e != nil {
		t.Fatal(e)
	}
	ownerDB, e := pgxpool.New(ctx, ownerDSN)
	if e != nil {
		t.Fatal(e)
	}
	defer ownerDB.Close()
	var hash, ack string
	if e = ownerDB.QueryRow(ctx, ownerSQL("GetCommand"), "11111111-1111-4111-8111-111111111111", accepted.OperationID).Scan(&hash, &ack); e != pgx.ErrNoRows {
		t.Fatal("owner called after Stop", e)
	}
	restarted := service.NewQuotaDispatchWorker(bctx, ledger, registry)
	if e = restarted.Start(ctx); e != nil {
		t.Fatal(e)
	}
	defer restarted.Stop(ctx)
	waitJoint(t, "fresh worker recovers original durable operation", func() bool {
		op, e := ledger.GetOperationForUser(ctx, 1, accepted.OperationID)
		return e == nil && op.DispatchState == quotaoperation.DispatchStateAcked && op.AttemptCount == 1
	})
	t.Log("four malformed ACKs remained charged/blocked; PostgreSQL-blocked worker canceled and stopped without later owner call; restart recovered")
}

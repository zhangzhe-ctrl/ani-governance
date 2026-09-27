//go:build gpu_joint

package gpucontract

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	attachment "github.com/zhangzhe-ctrl/ani-accelerator-service/api/gen/go/accelerator/integration/v1"
	acc "github.com/zhangzhe-ctrl/ani-accelerator-service/api/gen/go/accelerator/v1"
	quota "go-wind-admin/api/gen/go/quota/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/ent/quotaoperation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// A bounded reproduction of the incomplete original-charge DELETE boundary.
// The complete joint suite still verifies this scenario in its wider lifecycle.
func TestJointMixedCharges(t *testing.T) {
	ctx := context.Background()
	seed, e := readConfig[struct {
		Request *acc.GpuRequest `json:"request"`
	}]("seed.json")
	if e != nil || seed.Request == nil {
		t.Fatal("joint seed missing", e)
	}
	startJointProcess(t, "owner", "TestJointOwnerProcess")
	startJointProcess(t, "governance", "TestJointGovernanceProcess")
	ledger, _ := jointLedger(t)
	cfg, e := readConfig[testGovConfig]("governance-config.json")
	if e != nil {
		t.Fatal(e)
	}
	client, closeClient, e := data.NewAcceleratorClient(cfg.Accelerator)
	if e != nil {
		t.Fatal(e)
	}
	defer closeClient()
	openDB := func(name string) *pgxpool.Pool {
		dsn, e := secretFile(name)
		if e != nil {
			t.Fatal(e)
		}
		pool, e := pgxpool.New(ctx, dsn)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(pool.Close)
		return pool
	}
	ownerCfg, e := readConfig[testOwnerConfig]("owner-config.json")
	if e != nil {
		t.Fatal(e)
	}
	ownerTLS, e := testTLS(ownerCfg.CA, ownerCfg.Cert, ownerCfg.Key, "ani-governance")
	if e != nil {
		t.Fatal(e)
	}
	conn, e := grpc.NewClient(cfg.ReleaseAddress, grpc.WithTransportCredentials(credentials.NewTLS(ownerTLS)))
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	runMixedChargeRecovery(t, ctx, ledger, openDB("gov-dsn"), openDB("owner-dsn"), client, quota.NewQuotaReleaseServiceClient(conn), seed.Request, "11111111-1111-4111-8111-111111111111")
}

func runMixedChargeRecovery(t *testing.T, ctx context.Context, ledger *data.QuotaLedgerRepo, govDB, ownerDB *pgxpool.Pool, client *data.AcceleratorClient, release quota.QuotaReleaseServiceClient, request *acc.GpuRequest, tenant string) {
	t.Helper()
	if _, e := jointHTTP(ctx, "/pause", map[string]bool{"Paused": false}, nil); e != nil {
		t.Fatal(e)
	}
	var accepted data.QuotaOccupyResult
	if _, e := jointHTTP(ctx, "/create", map[string]any{"Key": uuid.NewString(), "Name": "mixed-original-vector", "Gpu": request}, &accepted); e != nil {
		t.Fatal(e)
	}
	waitJoint(t, "mixed CREATE original full vector dispatched", func() bool {
		op, e := ledger.GetOperationForUser(ctx, 1, accepted.OperationID)
		return e == nil && op.DispatchState == quotaoperation.DispatchStateAcked
	})
	charges, e := ledger.GetChargesForOperation(ctx, 1, accepted.OperationID)
	if e != nil || len(charges) != 2 {
		t.Fatal("mixed vector missing", e)
	}
	var storage data.QuotaChargeRef
	for _, q := range charges {
		if q.QuotaCode == "storage.bytes" {
			storage = data.QuotaChargeRef{ChargeID: q.ChargeID, QuotaCode: q.QuotaCode, ChargedUnits: q.OriginalUnits}
		}
	}
	if storage.ChargeID == "" {
		t.Fatal("storage charge absent")
	}
	if _, e = jointHTTP(ctx, "/pause", map[string]bool{"Paused": true}, nil); e != nil {
		t.Fatal(e)
	}
	var deleted attachment.GpuDeleteAcceptance
	if _, e = jointHTTP(ctx, "/delete", map[string]string{"Key": uuid.NewString(), "Resource": accepted.ResourceID}, &deleted); e != nil || deleted.Result != "QUEUED_FOR_OWNER" {
		t.Fatal(deleted, e)
	}
	// An isolated fault removes exactly one original non-GPU row. Restore the
	// same record, including its original identity, even if a later assertion fails.
	var saved string
	if e = govDB.QueryRow(ctx, ownerSQL("RemoveChargeFixture"), 1, accepted.OperationID, "storage.bytes").Scan(&saved); e != nil {
		t.Fatal(e)
	}
	restored := false
	defer func() {
		if !restored {
			if _, e := govDB.Exec(context.Background(), ownerSQL("RestoreChargeFixture"), saved); e != nil {
				t.Errorf("restore original charge fixture: %v", e)
			}
		}
	}()
	if _, e = jointHTTP(ctx, "/pause", map[string]bool{"Paused": false}, nil); e != nil {
		t.Fatal(e)
	}
	waitJoint(t, "incomplete original DELETE vector stops before owner", func() bool {
		op, e := ledger.GetOperationForUser(ctx, 1, deleted.DeleteOperationId)
		return e == nil && op.DispatchState == quotaoperation.DispatchStateUnknown && op.AttemptCount > 0 && op.LastErrorCode != nil && *op.LastErrorCode == "ORIGINAL_CHARGES_INVALID"
	})
	var healthy data.QuotaOccupyResult
	if _, e = jointHTTP(ctx, "/create", map[string]any{"Key": uuid.NewString(), "Name": "healthy-after-incomplete-vector", "Gpu": request}, &healthy); e != nil {
		t.Fatal(e)
	}
	waitJoint(t, "invalid original vector does not starve healthy CREATE", func() bool {
		op, e := ledger.GetOperationForUser(ctx, 1, healthy.OperationID)
		return e == nil && op.DispatchState == quotaoperation.DispatchStateAcked
	})
	var hash, ack string
	if e = ownerDB.QueryRow(ctx, ownerSQL("GetCommand"), tenant, deleted.DeleteOperationId).Scan(&hash, &ack); e != pgx.ErrNoRows {
		t.Fatal("invalid original vector reached owner", e)
	}
	if _, e = govDB.Exec(ctx, ownerSQL("RestoreChargeFixture"), saved); e != nil {
		t.Fatal(e)
	}
	restored = true
	waitJoint(t, "restored complete DELETE vector reaches durable owner", func() bool {
		op, e := ledger.GetOperationForUser(ctx, 1, deleted.DeleteOperationId)
		return e == nil && op.DispatchState == quotaoperation.DispatchStateAcked
	})
	ref := &acc.GpuUsageRef{TenantId: tenant, OwnerService: "ani-inference", ResourceId: accepted.ResourceID, CreateOperationId: accepted.OperationID}
	waitJoint(t, "GPU ends independently of original non-GPU remainder", func() bool {
		p, e := client.Usage.GetGpuUsage(ctx, &acc.GetGpuUsageRequest{Context: &acc.TenantContext{RequestId: uuid.NewString(), TenantId: tenant, Actor: &acc.Actor{Type: "user", Id: "1"}}, Ref: ref})
		return e == nil && p.Revision == 2
	})
	charges, e = ledger.GetChargesForOperation(ctx, 1, accepted.OperationID)
	if e != nil {
		t.Fatal(e)
	}
	for _, q := range charges {
		if q.QuotaCode == "storage.bytes" && q.ReleasedUnits != 0 {
			t.Fatal("GPU callback incorrectly refunded unrelated quota")
		}
	}
	for _, total := range []int64{5, 5, 10} {
		req := &quota.ReportQuotaReleaseRequest{ReleaseEventId: uuid.NewString(), OperationId: accepted.OperationID, Reason: quota.ReleaseReason_RESOURCE_RELEASED, Items: []*quota.QuotaReleaseItem{{ChargeId: storage.ChargeID, QuotaCode: storage.QuotaCode, ReleasedTotal: total}}}
		if _, e = release.ReportQuotaRelease(ctx, req); e != nil {
			t.Fatal("non-GPU cumulative release", e)
		}
		if _, e = release.ReportQuotaRelease(ctx, req); e != nil {
			t.Fatal("receipt replay", e)
		}
	}
	if _, e = jointHTTP(ctx, "/pause", map[string]bool{"Paused": true}, nil); e != nil {
		t.Fatal(e)
	}
	t.Log("mixed full DELETE vector recovered; GPU ENDED preceded non-GPU cumulative refunds")
}

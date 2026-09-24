//go:build gpu_joint

package gpucontract

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	attachment "github.com/zhangzhe-ctrl/ani-accelerator-service/api/gen/go/accelerator/integration/v1"
	acc "github.com/zhangzhe-ctrl/ani-accelerator-service/api/gen/go/accelerator/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/ent/quotaoperation"
	"go-wind-admin/app/admin/service/internal/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A separate Acc test process shares the same restricted database and CA, but
// has zero user grants. Its private config/log survive for audit; it never seeds.
func startAccWithoutUserGrants(t *testing.T, directory string) string {
	t.Helper()
	root := os.Getenv("GOV_ACC_JOINT_DIR")
	manifest, err := readConfig[struct {
		Connection string `json:"connection_ref"`
	}]("ready.json")
	if err != nil || manifest.Connection == "" {
		t.Fatal("manifest connection_ref required", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	security, ok := config["security"].(map[string]any)
	if !ok {
		t.Fatal("Acc security config missing")
	}
	server, ok := config["server"].(map[string]any)
	if !ok {
		t.Fatal("Acc server config missing")
	}
	grpcConfig, ok := server["grpc"].(map[string]any)
	if !ok {
		t.Fatal("Acc grpc config missing")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	grpcConfig["addr"] = address
	security["grants"] = []any{}
	raw, err = json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(directory, "acc-no-grants.json")
	if err = os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(directory, "acc-no-grants.private.log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(filepath.Join(root, "acc-contract-final.test"), "-test.run=^TestContractServerProcess$", "-test.v")
	command.Env = append(os.Environ(), "ACC_CONTRACT_CHILD=1", "ACC_CONTRACT_CONFIG="+configPath, "ACC_CONTRACT_CONNECTION="+manifest.Connection, "ACC_CONTRACT_MARKER="+filepath.Join(directory, "unused-commit-marker"))
	command.Stdout, command.Stderr = log, log
	if err = command.Start(); err != nil {
		_ = log.Close()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait(); _ = log.Close() }()
	exited := false
	t.Cleanup(func() {
		if exited {
			return
		}
		_ = command.Process.Signal(syscall.SIGINT)
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("grant-revoked Acc did not exit cleanly; inspect private log %s", logPath)
			}
		case <-time.After(5 * time.Second):
			_ = command.Process.Kill()
			<-done
			t.Error("grant-revoked Acc required forced shutdown")
		}
	})
	until := time.Now().Add(10 * time.Second)
	for time.Now().Before(until) {
		connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return address
		}
		select {
		case <-done:
			exited = true
			t.Fatalf("grant-revoked Acc exited; private log %s", logPath)
		default:
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("grant-revoked Acc readiness timeout")
	return ""
}

func TestJointPolicyRevocationRecovery(t *testing.T) {
	ctx := context.Background()
	root := os.Getenv("GOV_ACC_JOINT_DIR")
	directory := filepath.Join(root, "policy-recovery-"+uuid.NewString())
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	seed, err := readConfig[struct {
		Request *acc.GpuRequest `json:"request"`
	}]("seed.json")
	if err != nil || seed.Request == nil {
		t.Fatal("joint seed missing", err)
	}
	cfg, err := readConfig[testGovConfig]("governance-config.json")
	if err != nil {
		t.Fatal(err)
	}
	direct, closeDirect, err := data.NewAcceleratorClient(cfg.Accelerator)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDirect()
	tenant := &acc.TenantContext{RequestId: uuid.NewString(), TenantId: "11111111-1111-4111-8111-111111111111", Actor: &acc.Actor{Type: "user", Id: "1"}}
	request := &acc.ResolveGpuRequestRequest{Context: tenant, Gpu: seed.Request}
	plan, err := direct.Catalog.ResolveGpuRequest(ctx, request)
	if err != nil {
		t.Fatal("original delegated Resolve must succeed", err)
	}
	item, err := service.GpuPlanQuota(plan)
	if err != nil {
		t.Fatal(err)
	}
	revokedCfg := cfg.Accelerator
	revokedCfg.Address = startAccWithoutUserGrants(t, directory)
	revoked, closeRevoked, err := data.NewAcceleratorClient(revokedCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRevoked()
	if _, err = revoked.Catalog.ResolveGpuRequest(ctx, request); status.Code(err) != codes.PermissionDenied || status.Convert(err).Message() != "DELEGATION_DENIED" {
		t.Fatal("revoked grant did not reject delegated Resolve", err)
	}
	// Only this test configuration changes; the original B Acc config is untouched.
	configPath := filepath.Join(root, "governance-config.json")
	original, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(directory, "governance-config.backup.json"), original, 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.WriteFile(configPath, original, 0600); err != nil {
			t.Error("restore private Gov config", err)
		}
	})
	pausedCfg := cfg
	pausedCfg.StartPaused = true
	raw, err := json.Marshal(pausedCfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	startJointProcess(t, "owner", "TestJointOwnerProcess")
	gov := startJointProcess(t, "governance-nosync", "TestJointGovernanceProcess")
	if err = os.WriteFile(configPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	ledger, _ := jointLedger(t)
	var accepted data.QuotaOccupyResult
	if _, err = jointHTTP(ctx, "/create", map[string]any{"Key": uuid.NewString(), "Name": "policy-recovery", "Gpu": seed.Request}, &accepted); err != nil {
		t.Fatal(err)
	}
	if _, err = jointHTTP(ctx, "/pause", map[string]bool{"Paused": false}, nil); err != nil {
		t.Fatal(err)
	}
	waitJoint(t, "original owner durable CREATE", func() bool {
		op, e := ledger.GetOperationForUser(ctx, 1, accepted.OperationID)
		return e == nil && op.DispatchState == quotaoperation.DispatchStateAcked
	})
	// Restart paused before creating DELETE; no already-running drain can race
	// the test's requirement that its acceptance is persistent but unacknowledged.
	gov.stop()
	if err = os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	startJointProcess(t, "governance-nosync", "TestJointGovernanceProcess")
	if err = os.WriteFile(configPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	var deleted attachment.GpuDeleteAcceptance
	if _, err = jointHTTP(ctx, "/delete", map[string]string{"Key": uuid.NewString(), "Resource": accepted.ResourceID}, &deleted); err != nil || deleted.Result != "QUEUED_FOR_OWNER" {
		t.Fatal("persistent owner DELETE required", err)
	}
	ref := &acc.GpuUsageRef{TenantId: tenant.TenantId, OwnerService: "ani-inference", ResourceId: accepted.ResourceID, CreateOperationId: accepted.OperationID}
	worker := service.NewGpuUsageSyncWorker(ledger, revoked, nil)
	readProjection := func(revision uint64) bool {
		_ = worker.Step(ctx)
		p, e := direct.Usage.GetGpuUsage(ctx, &acc.GetGpuUsageRequest{Context: tenant, Ref: ref})
		return e == nil && p.Revision == revision
	}
	waitJoint(t, "DECLARED sync despite revoked user grants", func() bool { return readProjection(1) })
	adminDSN, err := secretFile("gov-admin-dsn")
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fixture.Close)
	var policyBefore string
	if err = fixture.QueryRow(ctx, ownerSQL("SnapshotRecoveryPolicy"), 1, item.QuotaCode).Scan(&policyBefore); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(directory, "policy-before.private.json"), []byte(policyBefore), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		tx, e := fixture.Begin(context.Background())
		if e != nil {
			t.Error(e)
			return
		}
		defer tx.Rollback(context.Background())
		if _, e = tx.Exec(context.Background(), ownerSQL("RestoreRecoveryTenant"), 1, policyBefore); e != nil {
			t.Error(e)
			return
		}
		if _, e = tx.Exec(context.Background(), ownerSQL("RestoreRecoveryQuota"), item.QuotaCode, policyBefore); e != nil {
			t.Error(e)
			return
		}
		if e = tx.Commit(context.Background()); e != nil {
			t.Error(e)
			return
		}
		var restored string
		if e = fixture.QueryRow(context.Background(), ownerSQL("SnapshotRecoveryPolicy"), 1, item.QuotaCode).Scan(&restored); e != nil || restored != policyBefore {
			t.Error("policy fixture did not restore its original fields", e)
		}
	})
	tx, err := fixture.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, ownerSQL("RestrictRecoveryTenant"), 1); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, ownerSQL("RestrictRecoveryQuota"), 1, item.QuotaCode); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var restrictedRaw string
	if err = fixture.QueryRow(ctx, ownerSQL("SnapshotRecoveryPolicy"), 1, item.QuotaCode).Scan(&restrictedRaw); err != nil {
		t.Fatal(err)
	}
	var restricted struct {
		Status     string    `json:"status"`
		ExpiredAt  time.Time `json:"expired_at"`
		QuotaValue int64     `json:"quota_value"`
	}
	if err = json.Unmarshal([]byte(restrictedRaw), &restricted); err != nil || restricted.Status != "OFF" || !restricted.ExpiredAt.Before(time.Now()) || restricted.QuotaValue != 0 {
		t.Fatal("policy fixture did not expire/disable tenant and reduce quota to zero", err)
	}
	if code, e := jointHTTP(ctx, "/create", map[string]any{"Key": uuid.NewString(), "Name": "denied-after-policy", "Gpu": seed.Request}, nil); e == nil || code != 403 {
		t.Fatal("expired/stopped tenant accepted new GPU request", code, e)
	}
	if code, e := jointHTTP(ctx, "/create-disabled", map[string]any{"Key": uuid.NewString(), "Name": "disabled-after-policy", "Gpu": seed.Request}, nil); e == nil || code != 503 {
		t.Fatal("closed new admission accepted GPU request", code, e)
	}
	deleteOp, err := ledger.GetOperationForUser(ctx, 1, deleted.DeleteOperationId)
	if err != nil || deleteOp.DispatchState != quotaoperation.DispatchStateQueued || deleteOp.AttemptCount != 0 {
		t.Fatal("DELETE must remain unacknowledged before refund", err)
	}
	charges, err := ledger.GetChargesForOperation(ctx, 1, accepted.OperationID)
	if err != nil || len(charges) != 1 || charges[0].ReleasedUnits != 0 {
		t.Fatal("original GPU charge changed before owner close", err)
	}
	command := &service.QuotaDispatchCommand{OperationID: deleteOp.OperationID, ResourceID: deleteOp.ResourceID, ResourceTenantID: deleteOp.ResourceTenantID, Actor: service.QuotaActor{Type: deleteOp.ActorType, ID: deleteOp.ActorID}, Action: deleteOp.Action, RequestHash: deleteOp.RequestHash, CanonicalRequest: []byte(deleteOp.CanonicalRequest), CreateOperationID: accepted.OperationID}
	for _, charge := range charges {
		command.Charges = append(command.Charges, service.QuotaChargeRef{ChargeID: charge.ChargeID, QuotaCode: charge.QuotaCode, ChargedUnits: charge.OriginalUnits})
	}
	ownerTLS, err := testTLS(cfg.CA, cfg.Cert, cfg.Key, "ani-inference")
	if err != nil {
		t.Fatal(err)
	}
	ownerClient := &ownerAdapter{client: &http.Client{Transport: &http.Transport{TLSClientConfig: ownerTLS}, Timeout: 3 * time.Second}, address: cfg.OwnerAddress}
	ack, err := ownerClient.Dispatch(ctx, command)
	if err != nil {
		t.Fatal("independent owner close failed", err)
	}
	if err = service.ValidateDurableOwnerAck(command, ack); err != nil {
		t.Fatal(err)
	}
	// Do not persist this direct test delivery ACK in Gov. The actual owner
	// closes its durable record and sends ReportQuotaRelease over its own mTLS.
	waitJoint(t, "expired/reduced tenant accepts reliable owner GPU refund", func() bool {
		rows, e := ledger.GetChargesForOperation(ctx, 1, accepted.OperationID)
		return e == nil && len(rows) == 1 && rows[0].ReleasedUnits == rows[0].OriginalUnits
	})
	deleteOp, err = ledger.GetOperationForUser(ctx, 1, deleted.DeleteOperationId)
	if err != nil || deleteOp.DispatchState != quotaoperation.DispatchStateQueued || deleteOp.AttemptCount != 0 || deleteOp.AckJSON != nil {
		t.Fatal("refund was incorrectly dependent on Gov DELETE ACK", err)
	}
	waitJoint(t, "ENDED sync despite expiry/reduced quota/closed admission/revoked grants", func() bool { return readProjection(2) })
	rows, err := ledger.RecomputeInvariants(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if !row.Balanced {
			t.Fatal("policy-independent refund broke account invariant")
		}
	}
	if _, err = revoked.Catalog.ResolveGpuRequest(ctx, request); status.Code(err) != codes.PermissionDenied {
		t.Fatal("revoked grants unexpectedly restored", err)
	}
	if _, err = jointHTTP(ctx, "/pause", map[string]bool{"Paused": false}, nil); err != nil {
		t.Fatal(err)
	}
	waitJoint(t, "same persisted DELETE can ACK after policy-independent refund", func() bool {
		op, e := ledger.GetOperationForUser(ctx, 1, deleted.DeleteOperationId)
		return e == nil && op.DispatchState == quotaoperation.DispatchStateAcked
	})
	t.Log("AUTH-03/RELEASE-06: original Resolve allowed, isolated zero-grant Resolve denied; actual owner mTLS refund succeeded while tenant OFF/expired/quota zero and DELETE unacknowledged; old operation Sync reached DECLARED then ENDED with zero user grants")
}

//go:build gpu_joint

package gpucontract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	attachment "github.com/zhangzhe-ctrl/ani-accelerator-service/api/gen/go/accelerator/integration/v1"
	acc "github.com/zhangzhe-ctrl/ani-accelerator-service/api/gen/go/accelerator/v1"
	quota "go-wind-admin/api/gen/go/quota/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/ent/quotaoperation"
	"go-wind-admin/app/admin/service/internal/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

type jointProcess struct {
	cmd  *exec.Cmd
	done chan error
	log  *os.File
}

func startJointProcess(t *testing.T, mode, method string) *jointProcess {
	t.Helper()
	dir := os.Getenv("GOV_ACC_JOINT_DIR")
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	ready := filepath.Join(dir, mode+"-ready")
	_ = os.Remove(ready)
	log, e := os.Create(filepath.Join(dir, fmt.Sprintf("%s-%d.log", mode, time.Now().UnixNano())))
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(exe, "-test.run=^"+method+"$", "-test.timeout=0", "-test.v")
	cmd.Env = append(os.Environ(), "GOV_ACC_PROCESS="+mode)
	cmd.Stdout = log
	cmd.Stderr = log
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	p := &jointProcess{cmd: cmd, done: make(chan error, 1), log: log}
	go func() { p.done <- cmd.Wait() }()
	t.Cleanup(func() { p.stop() })
	until := time.Now().Add(30 * time.Second)
	for time.Now().Before(until) {
		if _, e = os.Stat(ready); e == nil {
			return p
		}
		select {
		case e = <-p.done:
			t.Fatalf("%s process failed (%v); inspect %s", mode, e, log.Name())
		default:
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s readiness timeout; inspect %s", mode, log.Name())
	return nil
}
func (p *jointProcess) stop() {
	if p == nil || p.cmd == nil {
		return
	}
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		_ = p.cmd.Process.Kill()
		select {
		case <-p.done:
		case <-time.After(time.Second):
		}
	}
	_ = p.log.Close()
	p.cmd = nil
}

func (p *jointProcess) kill(t *testing.T) {
	t.Helper()
	if e := p.cmd.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Fatal("killed child did not exit")
	}
	_ = p.log.Close()
	p.cmd = nil
}

func jointHTTP(ctx context.Context, path string, in, out any) (int, error) {
	cfg, e := readConfig[testGovConfig]("governance-config.json")
	if e != nil {
		return 0, e
	}
	return jointHTTPAt(ctx, cfg.Address, path, in, out)
}

func jointHTTPAt(ctx context.Context, address, path string, in, out any) (int, error) {
	token, e := secretFile("control-token")
	if e != nil {
		return 0, e
	}
	b, e := json.Marshal(in)
	if e != nil {
		return 0, e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+address+path, bytes.NewReader(b))
	if e != nil {
		return 0, e
	}
	req.Header.Set("X-Test-Control", token)
	res, e := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if e != nil {
		return 0, e
	}
	defer res.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if e != nil {
		return res.StatusCode, e
	}
	if res.StatusCode != 200 {
		return res.StatusCode, fmt.Errorf("test control %s: %s", path, raw)
	}
	if out != nil {
		e = json.Unmarshal(raw, out)
	}
	return res.StatusCode, e
}

func waitJoint(t *testing.T, what string, predicate func() bool) {
	t.Helper()
	until := time.Now().Add(20 * time.Second)
	for time.Now().Before(until) {
		if predicate() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out: %s", what)
}

// This suite uses independent Gov/owner OS processes and an already running
// isolated Accelerator test assembly. Its PostgreSQL roles are real restricted
// roles; no hardware or production owner conclusion follows from success.
func TestJointSoftwareContract(t *testing.T) {
	if os.Getenv("GOV_ACC_JOINT_DIR") == "" {
		t.Fatal("GOV_ACC_JOINT_DIR required")
	}
	var seed struct {
		Request *acc.GpuRequest `json:"request"`
	}
	seed, e := readConfig[struct {
		Request *acc.GpuRequest `json:"request"`
	}]("seed.json")
	if e != nil || seed.Request == nil {
		t.Fatal("joint seed request missing", e)
	}
	owner := startJointProcess(t, "owner", "TestJointOwnerProcess")
	_ = owner
	gov := startJointProcess(t, "governance", "TestJointGovernanceProcess")
	gov2 := startJointProcess(t, "governance2", "TestJointGovernanceProcess")
	ledger, _ := jointLedger(t)
	ctx := context.Background()
	cfg, e := readConfig[testGovConfig]("governance-config.json")
	if e != nil {
		t.Fatal(e)
	}
	accClient, closeAcc, e := data.NewAcceleratorClient(cfg.Accelerator)
	if e != nil {
		t.Fatal(e)
	}
	defer closeAcc()
	create := func(key, name, path string) (*data.QuotaOccupyResult, int, error) {
		var result data.QuotaOccupyResult
		code, e := jointHTTP(ctx, path, map[string]any{"Key": key, "Name": name, "Gpu": seed.Request}, &result)
		return &result, code, e
	}
	key := uuid.NewString()
	// Force an owner-committed command with a lost response; worker must retry.
	if e = os.WriteFile(filepath.Join(os.Getenv("GOV_ACC_JOINT_DIR"), "owner-drop-next-ack"), []byte("once"), 0600); e != nil {
		t.Fatal(e)
	}
	results := make(chan *data.QuotaOccupyResult, 12)
	errs := make(chan error, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			address := cfg.Address
			if index%2 == 1 {
				address = "127.0.0.1:25564"
			}
			var r data.QuotaOccupyResult
			_, e := jointHTTPAt(ctx, address, "/create", map[string]any{"Key": key, "Name": "joint-concurrent", "Gpu": seed.Request}, &r)
			results <- &r
			errs <- e
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var accepted *data.QuotaOccupyResult
	for r := range results {
		if accepted == nil {
			accepted = r
		} else if r.OperationID != accepted.OperationID || r.ResourceID != accepted.ResourceID {
			t.Fatal("concurrent acceptance diverged")
		}
	}
	charges, e := ledger.GetChargesForOperation(ctx, 1, accepted.OperationID)
	if e != nil || len(charges) != 1 {
		t.Fatal(charges, e)
	}
	if charges[0].OriginalUnits != 6144*int64(seed.Request.Replicas) {
		t.Fatal("wrong MiB charge", charges[0].OriginalUnits)
	}
	if _, code, e := create(key, "changed", "/create"); e == nil || code != 409 {
		t.Fatal("changed request accepted", code, e)
	}
	if r, _, e := create(key, "joint-concurrent", "/create-disabled"); e != nil || !r.Replayed || r.OperationID != accepted.OperationID {
		t.Fatal("disabled replay failed", r, e)
	}
	if _, code, e := create(uuid.NewString(), "new-disabled", "/create-disabled"); e == nil || code != 503 {
		t.Fatal("disabled new request accepted", code, e)
	}
	if _, code, e := create(key, "joint-concurrent", "/create-denied"); e == nil || code != 404 {
		t.Fatal("unauthorized replay accepted", code, e)
	}
	waitJoint(t, "durable owner ACK after lost response", func() bool {
		op, e := ledger.GetOperationForUser(ctx, 1, accepted.OperationID)
		return e == nil && op.DispatchState == quotaoperation.DispatchStateAcked && op.AttemptCount >= 2
	})
	ref := &acc.GpuUsageRef{TenantId: "11111111-1111-4111-8111-111111111111", OwnerService: "ani-inference", ResourceId: accepted.ResourceID, CreateOperationId: accepted.OperationID}
	readUsage := func() (*acc.GpuUsageProjection, error) {
		return accClient.Usage.GetGpuUsage(ctx, &acc.GetGpuUsageRequest{Context: &acc.TenantContext{RequestId: uuid.NewString(), TenantId: ref.TenantId, Actor: &acc.Actor{Type: "user", Id: "1"}}, Ref: ref})
	}
	waitJoint(t, "DECLARED projection", func() bool { p, e := readUsage(); return e == nil && p.Revision == 1 })
	// Test-only observation overlay: real SaveObservation and persistence, never
	// a production evidence port or hardware sample. ENDED must retain it.
	observationFile := filepath.Join(os.Getenv("GOV_ACC_JOINT_DIR"), "gov-usage-ref.json")
	observationInput, _ := json.Marshal(map[string]any{"ref": ref, "state": "ACTIVE"})
	if e = os.WriteFile(observationFile, observationInput, 0600); e != nil {
		t.Fatal(e)
	}
	observation := exec.Command(filepath.Join(os.Getenv("GOV_ACC_JOINT_DIR"), "acc-contract.test"), "-test.run=^TestJointObservationFixture$", "-test.v")
	observation.Env = append(os.Environ(), "ACC_TEST_DSN_FILE="+filepath.Join(os.Getenv("GOV_ACC_JOINT_DIR"), "acc.dsn"), "ACC_JOINT_OBSERVATION_FILE="+observationFile)
	if output, e := observation.CombinedOutput(); e != nil {
		t.Fatalf("observation overlay failed: %v %s", e, output)
	}
	// Trusted certificate alone cannot authorize a GPU refund before DELETE.
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
	release := quota.NewQuotaReleaseServiceClient(conn)
	_, e = release.ReportQuotaRelease(ctx, &quota.ReportQuotaReleaseRequest{ReleaseEventId: uuid.NewString(), OperationId: accepted.OperationID, Reason: quota.ReleaseReason_RESOURCE_RELEASED, Items: []*quota.QuotaReleaseItem{{ChargeId: charges[0].ChargeID, QuotaCode: charges[0].QuotaCode, ReleasedTotal: charges[0].OriginalUnits}}})
	if e == nil {
		t.Fatal("trusted owner refund without persistent DELETE accepted")
	}
	charges, e = ledger.GetChargesForOperation(ctx, 1, accepted.OperationID)
	if e != nil || charges[0].ReleasedUnits != 0 {
		t.Fatal("negative refund changed balance", e)
	}
	var deleted attachment.GpuDeleteAcceptance
	deleteKey := uuid.NewString()
	lostRelease := filepath.Join(os.Getenv("GOV_ACC_JOINT_DIR"), "owner-drop-release-response")
	_ = os.Remove(lostRelease + ".committed")
	if e = os.WriteFile(lostRelease, []byte("once"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = jointHTTP(ctx, "/delete", map[string]string{"Key": deleteKey, "Resource": accepted.ResourceID}, &deleted); e != nil || deleted.Result != "QUEUED_FOR_OWNER" {
		t.Fatal(deleted, e)
	}
	waitJoint(t, "owner notification committed but ACK not persisted", func() bool { _, e := os.Stat(lostRelease + ".committed"); return e == nil })
	owner.kill(t)
	owner = startJointProcess(t, "owner", "TestJointOwnerProcess")
	waitJoint(t, "full cumulative release", func() bool {
		c, e := ledger.GetChargesForOperation(ctx, 1, accepted.OperationID)
		return e == nil && len(c) == 1 && c[0].ReleasedUnits == c[0].OriginalUnits
	})
	waitJoint(t, "ENDED projection", func() bool {
		p, e := readUsage()
		return e == nil && p.Revision == 2 && p.EndReason == acc.UsageEndReason_OWNER_RESOURCE_RELEASED
	})
	bindings, e := accClient.Usage.ListBindings(ctx, &acc.ListBindingsRequest{Context: &acc.TenantContext{RequestId: uuid.NewString(), TenantId: ref.TenantId, Actor: &acc.Actor{Type: "user", Id: "1"}}, Ref: ref})
	if e != nil || len(bindings.GetItems()) == 0 {
		t.Fatal("ENDED erased live fixture binding", e)
	}
	// Owner calls ObserveRelease directly. Even after quota ENDED, the actual
	// persisted live observation must still be reported as present.
	var observed struct {
		Pod *acc.PodRef `json:"pod"`
	}
	observationResult, e := os.ReadFile(observationFile + ".result.json")
	if e != nil || json.Unmarshal(observationResult, &observed) != nil || observed.Pod == nil {
		t.Fatal("observation fixture scope missing", e)
	}
	ownerAccTLS, e := testTLS(ownerCfg.CA, ownerCfg.Cert, ownerCfg.Key, cfg.Accelerator.ServerName)
	if e != nil {
		t.Fatal(e)
	}
	ownerAccConn, e := grpc.NewClient(cfg.Accelerator.Address, grpc.WithTransportCredentials(credentials.NewTLS(ownerAccTLS)))
	if e != nil {
		t.Fatal(e)
	}
	defer ownerAccConn.Close()
	ownerAcc := acc.NewAcceleratorUsageServiceClient(ownerAccConn)
	ownerObserveContext := metadata.NewOutgoingContext(ctx, metadata.Pairs("x-ani-tenant-id", ref.TenantId))
	observeRequest := &acc.ObserveReleaseRequest{RequestId: uuid.NewString(), Ref: ref, DeleteOperationId: deleted.DeleteOperationId}
	if _, e = ownerAcc.ObserveRelease(ownerObserveContext, observeRequest); e == nil {
		t.Fatal("empty owner release scope accepted")
	}
	observeRequest.Pods = []*acc.PodRef{observed.Pod}
	finding, e := ownerAcc.ObserveRelease(ownerObserveContext, observeRequest)
	if e != nil || finding.Finding != acc.ReleaseFinding_ALLOCATION_STILL_PRESENT {
		t.Fatal("live scope was reported released", finding, e)
	}
	var replay attachment.GpuDeleteAcceptance
	if _, e = jointHTTP(ctx, "/delete", map[string]string{"Key": deleteKey, "Resource": accepted.ResourceID}, &replay); e != nil || replay.DeleteOperationId != deleted.DeleteOperationId {
		t.Fatal("DELETE replay differs", e)
	}
	// Restart governance after completed and incomplete records; old idempotency
	// and derived projection must remain valid with no in-memory authority.
	gov2.stop()
	gov.stop()
	// Remove only a derived fixture row while all projection writers are stopped.
	// Restart must reconstruct byte-identical revision 2 from authoritative data.
	govDSN, e := secretFile("gov-dsn")
	if e != nil {
		t.Fatal(e)
	}
	govDB, e := pgxpool.New(ctx, govDSN)
	if e != nil {
		t.Fatal(e)
	}
	defer govDB.Close()
	var originalPayload, originalHash string
	var acked int64
	if e = govDB.QueryRow(ctx, ownerSQL("GetSyncSnapshot"), 1, accepted.OperationID).Scan(&originalPayload, &originalHash, &acked); e != nil {
		t.Fatal(e)
	}
	if _, e = govDB.Exec(ctx, ownerSQL("DeleteSyncFixture"), 1, accepted.OperationID); e != nil {
		t.Fatal(e)
	}
	gov = startJointProcess(t, "governance", "TestJointGovernanceProcess")
	waitJoint(t, "lost sync row reconstructed", func() bool {
		var payload, hash string
		var revision int64
		e := govDB.QueryRow(ctx, ownerSQL("GetSyncSnapshot"), 1, accepted.OperationID).Scan(&payload, &hash, &revision)
		return e == nil && payload == originalPayload && hash == originalHash && revision == 2
	})
	if r, _, e := create(key, "joint-concurrent", "/create-disabled"); e != nil || r.OperationID != accepted.OperationID {
		t.Fatal("post-restart replay", e)
	}
	if _, e = jointHTTP(ctx, "/pause", map[string]bool{"Paused": true}, nil); e != nil {
		t.Fatal(e)
	}
	unsent, _, e := create(uuid.NewString(), "unsent", "/create")
	if e != nil {
		t.Fatal(e)
	}
	var canceled attachment.GpuDeleteAcceptance
	if _, e = jointHTTP(ctx, "/delete", map[string]string{"Key": uuid.NewString(), "Resource": unsent.ResourceID}, &canceled); e != nil || canceled.Result != "CANCELED_BEFORE_DISPATCH" || canceled.DispatchOperationId != "" {
		t.Fatal(canceled, e)
	}
	var again attachment.GpuDeleteAcceptance
	if _, e = jointHTTP(ctx, "/delete", map[string]string{"Key": uuid.NewString(), "Resource": unsent.ResourceID}, &again); e != nil || again.Result != "CANCELED_BEFORE_DISPATCH" {
		t.Fatal(again, e)
	}
	if _, e = jointHTTP(ctx, "/pause", map[string]bool{"Paused": false}, nil); e != nil {
		t.Fatal(e)
	}
	gov.stop()
	// Kill the real governance process after acceptance commit with its separate
	// sync worker deliberately not started in this test assembly.
	gov = startJointProcess(t, "governance-nosync", "TestJointGovernanceProcess")
	if _, e = jointHTTP(ctx, "/pause", map[string]bool{"Paused": true}, nil); e != nil {
		t.Fatal(e)
	}
	gap, _, e := create(uuid.NewString(), "committed-before-sync", "/create")
	if e != nil {
		t.Fatal(e)
	}
	if e = govDB.QueryRow(ctx, ownerSQL("GetSyncSnapshot"), 1, gap.OperationID).Scan(&originalPayload, &originalHash, &acked); e == nil {
		t.Fatal("gap fixture already synchronized")
	}
	gov.kill(t)
	gov = startJointProcess(t, "governance", "TestJointGovernanceProcess")
	waitJoint(t, "post-kill missing projection recovered", func() bool {
		var payload, hash string
		var revision int64
		return govDB.QueryRow(ctx, ownerSQL("GetSyncSnapshot"), 1, gap.OperationID).Scan(&payload, &hash, &revision) == nil && revision == 1
	})
	var gapDelete attachment.GpuDeleteAcceptance
	if _, e = jointHTTP(ctx, "/delete", map[string]string{"Key": uuid.NewString(), "Resource": gap.ResourceID}, &gapDelete); e != nil {
		t.Fatal(e)
	}
	waitJoint(t, "old operation later released is rescanned", func() bool {
		var payload, hash string
		var revision int64
		return govDB.QueryRow(ctx, ownerSQL("GetSyncSnapshot"), 1, gap.OperationID).Scan(&payload, &hash, &revision) == nil && revision == 2
	})
	ownerDSN, e := secretFile("owner-dsn")
	if e != nil {
		t.Fatal(e)
	}
	ownerDB, e := pgxpool.New(ctx, ownerDSN)
	if e != nil {
		t.Fatal(e)
	}
	defer ownerDB.Close()
	waitJoint(t, "reliable notification acknowledged after owner restart", func() bool {
		var acknowledged bool
		var attempts int64
		var payload string
		e := ownerDB.QueryRow(ctx, ownerSQL("GetNotification"), ref.TenantId, accepted.OperationID).Scan(&acknowledged, &attempts, &payload)
		return e == nil && acknowledged && attempts >= 1
	})
	var closed, executed bool
	if e = ownerDB.QueryRow(ctx, ownerSQL("GetResource"), ref.TenantId, accepted.ResourceID).Scan(&closed, &executed); e != nil || !closed || !executed {
		t.Fatal("owner durable close missing", closed, executed, e)
	}
	if e = ownerDB.QueryRow(ctx, ownerSQL("GetResource"), ref.TenantId, unsent.ResourceID).Scan(&closed, &executed); e == nil {
		t.Fatal("unsent canceled create reached owner")
	}
	// Hold a real claimed CREATE in flight, persist DELETE, and deliver DELETE
	// first to the independent owner. Its tombstone must defeat late CREATE.
	if _, e = jointHTTP(ctx, "/pause", map[string]bool{"Paused": true}, nil); e != nil {
		t.Fatal(e)
	}
	late, _, e := create(uuid.NewString(), "delete-before-create", "/create")
	if e != nil {
		t.Fatal(e)
	}
	claimed, e := ledger.ClaimDispatchable(ctx, "test-delayed-original", time.Minute, 100)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, claim := range claimed {
		if claim.OperationID == late.OperationID {
			found = true
		}
	}
	if !found {
		t.Fatal("original CREATE was not claimed")
	}
	var earlyDelete attachment.GpuDeleteAcceptance
	if _, e = jointHTTP(ctx, "/delete", map[string]string{"Key": uuid.NewString(), "Resource": late.ResourceID}, &earlyDelete); e != nil || earlyDelete.Result != "QUEUED_FOR_OWNER" {
		t.Fatal("attempted create was locally canceled", e)
	}
	original, e := ledger.GetOperationForUser(ctx, 1, late.OperationID)
	if e != nil {
		t.Fatal(e)
	}
	lateCharges, e := ledger.GetChargesForOperation(ctx, 1, late.OperationID)
	if e != nil {
		t.Fatal(e)
	}
	createCommand := &service.QuotaDispatchCommand{OperationID: original.OperationID, ResourceID: original.ResourceID, ResourceTenantID: original.ResourceTenantID, Actor: service.QuotaActor{Type: original.ActorType, ID: original.ActorID}, Action: original.Action, RequestHash: original.RequestHash, CanonicalRequest: []byte(original.CanonicalRequest)}
	for _, q := range lateCharges {
		createCommand.Charges = append(createCommand.Charges, service.QuotaChargeRef{ChargeID: q.ChargeID, QuotaCode: q.QuotaCode, ChargedUnits: q.OriginalUnits})
	}
	deleteOperation, e := ledger.GetOperationForUser(ctx, 1, earlyDelete.DeleteOperationId)
	if e != nil {
		t.Fatal(e)
	}
	deleteCommand := *createCommand
	deleteCommand.OperationID = deleteOperation.OperationID
	deleteCommand.Action = deleteOperation.Action
	deleteCommand.RequestHash = deleteOperation.RequestHash
	deleteCommand.CreateOperationID = original.OperationID
	govOwnerTLS, e := testTLS(cfg.CA, cfg.Cert, cfg.Key, "ani-inference")
	if e != nil {
		t.Fatal(e)
	}
	directOwner := &ownerAdapter{client: &http.Client{Transport: &http.Transport{TLSClientConfig: govOwnerTLS}, Timeout: 3 * time.Second}, address: cfg.OwnerAddress}
	for _, command := range []*service.QuotaDispatchCommand{&deleteCommand, createCommand} {
		ack, e := directOwner.Dispatch(ctx, command)
		if e != nil || service.ValidateDurableOwnerAck(command, ack) != nil {
			t.Fatal("out-of-order owner delivery", e)
		}
	}
	if e = ownerDB.QueryRow(ctx, ownerSQL("GetResource"), ref.TenantId, late.ResourceID).Scan(&closed, &executed); e != nil || !closed || executed {
		t.Fatal("late create crossed tombstone", closed, executed, e)
	}
	waitJoint(t, "never executed closed owner reliably refunds", func() bool {
		c, e := ledger.GetChargesForOperation(ctx, 1, late.OperationID)
		return e == nil && len(c) == 1 && c[0].ReleasedUnits == c[0].OriginalUnits
	})
	runMixedChargeRecovery(t, ctx, ledger, govDB, ownerDB, accClient, release, seed.Request, ref.TenantId)
	runCapacityAdmissionCases(t, ctx, accClient, seed.Request, ref, observed.Pod)
	invariants, e := ledger.RecomputeInvariants(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	for _, i := range invariants {
		if !i.Balanced {
			t.Fatal("ledger imbalance", i)
		}
	}
	// Save only references/IDs, no DSN/cert private material.
	evidence, _ := json.MarshalIndent(map[string]any{"hardware_observed": false, "scope": "independent persistent test owner; production business code", "create_operation": accepted.OperationID, "resource": accepted.ResourceID, "delete_operation": deleted.DeleteOperationId, "canceled_operation": unsent.OperationID}, "", "  ")
	if e = os.WriteFile(filepath.Join(os.Getenv("GOV_ACC_JOINT_DIR"), "joint-contract-result.json"), evidence, 0600); e != nil {
		t.Fatal(e)
	}
}

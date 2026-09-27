//go:build gpu_joint

package gpucontract

import (
	"context"
	"crypto/tls"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	attachment "github.com/zhangzhe-ctrl/ani-accelerator-service/api/gen/go/accelerator/integration/v1"
	acc "github.com/zhangzhe-ctrl/ani-accelerator-service/api/gen/go/accelerator/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/ent/quotaoperation"
	"go-wind-admin/app/admin/service/internal/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// A test-only mTLS fault proxy produces real transport deadline/status errors.
// Successful calls forward to the running Accelerator, never an in-memory ACK.
type syncFaultProxy struct {
	acc.UnimplementedAcceleratorUsageServiceServer
	client            *data.AcceleratorClient
	mu                sync.Mutex
	target, permanent string
	attempts          int
	first             *acc.GpuUsageProjection
	changed           bool
}

func (p *syncFaultProxy) SyncGpuUsage(ctx context.Context, req *acc.SyncGpuUsageRequest) (*acc.SyncGpuUsageResponse, error) {
	p.mu.Lock()
	id := req.GetProjection().GetRef().GetCreateOperationId()
	attempt := 0
	if id == p.target {
		p.attempts++
		attempt = p.attempts
		if p.first == nil {
			p.first = proto.Clone(req.Projection).(*acc.GpuUsageProjection)
		} else if !proto.Equal(p.first, req.Projection) {
			p.changed = true
		}
	}
	permanent := id == p.permanent && req.Projection.Revision == 1
	p.mu.Unlock()
	if permanent {
		return nil, status.Error(codes.FailedPrecondition, "controlled sync contract rejection")
	}
	if attempt == 1 {
		return nil, status.Error(codes.Unavailable, "controlled downstream interruption")
	}
	if attempt == 2 {
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	return p.client.SyncGpuUsage(ctx, req)
}

func TestJointSyncFailureRecovery(t *testing.T) {
	ctx := context.Background()
	seed, e := readConfig[struct {
		Request *acc.GpuRequest `json:"request"`
	}]("seed.json")
	if e != nil || seed.Request == nil {
		t.Fatal("joint seed missing", e)
	}
	startJointProcess(t, "owner", "TestJointOwnerProcess")
	startJointProcess(t, "governance-nosync", "TestJointGovernanceProcess")
	ledger, _ := jointLedger(t)
	cfg, e := readConfig[testGovConfig]("governance-config.json")
	if e != nil {
		t.Fatal(e)
	}
	direct, closeDirect, e := data.NewAcceleratorClient(cfg.Accelerator)
	if e != nil {
		t.Fatal(e)
	}
	defer closeDirect()
	dsn, e := secretFile("gov-dsn")
	if e != nil {
		t.Fatal(e)
	}
	db, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	serverTLS, e := testTLS(cfg.CA, cfg.Cert, cfg.Key, "")
	if e != nil {
		t.Fatal(e)
	}
	serverTLS.ClientAuth = tls.RequireAndVerifyClientCert
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	proxy := &syncFaultProxy{client: direct}
	rpc := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	acc.RegisterAcceleratorUsageServiceServer(rpc, proxy)
	go rpc.Serve(listener)
	defer rpc.Stop()
	proxyCfg := cfg.Accelerator
	proxyCfg.Address, proxyCfg.ServerName, proxyCfg.Timeout = listener.Addr().String(), "ani-governance", 10*time.Second
	client, closeClient, e := data.NewAcceleratorClient(proxyCfg)
	if e != nil {
		t.Fatal(e)
	}
	defer closeClient()
	worker := service.NewGpuUsageSyncWorker(ledger, client, nil)
	create := func(name string) data.QuotaOccupyResult {
		var accepted data.QuotaOccupyResult
		if _, e := jointHTTP(ctx, "/create", map[string]any{"Key": uuid.NewString(), "Name": name, "Gpu": seed.Request}, &accepted); e != nil {
			t.Fatal(e)
		}
		waitJoint(t, "owner dispatch independent of sync", func() bool {
			op, e := ledger.GetOperationForUser(ctx, 1, accepted.OperationID)
			return e == nil && op.DispatchState == quotaoperation.DispatchStateAcked
		})
		return accepted
	}
	target := create("sync-transport-recovery")
	proxy.mu.Lock()
	proxy.target = target.OperationID
	proxy.mu.Unlock()
	before, e := ledger.GetOperationForUser(ctx, 1, target.OperationID)
	if e != nil {
		t.Fatal(e)
	}
	beforeCharges, e := ledger.GetChargesForOperation(ctx, 1, target.OperationID)
	if e != nil {
		t.Fatal(e)
	}
	var raw, hash, reason string
	var acked int64
	var attempts int
	var blocked bool
	read := func(id string) bool {
		return db.QueryRow(ctx, ownerSQL("GetSyncFailureState"), 1, id).Scan(&raw, &hash, &acked, &attempts, &blocked, &reason) == nil
	}
	waitJoint(t, "real mTLS unavailable retained", func() bool {
		_ = worker.Step(ctx)
		return read(target.OperationID) && attempts == 1 && reason == "Unavailable" && acked == 0 && !blocked
	})
	originalRaw, originalHash := raw, hash
	started := time.Now()
	waitJoint(t, "real RPC deadline retained", func() bool {
		_ = worker.Step(ctx)
		return read(target.OperationID) && attempts == 2 && reason == "DeadlineExceeded" && acked == 0 && !blocked
	})
	if time.Since(started) < 3*time.Second {
		t.Fatal("worker deadline did not elapse")
	}
	if raw != originalRaw || hash != originalHash {
		t.Fatal("retry changed persisted projection")
	}
	waitJoint(t, "sync retry reaches real Accelerator", func() bool { _ = worker.Step(ctx); return read(target.OperationID) && acked == 1 && attempts >= 3 })
	proxy.mu.Lock()
	changed := proxy.changed
	proxy.mu.Unlock()
	if changed {
		t.Fatal("retry changed wire projection")
	}
	after, e := ledger.GetOperationForUser(ctx, 1, target.OperationID)
	if e != nil || after.AttemptCount != before.AttemptCount {
		t.Fatal("sync changed business attempts", e)
	}
	afterCharges, e := ledger.GetChargesForOperation(ctx, 1, target.OperationID)
	if e != nil || len(beforeCharges) != len(afterCharges) {
		t.Fatal(e)
	}
	for i, c := range beforeCharges {
		if c.ChargeID != afterCharges[i].ChargeID || c.OriginalUnits != afterCharges[i].OriginalUnits || c.ReleasedUnits != afterCharges[i].ReleasedUnits {
			t.Fatal("sync failure changed accounting")
		}
	}
	// Seed an individually valid conflicting receiver fact. The production
	// Accelerator must reject the original immutable time, not the proxy.
	conflict := create("sync-real-receiver-conflict")
	original, e := ledger.GetOperationForUser(ctx, 1, conflict.OperationID)
	if e != nil {
		t.Fatal(e)
	}
	canonical, e := service.DecodeGpuCanonical([]byte(original.CanonicalRequest))
	if e != nil {
		t.Fatal(e)
	}
	seeded := &acc.GpuUsageProjection{
		Ref:      &acc.GpuUsageRef{TenantId: original.ResourceTenantID, OwnerService: original.OwnerService, ResourceId: original.ResourceID, CreateOperationId: original.OperationID},
		Revision: 1, State: acc.UsageState_DECLARED, Plan: canonical.GpuPlan,
		SourceOperationCreatedAt: original.CreatedAt.Add(time.Nanosecond).UTC().Format(time.RFC3339Nano),
		SourceFactRef:            "quota-operation:" + original.OperationID + ":accepted",
	}
	seeded.PayloadDigest, e = service.GpuProjectionDigest(seeded)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = direct.SyncGpuUsage(ctx, &acc.SyncGpuUsageRequest{RequestId: uuid.NewString(), Projection: seeded}); e != nil {
		t.Fatal("explicit receiver conflict fixture", e)
	}
	waitJoint(t, "real receiver immutable conflict blocks same payload", func() bool {
		_ = worker.Step(ctx)
		return read(conflict.OperationID) && blocked && acked == 0 && reason == "USAGE_PROJECTION_CONFLICT"
	})
	permanent := create("sync-permanent-rejection")
	proxy.mu.Lock()
	proxy.permanent = permanent.OperationID
	proxy.mu.Unlock()
	waitJoint(t, "permanent sync contract error persists", func() bool {
		_ = worker.Step(ctx)
		return read(permanent.OperationID) && blocked && acked == 0 && reason == "FailedPrecondition"
	})
	blockedRaw, blockedHash := raw, hash
	healthy := create("sync-after-blocked-record")
	waitJoint(t, "blocked projection does not stop other sync or dispatch", func() bool { _ = worker.Step(ctx); return read(healthy.OperationID) && acked == 1 })
	if !read(permanent.OperationID) || raw != blockedRaw || hash != blockedHash || !blocked {
		t.Fatal("permanent error lost original evidence")
	}
	for _, accepted := range []data.QuotaOccupyResult{target, permanent, healthy} {
		var deleted attachment.GpuDeleteAcceptance
		if _, e := jointHTTP(ctx, "/delete", map[string]string{"Key": uuid.NewString(), "Resource": accepted.ResourceID}, &deleted); e != nil {
			t.Fatal(e)
		}
		waitJoint(t, "refund and later revision recover independently", func() bool { _ = worker.Step(ctx); return read(accepted.OperationID) && acked == 2 && !blocked })
	}
	var conflictDelete attachment.GpuDeleteAcceptance
	if _, e = jointHTTP(ctx, "/delete", map[string]string{"Key": uuid.NewString(), "Resource": conflict.ResourceID}, &conflictDelete); e != nil {
		t.Fatal(e)
	}
	waitJoint(t, "receiver conflict does not block authoritative refund", func() bool {
		_ = worker.Step(ctx)
		charges, e := ledger.GetChargesForOperation(ctx, 1, conflict.OperationID)
		if e != nil || len(charges) == 0 {
			return false
		}
		for _, c := range charges {
			if c.ReleasedUnits != c.OriginalUnits {
				return false
			}
		}
		return read(conflict.OperationID) && blocked && acked == 0 && reason == "USAGE_PROJECTION_CONFLICT"
	})
	t.Log("real mTLS unavailable/deadline/receiver conflict retained; immutable retries converged; independent dispatch/refund and later revision recovered")
}

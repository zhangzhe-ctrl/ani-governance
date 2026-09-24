//go:build gpu_joint

package gpucontract

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	attachment "github.com/zhangzhe-ctrl/ani-accelerator-service/api/gen/go/accelerator/integration/v1"
	acc "github.com/zhangzhe-ctrl/ani-accelerator-service/api/gen/go/accelerator/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type snapshotBarrier struct {
	mu    sync.Mutex
	plans [2]*acc.ResolvedGpuPlan
	calls int
	ready chan struct{}
}

type snapshotResolveProxy struct {
	acc.UnimplementedAcceleratorCatalogServiceServer
	client  *data.AcceleratorClient
	barrier *snapshotBarrier
	index   int
}

// Only the ordering of an explicitly unordered managed KeyValue set changes.
// Both results come from a real delegated Resolve and retain its semantic digest.
func (p *snapshotResolveProxy) ResolveGpuRequest(ctx context.Context, request *acc.ResolveGpuRequestRequest) (*acc.ResolvedGpuPlan, error) {
	remote, ok := peer.FromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing peer")
	}
	identity, ok := remote.AuthInfo.(credentials.TLSInfo)
	if !ok || len(identity.State.VerifiedChains) == 0 {
		return nil, status.Error(codes.Unauthenticated, "missing verified peer")
	}
	leaf := identity.State.VerifiedChains[0][0]
	if len(leaf.URIs) != 1 || leaf.URIs[0].String() != data.GovernanceAcceleratorURI {
		return nil, status.Error(codes.PermissionDenied, "wrong service")
	}
	delegation := request.GetContext()
	md, _ := metadata.FromIncomingContext(ctx)
	if delegation == nil || delegation.Actor == nil {
		return nil, status.Error(codes.PermissionDenied, "missing delegation")
	}
	for name, value := range map[string]string{"x-ani-action": "ResolveGpuRequest", "x-ani-actor-type": delegation.Actor.Type, "x-ani-actor-id": delegation.Actor.Id, "x-ani-tenant-id": delegation.TenantId} {
		values := md.Get(name)
		if len(values) != 1 || values[0] != value {
			return nil, status.Error(codes.PermissionDenied, "delegation mismatch")
		}
	}
	plan, err := p.client.Catalog.ResolveGpuRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	plan = proto.Clone(plan).(*acc.ResolvedGpuPlan)
	if len(plan.GetRuntime().GetNodeLabels()) < 2 {
		return nil, status.Error(codes.FailedPrecondition, "multiple managed labels required")
	}
	if p.index == 1 {
		slices.Reverse(plan.Runtime.NodeLabels)
	}
	if _, err = service.GpuPlanQuota(plan); err != nil {
		return nil, err
	}
	p.barrier.mu.Lock()
	p.barrier.calls++
	if p.barrier.plans[p.index] != nil {
		p.barrier.mu.Unlock()
		return nil, status.Error(codes.FailedPrecondition, "unexpected repeat Resolve")
	}
	p.barrier.plans[p.index] = proto.Clone(plan).(*acc.ResolvedGpuPlan)
	if p.barrier.plans[0] != nil && p.barrier.plans[1] != nil {
		close(p.barrier.ready)
	}
	p.barrier.mu.Unlock()
	select {
	case <-p.barrier.ready:
		return plan, nil
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}
}

func TestJointResolvedSnapshotRace(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
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
	serverTLS, err := testTLS(cfg.CA, cfg.Cert, cfg.Key, "")
	if err != nil {
		t.Fatal(err)
	}
	serverTLS.ClientAuth = tls.RequireAndVerifyClientCert
	barrier := &snapshotBarrier{ready: make(chan struct{})}
	var proxyAddresses [2]string
	for i := range proxyAddresses {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		rpc := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS.Clone())))
		acc.RegisterAcceleratorCatalogServiceServer(rpc, &snapshotResolveProxy{client: direct, barrier: barrier, index: i})
		go rpc.Serve(listener)
		t.Cleanup(rpc.Stop)
		proxyAddresses[i] = listener.Addr().String()
	}
	configPath := filepath.Join(os.Getenv("GOV_ACC_JOINT_DIR"), "governance-config.json")
	originalConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	backupPath := configPath + ".snapshot-race-" + uuid.NewString() + ".backup"
	if err = os.WriteFile(backupPath, originalConfig, 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.WriteFile(configPath, originalConfig, 0600); err != nil {
			t.Errorf("restore private config: %v", err)
			return
		}
		if err := os.Remove(backupPath); err != nil {
			t.Error(err)
		}
	})
	addresses := []string{cfg.Address, "127.0.0.1:25564"}
	for i, mode := range []string{"governance-nosync", "governance-nosync2"} {
		proxyConfig := cfg
		proxyConfig.StartPaused = true
		proxyConfig.Accelerator.Address = proxyAddresses[i]
		proxyConfig.Accelerator.ServerName = "ani-governance"
		proxyConfig.Accelerator.Timeout = 10 * time.Second
		raw, err := json.Marshal(proxyConfig)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(configPath, raw, 0600); err != nil {
			t.Fatal(err)
		}
		startJointProcess(t, mode, "TestJointGovernanceProcess")
		if _, err = jointHTTPAt(ctx, addresses[i], "/pause", map[string]bool{"Paused": true}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(configPath, originalConfig, 0600); err != nil {
		t.Fatal(err)
	}
	ledger, _ := jointLedger(t)
	balances := func() map[string]int64 {
		t.Helper()
		rows, err := ledger.RecomputeInvariants(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]int64{service.GpuSharedQuotaCode: 0, service.GpuPhysicalQuotaCode: 0, "storage.bytes": 0}
		for _, row := range rows {
			if !row.Balanced {
				t.Fatal("ledger invariant violated")
			}
			out[row.QuotaCode] = row.OccupiedUnits
		}
		return out
	}
	before := balances()
	input := map[string]any{"Key": uuid.NewString(), "Name": "mixed-snapshot-race", "Gpu": seed.Request}
	var results [2]data.QuotaOccupyResult
	var errors [2]error
	var wg sync.WaitGroup
	for i := range addresses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errors[i] = jointHTTPAt(ctx, addresses[i], "/create", input, &results[i])
		}(i)
	}
	wg.Wait()
	for _, err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if results[0].OperationID != results[1].OperationID || results[0].ResourceID != results[1].ResourceID || !reflect.DeepEqual(results[0].ChargeIDs, results[1].ChargeIDs) || results[0].Replayed == results[1].Replayed {
		t.Fatal("two processes did not elect one original acceptance")
	}
	winner := 0
	if results[0].Replayed {
		winner = 1
	}
	op, err := ledger.GetOperationForUser(ctx, 1, results[0].OperationID)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := service.DecodeGpuCanonical([]byte(op.CanonicalRequest))
	if err != nil {
		t.Fatal(err)
	}
	barrier.mu.Lock()
	plans, calls := barrier.plans, barrier.calls
	barrier.mu.Unlock()
	if calls != 2 || plans[0] == nil || plans[1] == nil || proto.Equal(plans[0], plans[1]) || plans[0].ResolutionDigest != plans[1].ResolutionDigest {
		t.Fatal("expected two distinct legal byte representations with identical semantic digest")
	}
	canonicalBytes := func(plan *acc.ResolvedGpuPlan) string {
		t.Helper()
		copy := *canonical
		copy.GpuPlan = plan
		var out bytes.Buffer
		encoder := json.NewEncoder(&out)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(copy); err != nil {
			t.Fatal(err)
		}
		return string(bytes.TrimSuffix(out.Bytes(), []byte{'\n'}))
	}
	if op.CanonicalRequest != canonicalBytes(plans[winner]) || op.CanonicalRequest == canonicalBytes(plans[1-winner]) {
		t.Fatal("persisted canonical bytes are not the winning Resolve snapshot")
	}
	charges, err := ledger.GetChargesForOperation(ctx, 1, op.OperationID)
	if err != nil || len(charges) != 2 {
		t.Fatal("original full charge vector missing", err)
	}
	quota, err := service.GpuPlanQuota(plans[winner])
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]int64{quota.QuotaCode: quota.Units, "storage.bytes": 10}
	after := balances()
	for _, charge := range charges {
		if charge.OriginalUnits != expected[charge.QuotaCode] || charge.ReleasedUnits != 0 {
			t.Fatal("charge vector differs from the one winning request")
		}
	}
	for code, units := range expected {
		if after[code]-before[code] != units {
			t.Fatalf("%s was not charged exactly once", code)
		}
	}
	for _, address := range addresses {
		var replay data.QuotaOccupyResult
		if _, err = jointHTTPAt(ctx, address, "/create", input, &replay); err != nil || !replay.Replayed || replay.OperationID != op.OperationID {
			t.Fatal("historical replay changed acceptance", err)
		}
	}
	again, err := ledger.GetOperationForUser(ctx, 1, op.OperationID)
	if err != nil || again.CanonicalRequest != op.CanonicalRequest || !reflect.DeepEqual(after, balances()) {
		t.Fatal("loser or replay overwrote canonical/balance", err)
	}
	barrier.mu.Lock()
	calls = barrier.calls
	barrier.mu.Unlock()
	if calls != 2 {
		t.Fatal("replay called Resolve again")
	}
	var canceled attachment.GpuDeleteAcceptance
	if _, err = jointHTTPAt(ctx, addresses[0], "/delete", map[string]string{"Key": uuid.NewString(), "Resource": op.ResourceID}, &canceled); err != nil || canceled.Result != "CANCELED_BEFORE_DISPATCH" {
		t.Fatal("paused original was not locally canceled", err)
	}
	if !reflect.DeepEqual(before, balances()) {
		t.Fatal("cleanup did not restore original balances")
	}
	t.Logf("CREATE-03 two real delegated Resolve responses, identical semantic digest=%s; winner process=%d canonical_sha256=%x; loser and replays preserved original full vector and single charge", plans[0].ResolutionDigest, winner+1, sha256.Sum256([]byte(op.CanonicalRequest)))
}

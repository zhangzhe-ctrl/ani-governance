//go:build quota_lab

package simulator

// QUOTA-GPU-LOCAL-01 模拟器全链集成测试（真实 PostgreSQL + 真实 mTLS gRPC）。
// 覆盖 FAIL-03/04/05/06/08/09/13(辅助)/14/16(辅助)/17、CON-06、AUTH-05。
//
// 环境要求（缺少必须 Fatal，不得 Skip）：
//   QUOTA_LAB_PG_DSN      已完成迁移的 governance 库 DSN（账本侧）
//   QUOTA_LAB_PG_ADMIN_DSN 仅用于显式创建/迁移/销毁独立模拟器库和角色
//   QUOTA_LAB_CERTS_DIR   任务证书目录（ca.pem、ani-governance.pem/key、
//                         ani-gpu-simulator.pem/key、other-service.pem/key）

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	_ "embed"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	entsql "entgo.io/ent/dialect/sql"
	entCrud "github.com/tx7do/go-crud/entgo"

	quotapb "go-wind-admin/api/gen/go/quota/service/v1"
	"google.golang.org/grpc/codes"

	quotalabpb "go-wind-admin/api/gen/go/quota_lab/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/server"
	appViewer "go-wind-admin/pkg/entgo/viewer"
)

//go:embed testdata/integration-queries.sql
var integrationQueries string

// Test-only named statements; fixture identifiers are quoted before substitution.
func fixtureSQL(name string) string {
	for _, section := range strings.Split(integrationQueries, "-- name: ") {
		label, query, ok := strings.Cut(section, "\n")
		if ok && label == name {
			return strings.TrimSpace(query)
		}
	}
	panic("missing simulator test query: " + name)
}

type simStack struct {
	t                *testing.T
	owner            *OwnerStore
	provider         *ProviderStore
	sim              *Simulator
	grpcAddr         string
	grpcStop         func()
	ledger           *data.QuotaLedgerRepo
	govAddr          string
	govStop          func()
	govClient        quotapb.QuotaReleaseServiceClient
	notifier         *Notifier
	tenantID         uint32
	resourceTenantID string
	currentCharge    string
	certsDir         string
	ownerDSN         string
	providerDS       string
}

func mustDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("QUOTA_LAB_PG_DSN")
	if dsn == "" {
		t.Fatal("QUOTA_LAB_PG_DSN is required (missing DSN must fail, not skip)")
	}
	return dsn
}

func mustCertsDir(t *testing.T) string {
	t.Helper()
	d := os.Getenv("QUOTA_LAB_CERTS_DIR")
	if d == "" {
		t.Fatal("QUOTA_LAB_CERTS_DIR is required")
	}
	return d
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	p := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return p
}

// Each simulator database is migrated explicitly with a separate admin identity.
// Runtime stores are non-owner roles with DML only; startup performs no DDL.
func createScratchDB(t *testing.T, kind string) string {
	t.Helper()
	adminDSN := os.Getenv("QUOTA_LAB_PG_ADMIN_DSN")
	require.NotEmpty(t, adminDSN, "QUOTA_LAB_PG_ADMIN_DSN required for explicit isolated fixture migrations")
	adminURL, err := url.Parse(adminDSN)
	require.NoError(t, err)
	require.Contains(t, []string{"postgres", "postgresql"}, adminURL.Scheme)
	adminURL.Path = "/postgres"
	admin, err := sql.Open("pgx", adminURL.String())
	require.NoError(t, err)
	defer admin.Close()
	name := "qsim_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	roleName := name + "_runtime"
	password := uuid.NewString()
	qName, qRole := pgx.Identifier{name}.Sanitize(), pgx.Identifier{roleName}.Sanitize()
	_, err = admin.Exec(fmt.Sprintf(fixtureSQL("CreateRole"), qRole, password))
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, err := sql.Open("pgx", adminURL.String())
		if err != nil {
			t.Error(err)
			return
		}
		defer cleanup.Close()
		_, err = cleanup.Exec(fmt.Sprintf(fixtureSQL("DropDatabase"), qName))
		require.NoError(t, err)
		_, err = cleanup.Exec(fmt.Sprintf(fixtureSQL("DropRole"), qRole))
		require.NoError(t, err)
	})
	_, err = admin.Exec(fmt.Sprintf(fixtureSQL("CreateDatabase"), qName))
	require.NoError(t, err)
	fixtureURL := *adminURL
	fixtureURL.Path = "/" + name
	fixtureDB, err := sql.Open("pgx", fixtureURL.String())
	require.NoError(t, err)
	defer fixtureDB.Close()
	schema, err := os.ReadFile(filepath.Join("testdata", kind+"-schema.sql"))
	require.NoError(t, err)
	_, err = fixtureDB.Exec(string(schema))
	require.NoError(t, err)
	_, err = fixtureDB.Exec(fmt.Sprintf(fixtureSQL("GrantRuntime"), qName, qRole))
	require.NoError(t, err)
	runtimeURL := fixtureURL
	runtimeURL.User = url.UserPassword(roleName, password)
	runtimeDB, err := sql.Open("pgx", runtimeURL.String())
	require.NoError(t, err)
	defer runtimeDB.Close()
	assertRestrictedRuntime(t, runtimeDB)
	return runtimeURL.String()
}

func assertRestrictedRuntime(t *testing.T, db *sql.DB) {
	t.Helper()
	var superuser, createDB, createRole, dbOwner, schemaCreate, temp bool
	require.NoError(t, db.QueryRow(fixtureSQL("RuntimeAuthority")).Scan(&superuser, &createDB, &createRole, &dbOwner, &schemaCreate, &temp))
	require.False(t, superuser || createDB || createRole || dbOwner || schemaCreate || temp, "runtime must be non-owner with no DDL/TEMP authority")
}

func newLedgerOnGovDB(t *testing.T) *data.QuotaLedgerRepo {
	t.Helper()
	db, err := sql.Open("pgx", mustDSN(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	assertRestrictedRuntime(t, db)
	drv := entsql.OpenDB("postgres", db)
	t.Cleanup(func() { drv.Close() })
	client := ent.NewClient(ent.Driver(drv))
	c := entCrud.NewEntClient(client, drv)
	return data.NewQuotaLedgerRepoForTest(c, bLogger.NewHelper(bLogger.NopLogger()))
}

func newStack(t *testing.T) *simStack {
	t.Helper()
	certs := mustCertsDir(t)
	ownerDSN := createScratchDB(t, "owner")
	provDSN := createScratchDB(t, "provider")

	owner, err := OpenOwner(ownerDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = owner.Close() })
	provider, err := OpenProvider(provDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Close() })

	sim := NewSimulator(owner, provider)

	// 模拟器 gRPC server（mTLS）。
	gport := freePort(t)
	gsrv, err := NewGRPCServer(ServerConfig{
		Address:  fmt.Sprintf("127.0.0.1:%d", gport),
		CAFile:   filepath.Join(certs, "ca.pem"),
		CertFile: filepath.Join(certs, "ani-gpu-simulator.pem"),
		KeyFile:  filepath.Join(certs, "ani-gpu-simulator.key"),
	}, sim)
	require.NoError(t, err)
	require.NoError(t, gsrv.Start())
	t.Cleanup(gsrv.Stop)

	// Governance 内部退额 server（mTLS，精确 owner 映射）。
	lport := freePort(t)
	ledger := newLedgerOnGovDB(t)
	isrv, err := server.NewQuotaInternalServer(server.QuotaInternalServerConfig{
		Enabled:      true,
		Address:      fmt.Sprintf("127.0.0.1:%d", lport),
		CAFile:       filepath.Join(certs, "ca.pem"),
		CertFile:     filepath.Join(certs, "ani-governance.pem"),
		KeyFile:      filepath.Join(certs, "ani-governance.key"),
		CertOwnerMap: map[string]string{"ani-gpu-simulator": "ani-gpu-simulator", "ani-gpu-simulator-alias": "ani-gpu-simulator", "ani-second-owner": "ani-second-owner"},
	}, ledger)
	require.NoError(t, err)
	require.NotNil(t, isrv)
	require.NoError(t, isrv.Start(context.Background()))
	t.Cleanup(func() { _ = isrv.Stop(context.Background()) })

	notifier, err := NewNotifier(owner, NotifierConfig{
		GovernanceAddr: fmt.Sprintf("127.0.0.1:%d", lport),
		CAFile:         filepath.Join(certs, "ca.pem"),
		CertFile:       filepath.Join(certs, "ani-gpu-simulator.pem"),
		KeyFile:        filepath.Join(certs, "ani-gpu-simulator.key"),
		Interval:       200 * time.Millisecond,
	})
	require.NoError(t, err)

	// 账本 fixture：租户 + 套餐 + gpu.count=8。
	tenantID, resourceTenantID := ledgerFixture(t, ledger)

	return &simStack{
		t: t, owner: owner, provider: provider, sim: sim,
		grpcAddr: fmt.Sprintf("127.0.0.1:%d", gport),
		grpcStop: func() { gsrv.Stop() },
		ledger:   ledger,
		govAddr:  fmt.Sprintf("127.0.0.1:%d", lport),
		govClient: newGovClient(t, fmt.Sprintf("127.0.0.1:%d", lport),
			filepath.Join(certs, "ca.pem"),
			filepath.Join(certs, "ani-gpu-simulator.pem"),
			filepath.Join(certs, "ani-gpu-simulator.key")),
		notifier:         notifier,
		tenantID:         tenantID,
		resourceTenantID: resourceTenantID,
		currentCharge:    "",
		certsDir:         certs,
		ownerDSN:         ownerDSN, providerDS: provDSN,
	}
}

func ledgerFixture(t *testing.T, ledger *data.QuotaLedgerRepo) (uint32, string) {
	t.Helper()
	ctx := appViewer.NewSystemViewerContext(context.Background())
	c := ledger.EntClientForTest()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)
	plan, err := c.Plan.Create().SetNillableName(ptrStr("qsim_plan_" + suffix)).Save(ctx)
	require.NoError(t, err)
	tn, err := c.Tenant.Create().SetName("qsim_tenant_" + suffix).SetCode("qsim_tenant_" + suffix).
		SetResourceTenantID(uuid.NewString()).SetNillablePlanID(&plan.ID).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, c.PlanQuota.Create().SetPlanID(plan.ID).
		SetQuotaCode(data.QuotaCodeGpuCount).SetQuotaValue(8).Exec(ctx))
	return tn.ID, tn.ResourceTenantID
}

func ptrStr(s string) *string { return &s }

func newGovClient(t *testing.T, addr, caFile, certFile, keyFile string) quotapb.QuotaReleaseServiceClient {
	t.Helper()
	pool := x509.NewCertPool()
	pem, err := os.ReadFile(caFile)
	require.NoError(t, err)
	require.True(t, pool.AppendCertsFromPEM(pem))
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS13, RootCAs: pool, Certificates: []tls.Certificate{cert},
		ServerName: "ani-governance",
	})))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return quotapb.NewQuotaReleaseServiceClient(conn)
}

// simGRPCClient 返回模拟器 gRPC 客户端（治理身份）。
func (s *simStack) simGRPCClient(t *testing.T) quotalabpb.GpuSimulatorServiceClient {
	t.Helper()
	pool := x509.NewCertPool()
	pem, err := os.ReadFile(filepath.Join(s.certsDir, "ca.pem"))
	require.NoError(t, err)
	require.True(t, pool.AppendCertsFromPEM(pem))
	cert, err := tls.LoadX509KeyPair(filepath.Join(s.certsDir, "ani-governance.pem"),
		filepath.Join(s.certsDir, "ani-governance.key"))
	require.NoError(t, err)
	conn, err := grpc.NewClient(s.grpcAddr, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS13, RootCAs: pool, Certificates: []tls.Certificate{cert},
		ServerName: "ani-gpu-simulator",
	})))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return quotalabpb.NewGpuSimulatorServiceClient(conn)
}

func (s *simStack) occupy(t *testing.T, units int64, key string) *data.QuotaOccupyResult {
	t.Helper()
	res, err := s.ledger.Occupy(context.Background(), &data.QuotaOccupyInput{
		TenantID:         s.tenantID,
		ResourceTenantID: s.resourceTenantID,
		ResourceID:       uuidStr(),
		ActorType:        "user", ActorID: "7",
		OwnerService: "ani-gpu-simulator", Action: "LAB_GPU_CREATE",
		IdempotencyKey:   key,
		RequestHash:      "hash-" + key,
		CanonicalRequest: `{"schema_version":1}`,
		Items:            []data.QuotaOccupyItem{{QuotaCode: "gpu.count", Units: units}},
	})
	require.NoError(t, err)
	return res
}

func uuidStr() string {
	return fmt.Sprintf("%08x-0000-4000-8000-%012d", time.Now().UnixNano()&0xffffffff, time.Now().UnixNano()%1_000_000_000_000)
}

func (s *simStack) occupied(t *testing.T) int64 {
	t.Helper()
	return scalarI64(s.t, s.ledger, fixtureSQL("Occupied"), s.tenantID)
}

func (s *simStack) facts(t *testing.T) int {
	t.Helper()
	return s.factsForCharge(t, s.currentCharge)
}

func (s *simStack) factsForCharge(t *testing.T, chargeID string) int {
	t.Helper()
	var n int
	err := s.owner.db.QueryRow(fixtureSQL("FactCount"), chargeID).Scan(&n)
	require.NoError(s.t, err)
	return n
}

func (s *simStack) allocatedUnits(t *testing.T) int {
	t.Helper()
	var n int
	err := s.provider.db.QueryRow(fixtureSQL("AllocationCount")).Scan(&n)
	require.NoError(s.t, err)
	return n
}

func (s *simStack) pendingNotifies(t *testing.T) int {
	t.Helper()
	var n int
	err := s.owner.db.QueryRow(fixtureSQL("PendingNotifyCount")).Scan(&n)
	require.NoError(s.t, err)
	return n
}

func scalarI64(t *testing.T, ledger *data.QuotaLedgerRepo, q string, args ...any) int64 {
	t.Helper()
	var v int64
	err := ledger.DB().QueryRow(q, args...).Scan(&v)
	require.NoError(t, err)
	return v
}

func chargeIDOfOp(opID string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("charge:"+opID)).String()
}

func createIn(t *testing.T, s *simStack, opID, resID string, units int32, chargeID string) (bool, error) {
	return s.sim.AcceptCreate(context.Background(), &AcceptCreateInput{
		OperationID: opID, ResourceID: resID,
		TenantID: s.resourceTenantID,
		Actor:    "governance:user:7", RequestHash: "h-" + opID,
		Name: "n", GpuCount: units, ChargeID: chargeID,
	})
}

func deleteIn(t *testing.T, s *simStack, opID, createOpID, resID, chargeID string) (bool, error) {
	return s.sim.AcceptDelete(context.Background(), &AcceptDeleteInput{
		OperationID: opID, CreateOperationID: createOpID, ResourceID: resID,
		TenantID: s.resourceTenantID,
		Actor:    "governance:user:7", RequestHash: "dh-" + opID, ChargeID: chargeID,
	})
}

// ── AUTH-05：内部退额证书负向 ────────────────────────────────

func TestSimPG_AUTH05_CertNegatives(t *testing.T) {
	s := newStack(t)
	certs := s.certsDir

	dial := func(tlsCfg *tls.Config) error {
		conn, err := grpc.NewClient(s.govAddr, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
		require.NoError(t, err)
		defer func() { _ = conn.Close() }()
		c := quotapb.NewQuotaReleaseServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, err = c.ReportQuotaRelease(ctx, &quotapb.ReportQuotaReleaseRequest{
			ReleaseEventId: uuidStr(), OperationId: uuidStr(),
			Items:  []*quotapb.QuotaReleaseItem{{ChargeId: uuidStr(), QuotaCode: "gpu.count", ReleasedTotal: 1}},
			Reason: quotapb.ReleaseReason_RESOURCE_RELEASED,
		})
		return err
	}

	loadPool := func() *x509.CertPool {
		pool := x509.NewCertPool()
		pem, err := os.ReadFile(filepath.Join(certs, "ca.pem"))
		require.NoError(t, err)
		require.True(t, pool.AppendCertsFromPEM(pem))
		return pool
	}
	loadPair := func(name string) tls.Certificate {
		cert, err := tls.LoadX509KeyPair(filepath.Join(certs, name+".pem"), filepath.Join(certs, name+".key"))
		require.NoError(t, err)
		return cert
	}

	// 1) 无客户端证书。
	err := dial(&tls.Config{MinVersion: tls.VersionTLS13, RootCAs: loadPool(), ServerName: "ani-governance"})
	require.Error(t, err, "no client cert must be rejected")

	// 2) 错误 CA（自签另一套，这里用拒绝一切的方式：空池）。
	err = dial(&tls.Config{MinVersion: tls.VersionTLS13, RootCAs: x509.NewCertPool(),
		Certificates: []tls.Certificate{loadPair("ani-gpu-simulator")}, ServerName: "ani-governance"})
	require.Error(t, err, "wrong CA must fail handshake")

	// 3) 同 CA 但 SAN=other-service（未注册 owner）。
	err = dial(&tls.Config{MinVersion: tls.VersionTLS13, RootCAs: loadPool(),
		Certificates: []tls.Certificate{loadPair("other-service")}, ServerName: "ani-governance"})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, "QUOTA_RELEASE_DENIED", firstSegment(st.Message()),
		"same-CA other service must be rejected by identity mapping")

	// 4) Correct certificate plus an unknown original operation is denied by
	// the owner-scoped locator, without revealing another owner's operation.
	_, err = s.govClient.ReportQuotaRelease(context.Background(), &quotapb.ReportQuotaReleaseRequest{
		ReleaseEventId: uuidStr(), OperationId: uuidStr(),
		Items:  []*quotapb.QuotaReleaseItem{{ChargeId: uuidStr(), QuotaCode: "gpu.count", ReleasedTotal: 1}},
		Reason: quotapb.ReleaseReason_RESOURCE_RELEASED,
	})
	require.Error(t, err)
	st, ok = status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.PermissionDenied, st.Code())

	// DNS SAN aliases mapping to the same owner are accepted; an ambiguous
	// certificate matching two different registered owners is rejected before
	// any ledger mutation. This is the existing Gov DNS contract, separate from
	// Accelerator's unique URI SAN contract.
	original := s.occupy(t, 1, uuidStr())
	charge := chargeOfOp(t, s, original.OperationID)
	release := &quotapb.ReportQuotaReleaseRequest{
		ReleaseEventId: uuidStr(), OperationId: original.OperationID,
		Items:  []*quotapb.QuotaReleaseItem{{ChargeId: charge, QuotaCode: "gpu.count", ReleasedTotal: 1}},
		Reason: quotapb.ReleaseReason_RESOURCE_RELEASED,
	}
	for _, variant := range []struct {
		name     string
		accepted bool
	}{{"mixed-owner", false}, {"same-owner-alias", true}} {
		client := newGovClient(t, s.govAddr, filepath.Join(certs, "ca.pem"), filepath.Join(certs, variant.name+".pem"), filepath.Join(certs, variant.name+".key"))
		_, err = client.ReportQuotaRelease(context.Background(), release)
		if variant.accepted {
			require.NoError(t, err)
			require.Zero(t, s.occupied(t))
		} else {
			require.Equal(t, codes.Unauthenticated, status.Code(err))
			require.Contains(t, status.Convert(err).Message(), "ambiguous certificate identity")
			require.EqualValues(t, 1, s.occupied(t))
		}
	}
}

func firstSegment(msg string) string {
	for i := 0; i < len(msg); i++ {
		if msg[i] == ':' {
			return msg[:i]
		}
	}
	return msg
}

// ── FAIL-03：owner 接受后丢 ACK → 同 ID 重试不重复创建 ───────

func TestSimPG_FAIL03_AckLostRetryNoDuplication(t *testing.T) {
	s := newStack(t)
	c := s.simGRPCClient(t)
	opID, resID := uuidStr(), uuidStr()
	chargeID03 := uuidStr()
	req := &quotalabpb.AcceptCreateRequest{
		OperationId: opID, ResourceId: resID,
		TenantId: s.resourceTenantID,
		Actor:    "governance:user:7", RequestHash: "h-" + opID,
		Name: "fail03", GpuCount: 2, ChargeId: chargeID03,
		QuotaCode: "gpu.count", ChargedUnits: 2,
	}

	// 第一次发送：立即超时（ACK 丢失，服务端可能已持久接受）。
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	_, _ = c.AcceptCreate(ctx, req)
	cancel()
	time.Sleep(200 * time.Millisecond)

	// 同 ID 同报文重试：返回原接受结果，单元不重复。
	for i := 0; i < 3; i++ {
		resp, err := c.AcceptCreate(context.Background(), req)
		require.NoError(t, err)
		require.True(t, resp.GetAccepted())
	}
	require.Equal(t, 2, s.allocatedUnits(t), "retries must not duplicate units")

	// Governance 侧不因重试重复扣额：占额仍由原 charge 承担（由 Occupy 幂等保证，
	// 这里重算恒等式）。
	rows, err := s.ledger.RecomputeInvariants(context.Background(), s.tenantID)
	require.NoError(t, err)
	for _, r := range rows {
		require.True(t, r.Balanced)
	}
}

// ── FAIL-04：provider 已分配、owner 未写完成 → 重启恢复不重复 ─

func TestSimPG_FAIL04_RecoverAfterProviderCommit(t *testing.T) {
	s := newStack(t)
	opID, resID := uuidStr(), uuidStr()
	tenant := s.resourceTenantID

	// 构造中间态：owner 命令 accepted（未完成）+ provider 已分配 2 单元。
	_, err := s.owner.db.Exec(fixtureSQL("InsertAcceptedCommand"), opID, tenant, resID)
	require.NoError(t, err)
	_, err = s.provider.db.Exec(fixtureSQL("InsertProviderOperation"), opID, tenant, resID)
	require.NoError(t, err)
	for i := 1; i <= 2; i++ {
		_, err = s.provider.db.Exec(fixtureSQL("InsertAllocation"), resID, i, tenant, opID)
		require.NoError(t, err)
	}

	// 新 Simulator 实例（模拟进程重启）执行 Recover。
	fresh := NewSimulator(s.owner, s.provider)
	require.NoError(t, fresh.Recover(context.Background()))

	require.Equal(t, 2, s.allocatedUnits(t), "recovery must not re-allocate")
	var status string
	require.NoError(t, s.owner.db.QueryRow(fixtureSQL("CommandStatus"), opID).Scan(&status))
	require.Equal(t, StatusCompleted, status)
}

// ── FAIL-05/06：部分创建失败、清理失败保留占额、恢复收敛 ─────

func TestSimPG_FAIL05_06_PartialCleanupThenRecover(t *testing.T) {
	s := newStack(t)
	opID, resID := uuidStr(), uuidStr()

	// 单元 2 创建失败 + 单元 1 清理失败 → 事实 0 条，命令保持 accepted。
	s.sim.Fails.FailCreateOrdinal = 2
	s.sim.Fails.FailReleaseOrdinal = 1
	s.currentCharge = chargeIDOfOp(opID)
	_, err := createIn(t, s, opID, resID, 2, chargeIDOfOp(opID))
	require.Error(t, err, "provision failure must surface as permanent contract error")
	require.Equal(t, 0, s.facts(t), "unproven release must not produce facts")
	require.Equal(t, 1, s.allocatedUnits(t), "unit 1 stays allocated (cleanup failed)")
	require.Equal(t, 1, s.pendingNotifies(t), "notify enqueued with total=0")
	var status string
	require.NoError(t, s.owner.db.QueryRow(fixtureSQL("CommandStatus"), opID).Scan(&status))
	require.Equal(t, StatusAccepted, status, "dirty cleanup keeps command open for recovery")

	// FAIL-06：撤销注入 → 重放/恢复继续清理并完成累计退额。
	s.sim.Fails.FailReleaseOrdinal = 0
	_, err = createIn(t, s, opID, resID, 2, chargeIDOfOp(opID))
	require.NoError(t, err)
	require.Equal(t, 1, s.facts(t), "unit 1 cleaned after injection removed")
	require.Equal(t, 0, s.allocatedUnits(t), "no residual units after fence+cleanup")
	require.NoError(t, s.owner.db.QueryRow(fixtureSQL("CommandStatus"), opID).Scan(&status))
	require.Equal(t, StatusAborted, status)
	var total int
	require.NoError(t, s.owner.db.QueryRow(fixtureSQL("ReleasedNotificationTotal"),
		chargeIDOfOp(opID)).Scan(&total))
	require.Equal(t, 1, total, "cumulative release must be 1 (unit 2 was never created)")
}

// ── FAIL-08/09/16：部分退额、通知重试、账本不可用 ────────────

func TestSimPG_FAIL08_09_16_PartialReleaseNotifyRetry(t *testing.T) {
	s := newStack(t)
	res := s.occupy(t, 2, uuidStr())
	charge := chargeOfOp(t, s, res.OperationID)
	s.currentCharge = charge
	require.Equal(t, int64(2), s.occupied(t))
	_, err := createIn(t, s, res.OperationID, res.ResourceID, 2, charge)
	require.NoError(t, err)
	require.Equal(t, 2, s.allocatedUnits(t))

	// FAIL-16 辅证：Governance 地址不可达时通知保留。
	badNotifier, err := NewNotifier(s.owner, NotifierConfig{
		GovernanceAddr: "127.0.0.1:1", // closed port
		CAFile:         filepath.Join(s.certsDir, "ca.pem"),
		CertFile:       filepath.Join(s.certsDir, "ani-gpu-simulator.pem"),
		KeyFile:        filepath.Join(s.certsDir, "ani-gpu-simulator.key"),
	})
	require.NoError(t, err)
	_ = badNotifier.DeliverPending(context.Background())
	require.Equal(t, 0, s.pendingNotifies(t), "no notify yet before delete")

	// FAIL-08：第 2 单元释放失败 → 第 1 单元部分退额，第 2 继续占额。
	s.sim.Fails.FailReleaseOrdinal = 2
	delOp := uuidStr()
	_, err = deleteIn(t, s, delOp, res.OperationID, res.ResourceID, charge)
	require.NoError(t, err)
	require.Equal(t, 1, s.facts(t))
	require.Equal(t, 1, s.pendingNotifies(t))

	// FAIL-09：Governance 地址不可达 → 通知保留并计数，重启后重试。
	badNotifier, err2 := NewNotifier(s.owner, NotifierConfig{
		GovernanceAddr: "127.0.0.1:1",
		CAFile:         filepath.Join(s.certsDir, "ca.pem"),
		CertFile:       filepath.Join(s.certsDir, "ani-gpu-simulator.pem"),
		KeyFile:        filepath.Join(s.certsDir, "ani-gpu-simulator.key"),
	})
	require.NoError(t, err2)
	_ = badNotifier.DeliverPending(context.Background())
	require.Equal(t, 1, s.pendingNotifies(t), "failed delivery must keep the notify")

	// 恢复可达：同一通知投递 → Governance 释放 1。
	require.NoError(t, s.notifier.DeliverPending(context.Background()))
	require.Eventually(t, func() bool {
		return scalarI64(t, s.ledger, fixtureSQL("ReleasedChargeUnits"), s.tenantID, charge) == 1
	}, 5*time.Second, 100*time.Millisecond)
	require.Equal(t, int64(1), s.occupied(t), "partial refund: 2 -> 1")

	// 清除注入 → 重放删除 → 剩余单元释放 → 全额退额。
	s.sim.Fails.FailReleaseOrdinal = 0
	_, err = deleteIn(t, s, delOp, res.OperationID, res.ResourceID, charge)
	require.NoError(t, err)
	require.Equal(t, 2, s.facts(t))
	require.NoError(t, s.notifier.DeliverPending(context.Background()))
	require.Eventually(t, func() bool { return s.occupied(t) == 0 }, 5*time.Second, 100*time.Millisecond)
	require.Equal(t, 0, s.pendingNotifies(t))
}

func chargeOfOp(t *testing.T, s *simStack, opID string) string {
	t.Helper()
	var chargeID string
	require.NoError(t, s.ledger.DB().QueryRow(
		fixtureSQL("OperationCharge"), s.tenantID, opID).Scan(&chargeID))
	return chargeID
}

// ── FAIL-14：全额退额后重放旧创建 → 命中终态，不再分配 ───────

func TestSimPG_FAIL14_ReplayCreateAfterFullRelease(t *testing.T) {
	s := newStack(t)
	opID, resID := uuidStr(), uuidStr()
	chargeID := chargeIDOfOp(opID)
	s.currentCharge = chargeID
	_, err := createIn(t, s, opID, resID, 2, chargeID)
	require.NoError(t, err)
	_, err = deleteIn(t, s, uuidStr(), opID, resID, chargeID)
	require.NoError(t, err)
	require.Equal(t, 0, s.allocatedUnits(t))

	// 重放旧创建：幂等命中 completed，provider 不重新分配。
	ok, err := createIn(t, s, opID, resID, 2, chargeID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 0, s.allocatedUnits(t))
}

// ── FAIL-17：永久合同错误 → 暂停持额 → ResumeDispatch 收敛 ──

func TestSimPG_FAIL17_ContractErrorThenResume(t *testing.T) {
	s := newStack(t)
	res := s.occupy(t, 2, uuidStr())
	charge := chargeOfOp(t, s, res.OperationID)
	s.currentCharge = charge
	_, cerr := createIn(t, s, res.OperationID, res.ResourceID, 2, charge)
	require.NoError(t, cerr)

	// owner 返回永久合同错误（同删除 ID 异 hash）→ Governance 侧等价：
	// worker 标记 UNKNOWN + retry_blocked（保持占额，不退款）。
	_, err := deleteIn(t, s, "d-"+res.OperationID, res.OperationID, res.ResourceID, charge)
	require.NoError(t, err)
	badHash := &AcceptDeleteInput{
		OperationID: "d-" + res.OperationID, CreateOperationID: res.OperationID,
		ResourceID: res.ResourceID,
		TenantID:   s.resourceTenantID,
		Actor:      "a", RequestHash: "WRONG", ChargeID: charge,
	}
	_, err = s.sim.AcceptDelete(context.Background(), badHash)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPermanentConflict)

	ctx := appViewer.NewSystemViewerContext(context.Background())
	_, e1 := s.ledger.DB().ExecContext(ctx,
		fixtureSQL("InjectBlockedDispatch"), s.tenantID, res.OperationID)
	require.NoError(t, e1)
	require.Equal(t, int64(2), s.occupied(t), "blocked operation keeps the charge")

	// ResumeDispatch：清除 retry_blocked，保留原 operation/charge/request_hash。
	require.NoError(t, s.ledger.ResumeDispatch(ctx, s.tenantID, res.OperationID))
	blocked := scalarI64(t, s.ledger, fixtureSQL("BlockedOperationCount"), s.tenantID, res.OperationID)
	require.Equal(t, int64(0), blocked)

	// 撤销故障（正确 hash）→ 同一操作完成执行与清理 → 账本归零（不直接改余额）。
	_, err = deleteIn(t, s, "d-"+res.OperationID, res.OperationID, res.ResourceID, charge)
	require.NoError(t, err)
	require.NoError(t, s.notifier.DeliverPending(context.Background()))
	require.Eventually(t, func() bool { return s.occupied(t) == 0 }, 5*time.Second, 100*time.Millisecond)
}

// ── CON-06：同资源不同 DELETE key 并发 / ABORT 与 DELETE 交叉 ─

func TestSimPG_CON06_ConcurrentDeleteAndAbort(t *testing.T) {
	s := newStack(t)
	res := s.occupy(t, 4, uuidStr())
	chargeID := chargeOfOp(t, s, res.OperationID)
	s.currentCharge = chargeID
	_, err := createIn(t, s, res.OperationID, res.ResourceID, 4, chargeID)
	require.NoError(t, err)

	// 三个并发删除（不同 key）+ 一个并发封闭（ABORT 路径）。
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, _ = deleteIn(t, s, fmt.Sprintf("del-%d-%s", n, uuidStr()), res.OperationID, res.ResourceID, chargeID)
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = s.provider.db.Exec(fixtureSQL("CloseProviderOperation"), res.OperationID)
	}()
	wg.Wait()

	// 收敛：重放删除直到无残留。
	for i := 0; i < 5 && s.allocatedUnits(t) > 0; i++ {
		_, _ = deleteIn(t, s, "del-final-"+uuidStr(), res.OperationID, res.ResourceID, chargeID)
	}
	require.Equal(t, 0, s.allocatedUnits(t), "no residual units after convergence")

	// 同一 (charge_id, unit_ordinal) 只允许一个释放事实（唯一主键 + 去重）。
	var dupFacts int
	require.NoError(t, s.owner.db.QueryRow(fixtureSQL("DuplicateFacts")).Scan(&dupFacts))
	require.Equal(t, 0, dupFacts)
	require.Equal(t, 4, s.facts(t), "each ordinal exactly one release fact")

	// 通知累计值与事实一致，Governance 侧全额退额。
	require.NoError(t, s.notifier.DeliverPending(context.Background()))
	require.Eventually(t, func() bool { return s.occupied(t) == 0 }, 10*time.Second, 200*time.Millisecond)
}

//go:build modeldev_pg && modeldev_contract

package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	modeldevv1 "github.com/zhangzhe-ctrl/ani-modeldev-service/api/ani/modeldev/v1"
	trainingv1 "github.com/zhangzhe-ctrl/ani-modeldev-service/api/ani/training/v1"
	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01"
	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01/conformance"
	contractpb "github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01/protobuf"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/modeldevacceptance"
	"go-wind-admin/app/admin/service/internal/data/ent/modeldevreleasebinding"
	"go-wind-admin/app/admin/service/tests/testutil"
	appViewer "go-wind-admin/pkg/entgo/viewer"
	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestModelDevWorkerDeliversFrozenAcceptanceAfterRestart(t *testing.T) {
	fixture := readModelDevDeliveryFixture(t)
	ctx, cancel := context.WithTimeout(appViewer.NewSystemViewerContext(context.Background()), 40*time.Second)
	defer cancel()
	original := fixture.Frozen.envelope(fixture.Target)
	control := fixture.Frozen.envelope(fixture.Control)
	wantIntent, wantSnapshot, err := original.CanonicalPayloads()
	require.NoError(t, err)
	replayModelDevDeliveryControl(t, ctx, fixture, control)

	runtimeDSN, fixtureDSN := modelDevDeliveryDatabaseSettings(t)
	writer, _ := openModelDevDeliveryPG(t, fixtureDSN)
	initial, closeInitial := openModelDevDeliveryPG(t, runtimeDSN)
	assertModelDevDeliveryDatabaseRoles(t, ctx, initial, writer)
	owner, err := writer.Client().Tenant.Create().SetName("modeldev delivery fixture").SetCode("delivery-" + uuid.NewString()).SetResourceTenantID(original.TenantID).Save(ctx)
	require.NoError(t, err, "MODELDEV_DELIVERY_PREFLIGHT: fresh fixture tenant required")
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(appViewer.NewSystemViewerContext(context.Background()), 5*time.Second)
		defer stop()
		_, err := writer.Client().ModelDevAcceptance.Delete().Where(modeldevacceptance.TenantIDEQ(owner.ID)).Exec(cleanup)
		require.NoError(t, err)
		_, err = writer.Client().ModelDevReleaseBinding.Delete().Where(modeldevreleasebinding.TenantIDEQ(owner.ID)).Exec(cleanup)
		require.NoError(t, err)
		require.NoError(t, writer.Client().Tenant.DeleteOneID(owner.ID).Exec(cleanup))
	})
	scope := data.ModelDevAdmissionScope{TenantID: owner.ID, ResourceTenantID: original.TenantID, Actor: original.Actor, Action: data.ModelDevCreateAction, IdempotencyKey: uuid.NewString()}
	change, err := data.NewModelDevReleaseBindingRepo(initial).CompareAndSwap(ctx,
		data.ModelDevReleaseBindingScope{TenantID: owner.ID, ResourceTenantID: original.TenantID, PresetID: original.Intent.PresetID},
		data.ModelDevReleaseBindingUpdate{
			Target: data.ModelDevReleaseBindingTarget{ReleaseID: original.Snapshot.Release.ReleaseID, ReleaseDigest: original.Snapshot.Release.ReleaseDigest, NewSubmissionsEnabled: true},
			Actor:  original.Actor, RequestedAt: original.AcceptedAt.Add(-time.Minute), Reason: "synthetic delivery fixture", EvidenceReference: "contract:cpu-p01:delivery",
		})
	require.NoError(t, err)
	require.False(t, change.Replayed)
	require.Equal(t, uint64(1), change.After.Generation)
	accepted, replayed, err := data.NewModelDevAcceptanceRepo(initial).AcceptFrozen(ctx, scope, data.ModelDevFrozenCandidate{
		OperationID: original.OperationID, ExecutionID: original.ExecutionID, Intent: original.Intent, Snapshot: original.Snapshot, AcceptedAt: original.AcceptedAt,
	})
	require.NoError(t, err, "MODELDEV_DELIVERY_PREFLIGHT: original acceptance must commit before worker construction")
	require.False(t, replayed)
	require.Equal(t, "QUEUED", accepted.DispatchState)
	require.Equal(t, original.OperationID, accepted.OperationID)
	require.Equal(t, original.ExecutionID, accepted.ExecutionID)
	closeInitial()
	require.Error(t, initial.DB().PingContext(ctx), "the accepting connection must actually be closed before reconstruction")

	workerConnection, _ := openModelDevDeliveryPG(t, runtimeDSN)
	observer, _ := openModelDevDeliveryPG(t, runtimeDSN)
	assertModelDevDeliveryOriginal(t, ctx, observer, scope, original, wantIntent, wantSnapshot, "QUEUED")
	client, closeClient, err := data.NewModelDevClient(data.ModelDevClientConfig{
		Address: fixture.Address, CAFile: fixture.TLS.CAFile, CertFile: fixture.TLS.CertFile, KeyFile: fixture.TLS.KeyFile, Timeout: 3 * time.Second,
	})
	require.NoError(t, err, "MODELDEV_DELIVERY_PREFLIGHT: real fixed-identity client must construct")
	t.Cleanup(closeClient)
	worker := NewModelDevDispatchWorker(testutil.NewBootstrapContext(nil), data.NewModelDevAcceptanceRepo(workerConnection), client)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		require.NoError(t, worker.Stop(cleanup))
	})
	t.Log("MODELDEV_DELIVERY_PREFLIGHT PASS: real generated mTLS control ACK, runtime PostgreSQL role, committed frozen QUEUED record, closed accepting connection and reconstructed worker")
	if err := worker.Start(ctx); err != nil {
		if err.Error() != "modeldev dispatch worker not implemented" {
			t.Fatal("MODELDEV_DELIVERY_BEHAVIOR: unexpected worker Start failure; not the planned RED")
		}
		assertModelDevDeliveryOriginal(t, ctx, observer, scope, original, wantIntent, wantSnapshot, "QUEUED")
		// The current RED schema has no receipt column. Do not read future
		// delivery columns until the product's Start actually succeeds.
		t.Fatal("MODELDEV_DELIVERY_BEHAVIOR: modeldev dispatch worker not implemented after real PG/mTLS preflight PASS")
	}

	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(25 * time.Millisecond)
	defer poll.Stop()
	var receiptBytes []byte
	for {
		var state string
		err := observer.DB().QueryRowContext(ctx, `SELECT dispatch_state,owner_receipt_canonical FROM sys_modeldev_acceptances WHERE tenant_id=$1 AND resource_tenant_id=$2 AND operation_id=$3`, scope.TenantID, scope.ResourceTenantID, original.OperationID).Scan(&state, &receiptBytes)
		if err != nil {
			t.Fatal("MODELDEV_DELIVERY_BEHAVIOR: receipt read failed after successful Start; not the planned stub RED")
		}
		if state == "ACKED" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("MODELDEV_DELIVERY_BEHAVIOR: context ended before durable ACK")
		case <-deadline.C:
			t.Fatal("MODELDEV_DELIVERY_BEHAVIOR: worker did not persist ACK within eight seconds")
		case <-poll.C:
		}
	}
	assertModelDevDeliveryReceipt(t, receiptBytes, original)
	assertModelDevDeliveryOriginal(t, ctx, observer, scope, original, wantIntent, wantSnapshot, "ACKED")
	stopContext, stop := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, worker.Stop(stopContext))
	stop()
	reconnected, _ := openModelDevDeliveryPG(t, runtimeDSN)
	var afterRestart []byte
	require.NoError(t, reconnected.DB().QueryRowContext(ctx, `SELECT owner_receipt_canonical FROM sys_modeldev_acceptances WHERE tenant_id=$1 AND resource_tenant_id=$2 AND operation_id=$3 AND dispatch_state='ACKED'`, scope.TenantID, scope.ResourceTenantID, original.OperationID).Scan(&afterRestart))
	require.Equal(t, receiptBytes, afterRestart, "a new connection must recover the complete same durable owner receipt")
	assertModelDevDeliveryReceipt(t, afterRestart, original)
	t.Log("MODELDEV_DELIVERY_BEHAVIOR PASS: original admission retained, strict owner ACK durable after worker stop and reconnect")
}

type modelDevDeliveryIdentity struct {
	OperationID string `json:"operation_id"`
	ExecutionID string `json:"execution_id"`
}

type modelDevDeliveryFrozen struct {
	ResourceTenantID  string          `json:"resource_tenant_id"`
	Actor             string          `json:"actor"`
	Intent            cpup01.Intent   `json:"intent"`
	Snapshot          cpup01.Snapshot `json:"snapshot"`
	IntentHash        string          `json:"intent_hash"`
	ExecutionSpecHash string          `json:"execution_spec_hash"`
	AcceptedAt        time.Time       `json:"accepted_at"`
}

func (f modelDevDeliveryFrozen) envelope(identity modelDevDeliveryIdentity) cpup01.AdmissionEnvelope {
	return cpup01.AdmissionEnvelope{TenantID: f.ResourceTenantID, Actor: f.Actor, OperationID: identity.OperationID, ExecutionID: identity.ExecutionID, Intent: f.Intent, IntentHash: f.IntentHash, Snapshot: f.Snapshot, SpecHash: f.ExecutionSpecHash, AcceptedAt: f.AcceptedAt}
}

type modelDevDeliveryFixture struct {
	Schema  string `json:"schema"`
	Address string `json:"address"`
	TLS     struct {
		CAFile   string `json:"ca_file"`
		CertFile string `json:"cert_file"`
		KeyFile  string `json:"key_file"`
	} `json:"tls"`
	Frozen  modelDevDeliveryFrozen   `json:"frozen"`
	Target  modelDevDeliveryIdentity `json:"target"`
	Control modelDevDeliveryIdentity `json:"control"`
}

func readModelDevDeliveryFixture(t *testing.T) modelDevDeliveryFixture {
	t.Helper()
	file := os.Getenv("ANI_MODELDEV_CONTRACT_HANDSHAKE")
	if !filepath.IsAbs(file) || filepath.Base(file) != "handshake.json" {
		t.Fatal("MODELDEV_DELIVERY_PREFLIGHT: explicit private handshake required")
	}
	directory := filepath.Dir(file)
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("MODELDEV_DELIVERY_PREFLIGHT: private provider directory required")
	}
	t.Cleanup(func() {
		stop, err := os.OpenFile(filepath.Join(directory, "stop"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Error("MODELDEV_DELIVERY_CLEANUP: exact provider stop signal failed")
			return
		}
		if err := stop.Close(); err != nil {
			t.Error("MODELDEV_DELIVERY_CLEANUP: stop signal close failed")
		}
	})
	info, err = os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 16384 {
		t.Fatal("MODELDEV_DELIVERY_PREFLIGHT: bounded private handshake required")
	}
	raw, err := os.ReadFile(file)
	if err != nil || len(raw) > 16384 {
		t.Fatal("MODELDEV_DELIVERY_PREFLIGHT: handshake read failed")
	}
	var fixture modelDevDeliveryFixture
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&fixture) != nil || decoder.Decode(new(any)) != io.EOF {
		t.Fatal("MODELDEV_DELIVERY_PREFLIGHT: strict handshake decode failed")
	}
	host, port, addressErr := net.SplitHostPort(fixture.Address)
	portNumber, portErr := strconv.ParseUint(port, 10, 16)
	if fixture.Schema != "ani.cpu-p01.governance-delivery-fixture.v1" || addressErr != nil || host != "127.0.0.1" || portErr != nil || portNumber == 0 ||
		fixture.TLS.CAFile != filepath.Join(directory, "ca.pem") || fixture.TLS.CertFile != filepath.Join(directory, "governance.pem") || fixture.TLS.KeyFile != filepath.Join(directory, "governance.key") {
		t.Fatal("MODELDEV_DELIVERY_PREFLIGHT: handshake connection scope invalid")
	}
	canonicalHandshake, err := json.Marshal(fixture)
	if err != nil || !bytes.Equal(raw, canonicalHandshake) {
		t.Fatal("MODELDEV_DELIVERY_PREFLIGHT: handshake is not a complete canonical fixture")
	}
	for _, identity := range []modelDevDeliveryIdentity{fixture.Target, fixture.Control} {
		operation, operationErr := uuid.Parse(identity.OperationID)
		execution, executionErr := uuid.Parse(identity.ExecutionID)
		if operationErr != nil || executionErr != nil || operation == uuid.Nil || execution == uuid.Nil || operation.String() != identity.OperationID || execution.String() != identity.ExecutionID || operation == execution {
			t.Fatal("MODELDEV_DELIVERY_PREFLIGHT: invalid fixture identities")
		}
	}
	if fixture.Target.OperationID == fixture.Control.OperationID || fixture.Target.ExecutionID == fixture.Control.ExecutionID {
		t.Fatal("MODELDEV_DELIVERY_PREFLIGHT: target and control must differ")
	}
	seed, release := conformance.SnapshotV1(), conformance.ReleaseV1()
	parameters := []cpup01.Parameter{{Name: "learning_rate", Type: "DECIMAL", Value: "0.0200"}}
	wantIntent, wantIntentHash, err := cpup01.CanonicalIntent(cpup01.Intent{Name: "configured-admission", Kind: "GENERAL_TRAINING", PresetID: release.PresetID, DatasetVersionID: seed.Input.InputVersionID, GeneralParameters: &parameters})
	require.NoError(t, err)
	wantBytes, _ := testutil.ExpectedModelDevContractSnapshot(t)
	var want cpup01.Snapshot
	require.NoError(t, json.Unmarshal(wantBytes, &want))
	want.Release.AcceptedBindingGeneration = 1
	wantBytes, err = want.Canonical()
	require.NoError(t, err)
	wantHash, err := want.Digest()
	require.NoError(t, err)
	for _, identity := range []modelDevDeliveryIdentity{fixture.Target, fixture.Control} {
		actualIntent, actualSnapshot, err := fixture.Frozen.envelope(identity).CanonicalPayloads()
		if err != nil || !bytes.Equal(actualIntent, wantIntent) || !bytes.Equal(actualSnapshot, wantBytes) || fixture.Frozen.IntentHash != wantIntentHash || fixture.Frozen.ExecutionSpecHash != wantHash ||
			fixture.Frozen.ResourceTenantID != "11111111-2222-4333-8444-555555555555" || fixture.Frozen.Actor != "governance:user:42" || !fixture.Frozen.AcceptedAt.Equal(time.Date(2026, 9, 30, 12, 3, 0, 123000, time.UTC)) {
			t.Fatal("MODELDEV_DELIVERY_PREFLIGHT: frozen command differs from independently authored fixture")
		}
	}
	return fixture
}

func replayModelDevDeliveryControl(t *testing.T, ctx context.Context, fixture modelDevDeliveryFixture, original cpup01.AdmissionEnvelope) {
	t.Helper()
	ca, err := os.ReadFile(fixture.TLS.CAFile)
	if err != nil {
		t.Fatal("MODELDEV_DELIVERY_PREFLIGHT: synthetic CA unavailable")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		t.Fatal("MODELDEV_DELIVERY_PREFLIGHT: synthetic CA invalid")
	}
	certificate, err := tls.LoadX509KeyPair(fixture.TLS.CertFile, fixture.TLS.KeyFile)
	if err != nil {
		t.Fatal("MODELDEV_DELIVERY_PREFLIGHT: synthetic client identity invalid")
	}
	connection, err := grpc.NewClient(fixture.Address, grpc.WithDisableServiceConfig(), grpc.WithDisableRetry(), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{certificate}, ServerName: "ani-modeldev-service"})))
	if err != nil {
		t.Fatal("MODELDEV_DELIVERY_PREFLIGHT: generated channel configuration failed")
	}
	defer connection.Close()
	intent, err := contractpb.EncodeIntent(original.Intent)
	require.NoError(t, err)
	snapshot, err := contractpb.EncodeSnapshot(original.Snapshot)
	require.NoError(t, err)
	call, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	call = metadata.NewOutgoingContext(call, metadata.Pairs("x-ani-tenant-id", original.TenantID, "x-ani-actor", original.Actor, "x-ani-request-id", uuid.NewString()))
	identity := &trainingv1.ExecutionIdentity{OperationId: original.OperationID, ExecutionId: original.ExecutionID, ExecutionSpecHash: original.SpecHash}
	reply, err := modeldevv1.NewModelDevCommandServiceClient(connection).AcceptExecution(call, &modeldevv1.AcceptExecutionRequest{Identity: identity, ResourceTenantId: original.TenantID, AdmittedActorId: original.Actor, IntentHash: original.IntentHash, Snapshot: snapshot, AcceptedAt: timestamppb.New(original.AcceptedAt), Intent: intent}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("MODELDEV_DELIVERY_PREFLIGHT: actual control replay failed (code=%s); worker behavior NOT_RUN", status.Code(err))
	}
	want := &modeldevv1.AcceptExecutionResponse{Identity: identity, Replayed: true, Revision: 1, States: &modeldevv1.ExecutionStates{ComputeState: modeldevv1.ComputeState_COMPUTE_STATE_ACCEPTED, DeliveryState: modeldevv1.DeliveryState_DELIVERY_STATE_PENDING, ResourceState: modeldevv1.ResourceState_RESOURCE_STATE_NOT_APPLICABLE, CloseState: modeldevv1.CloseState_CLOSE_STATE_OPEN}}
	require.True(t, proto.Equal(want, reply), "MODELDEV_DELIVERY_PREFLIGHT: complete control replay receipt must match independently known facts")
}

func modelDevDeliveryDatabaseSettings(t *testing.T) (string, string) {
	t.Helper()
	dsn, file := os.Getenv("ANI_TEST_DATABASE_DSN"), os.Getenv("ANI_MODELDEV_FIXTURE_DSN_FILE")
	if dsn == "" || os.Getenv("ANI_TEST_DATABASE_EXCLUSIVE") != "1" || !filepath.IsAbs(file) {
		t.Fatal("MODELDEV_DELIVERY_PREFLIGHT: explicit exclusive PG and private fixture identity required")
	}
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 16384 {
		t.Fatal("MODELDEV_DELIVERY_PREFLIGHT: bounded private fixture identity required")
	}
	raw, err := os.ReadFile(file)
	if err != nil || len(raw) > 16384 {
		t.Fatal("MODELDEV_DELIVERY_PREFLIGHT: fixture identity read failed")
	}
	fixtureDSN := strings.TrimSpace(string(raw))
	runtimeConfig, runtimeErr := pgx.ParseConfig(dsn)
	fixtureConfig, fixtureErr := pgx.ParseConfig(fixtureDSN)
	if runtimeErr != nil || fixtureErr != nil || runtimeConfig.Host != fixtureConfig.Host || runtimeConfig.Port != fixtureConfig.Port || runtimeConfig.Database != fixtureConfig.Database || runtimeConfig.User == fixtureConfig.User {
		t.Fatal("MODELDEV_DELIVERY_PREFLIGHT: distinct fixture/runtime roles must use same task PG")
	}
	return dsn, fixtureDSN
}

func openModelDevDeliveryPG(t *testing.T, dsn string) (*entCrud.EntClient[*ent.Client], func()) {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal("MODELDEV_DELIVERY_PREFLIGHT: PostgreSQL configuration failed")
	}
	driver := entsql.OpenDB("postgres", db)
	client := ent.NewClient(ent.Driver(driver))
	closed := false
	closeConnection := func() {
		if !closed {
			require.NoError(t, client.Close())
			closed = true
		}
	}
	t.Cleanup(closeConnection)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal("MODELDEV_DELIVERY_PREFLIGHT: actual PostgreSQL connection failed")
	}
	return entCrud.NewEntClient(client, driver), closeConnection
}

func assertModelDevDeliveryDatabaseRoles(t *testing.T, ctx context.Context, runtime, writer *entCrud.EntClient[*ent.Client]) {
	t.Helper()
	var runtimeUser, fixtureUser, runtimeDatabase, fixtureDatabase, runtimeAddress, fixtureAddress string
	var runtimePort, fixturePort int
	identitySQL := `SELECT current_user,current_database(),coalesce(inet_server_addr()::text,''),coalesce(inet_server_port(),0)`
	require.NoError(t, runtime.DB().QueryRowContext(ctx, identitySQL).Scan(&runtimeUser, &runtimeDatabase, &runtimeAddress, &runtimePort))
	require.NoError(t, writer.DB().QueryRowContext(ctx, identitySQL).Scan(&fixtureUser, &fixtureDatabase, &fixtureAddress, &fixturePort))
	require.NotEqual(t, runtimeUser, fixtureUser)
	require.Equal(t, runtimeDatabase, fixtureDatabase)
	require.Equal(t, runtimeAddress, fixtureAddress)
	require.Equal(t, runtimePort, fixturePort)
	var superuser, bypassRLS, canCreate, canTemp bool
	require.NoError(t, runtime.DB().QueryRowContext(ctx, `SELECT rolsuper,rolbypassrls,has_database_privilege(current_database(),'CREATE'),has_database_privilege(current_database(),'TEMP') FROM pg_roles WHERE rolname=current_user`).Scan(&superuser, &bypassRLS, &canCreate, &canTemp))
	require.False(t, superuser)
	require.False(t, bypassRLS)
	require.False(t, canCreate)
	require.False(t, canTemp)
}

func assertModelDevDeliveryOriginal(t *testing.T, ctx context.Context, observer *entCrud.EntClient[*ent.Client], scope data.ModelDevAdmissionScope, original cpup01.AdmissionEnvelope, wantIntent, wantSnapshot []byte, state string) {
	t.Helper()
	row, err := observer.Client().ModelDevAcceptance.Query().Where(modeldevacceptance.TenantIDEQ(scope.TenantID), modeldevacceptance.ResourceTenantIDEQ(scope.ResourceTenantID), modeldevacceptance.OperationIDEQ(original.OperationID)).Only(ctx)
	require.NoError(t, err)
	require.NotNil(t, row.TenantID)
	require.Equal(t, scope.TenantID, *row.TenantID)
	require.Equal(t, original.TenantID, row.ResourceTenantID)
	require.Equal(t, original.Actor, row.Actor)
	require.Equal(t, scope.Action, row.Action)
	require.Equal(t, scope.IdempotencyKey, row.IdempotencyKey)
	require.Equal(t, original.OperationID, row.OperationID)
	require.Equal(t, original.ExecutionID, row.ExecutionID)
	require.Equal(t, wantIntent, row.IntentCanonical)
	require.Equal(t, wantSnapshot, row.SnapshotCanonical)
	require.Equal(t, original.IntentHash, row.IntentHash)
	require.Equal(t, original.SpecHash, row.ExecutionSpecHash)
	require.True(t, original.AcceptedAt.Equal(row.AcceptedAt))
	require.Equal(t, state, string(row.DispatchState))
	count, err := observer.Client().ModelDevAcceptance.Query().Where(modeldevacceptance.TenantIDEQ(scope.TenantID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count, "the same frozen acceptance is the only queued or acknowledged command")
}

func assertModelDevDeliveryReceipt(t *testing.T, raw []byte, original cpup01.AdmissionEnvelope) {
	t.Helper()
	var receipt struct {
		Schema   string `json:"schema"`
		Identity struct {
			OperationID       string `json:"operation_id"`
			ExecutionID       string `json:"execution_id"`
			ExecutionSpecHash string `json:"execution_spec_hash"`
		} `json:"identity"`
		States struct {
			ComputeState  string `json:"compute_state"`
			DeliveryState string `json:"delivery_state"`
			ResourceState string `json:"resource_state"`
			CloseState    string `json:"close_state"`
		} `json:"states"`
		Revision string `json:"revision"`
		Replayed bool   `json:"replayed"`
	}
	require.LessOrEqual(t, len(raw), 4096)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	require.NoError(t, decoder.Decode(&receipt))
	require.Equal(t, io.EOF, decoder.Decode(new(any)))
	canonical, err := json.Marshal(receipt)
	require.NoError(t, err)
	require.Equal(t, canonical, raw, "complete ordered receipt must reject omitted fields, duplicates, nulls and trailing whitespace")
	require.Equal(t, "ani.governance.modeldev-owner-receipt.v1", receipt.Schema)
	require.Equal(t, original.OperationID, receipt.Identity.OperationID)
	require.Equal(t, original.ExecutionID, receipt.Identity.ExecutionID)
	require.Equal(t, original.SpecHash, receipt.Identity.ExecutionSpecHash)
	require.Equal(t, "ACCEPTED", receipt.States.ComputeState)
	require.Equal(t, "PENDING", receipt.States.DeliveryState)
	require.Equal(t, "NOT_APPLICABLE", receipt.States.ResourceState)
	require.Equal(t, "OPEN", receipt.States.CloseState)
	revision, err := strconv.ParseUint(receipt.Revision, 10, 64)
	require.NoError(t, err)
	require.Equal(t, uint64(1), revision, "this provider permits no owner facts beyond the original admission")
	require.Equal(t, strconv.FormatUint(revision, 10), receipt.Revision)
}

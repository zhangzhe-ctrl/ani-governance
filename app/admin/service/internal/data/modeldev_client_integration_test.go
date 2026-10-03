//go:build modeldev_contract

package data

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	modeldevv1 "github.com/zhangzhe-ctrl/ani-modeldev-service/api/ani/modeldev/v1"
	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01"
	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01/conformance"
	contractpb "github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01/protobuf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The provider is the separately selected ModelDev governance_contract test.
// It runs real buildApp, pinned files and PostgreSQL; this package imports only
// the provider's public API/contract, never its internal implementation.
func TestModelDevResolveUsesRealManagedAdmissionProvider(t *testing.T) {
	fixture := readModelDevContractFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	wantCanonical, wantHash := expectedModelDevContractSnapshot(t)
	ca, err := os.ReadFile(fixture.TLS.CAFile)
	if err != nil {
		t.Fatal("MODELDEV_CLIENT_PREFLIGHT: private CA unavailable; behavior NOT_RUN")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		t.Fatal("MODELDEV_CLIENT_PREFLIGHT: private CA invalid; behavior NOT_RUN")
	}
	certificate, err := tls.LoadX509KeyPair(fixture.TLS.CertFile, fixture.TLS.KeyFile)
	if err != nil {
		t.Fatal("MODELDEV_CLIENT_PREFLIGHT: private client material invalid; behavior NOT_RUN")
	}
	connection, err := grpc.NewClient(fixture.Address, grpc.WithDisableServiceConfig(), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{certificate}, ServerName: "ani-modeldev-service",
	})))
	if err != nil {
		t.Fatal("MODELDEV_CLIENT_PREFLIGHT: direct channel configuration failed; behavior NOT_RUN")
	}
	t.Cleanup(func() { _ = connection.Close() })
	intent, err := contractpb.EncodeIntent(fixture.Intent)
	if err != nil {
		t.Fatal("MODELDEV_CLIENT_PREFLIGHT: fixture intent invalid; behavior NOT_RUN")
	}
	directContext := metadata.NewOutgoingContext(ctx, metadata.Pairs(
		"x-ani-tenant-id", fixture.Scope.ResourceTenantID, "x-ani-actor", fixture.Scope.Actor, "x-ani-request-id", uuid.NewString(),
	))
	direct, err := modeldevv1.NewModelDevAdmissionServiceClient(connection).ResolveAdmission(directContext, &modeldevv1.ResolveAdmissionRequest{
		Intent: intent,
		Release: &modeldevv1.AdmissionReleaseSelection{ReleaseId: fixture.Release.ReleaseID, ReleaseDigest: fixture.Release.ReleaseDigest, BindingGeneration: fixture.Release.BindingGeneration},
		AcceptedAt: timestamppb.New(fixture.AcceptedAt),
	}, grpc.WaitForReady(true))
	if err != nil || direct == nil {
		t.Fatalf("MODELDEV_CLIENT_PREFLIGHT: generated direct RPC unavailable (code=%s); behavior NOT_RUN", status.Code(err))
	}
	directSnapshot, err := contractpb.DecodeSnapshot(direct.Snapshot)
	if err != nil {
		t.Fatal("MODELDEV_CLIENT_PREFLIGHT: direct RPC returned invalid snapshot; behavior NOT_RUN")
	}
	directCanonical, err := directSnapshot.Canonical()
	if err != nil || !bytes.Equal(directCanonical, wantCanonical) || direct.ExecutionSpecHash != wantHash {
		t.Fatal("MODELDEV_CLIENT_PREFLIGHT: real provider differs from independently authored snapshot/hash; behavior NOT_RUN")
	}
	t.Log("MODELDEV_CLIENT_PREFLIGHT PASS: generated direct RPC over real mTLS to ModelDev buildApp with pinned files and durable READY input")

	client, closeClient, err := NewModelDevClient(ModelDevClientConfig{
		Address: fixture.Address, CAFile: fixture.TLS.CAFile, CertFile: fixture.TLS.CertFile, KeyFile: fixture.TLS.KeyFile, Timeout: 3*time.Second,
	})
	if err != nil || client == nil || closeClient == nil {
		t.Fatal("MODELDEV_CLIENT_BEHAVIOR: valid client configuration rejected")
	}
	t.Cleanup(closeClient)
	resolved, err := client.Resolve(ctx,
		ModelDevResolveScope{ResourceTenantID: fixture.Scope.ResourceTenantID, Actor: fixture.Scope.Actor},
		fixture.Intent,
		ModelDevReleaseSelection{ReleaseID: fixture.Release.ReleaseID, ReleaseDigest: fixture.Release.ReleaseDigest, BindingGeneration: fixture.Release.BindingGeneration},
		fixture.AcceptedAt,
	)
	if err != nil {
		if status.Code(err) == codes.Unimplemented && status.Convert(err).Message() == "modeldev resolution client not implemented" {
			t.Fatal("MODELDEV_CLIENT_BEHAVIOR: modeldev resolution client not implemented after real provider preflight PASS")
		}
		t.Fatalf("MODELDEV_CLIENT_BEHAVIOR: resolution failed (code=%s)", status.Code(err))
	}
	canonical, err := resolved.Snapshot.Canonical()
	if err != nil || !bytes.Equal(canonical, wantCanonical) || resolved.ExecutionSpecHash != wantHash {
		t.Fatal("MODELDEV_CLIENT_BEHAVIOR: client did not return the independent complete snapshot/hash")
	}
}

type modelDevContractFixture struct {
	Schema string `json:"schema"`
	Address string `json:"address"`
	TLS struct {
		CAFile string `json:"ca_file"`
		CertFile string `json:"cert_file"`
		KeyFile string `json:"key_file"`
	} `json:"tls"`
	Scope struct {
		ResourceTenantID string `json:"resource_tenant_id"`
		Actor string `json:"actor"`
	} `json:"scope"`
	Intent cpup01.Intent `json:"intent"`
	Release struct {
		ReleaseID string `json:"release_id"`
		ReleaseDigest string `json:"release_digest"`
		BindingGeneration uint64 `json:"binding_generation"`
	} `json:"release"`
	AcceptedAt time.Time `json:"accepted_at"`
}

func readModelDevContractFixture(t *testing.T) modelDevContractFixture {
	t.Helper()
	file := os.Getenv("ANI_MODELDEV_CONTRACT_HANDSHAKE")
	if !filepath.IsAbs(file) || filepath.Base(file) != "handshake.json" {
		t.Fatal("MODELDEV_CLIENT_PREFLIGHT: explicit private handshake required; behavior NOT_RUN")
	}
	directory := filepath.Dir(file)
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("MODELDEV_CLIENT_PREFLIGHT: private provider directory unavailable; behavior NOT_RUN")
	}
	// Cleanup requests provider shutdown even when the intentional product RED
	// calls Fatal. No credential or handshake contents enter logs or artifacts.
	t.Cleanup(func() {
		stop, err := os.OpenFile(filepath.Join(directory, "stop"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Error("MODELDEV_CLIENT_CLEANUP: cannot signal the exact provider fixture")
			return
		}
		if err := stop.Close(); err != nil {
			t.Error("MODELDEV_CLIENT_CLEANUP: cannot close provider stop signal")
		}
	})
	info, err = os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 16384 {
		t.Fatal("MODELDEV_CLIENT_PREFLIGHT: bounded private handshake unavailable; behavior NOT_RUN")
	}
	raw, err := os.ReadFile(file)
	if err != nil || len(raw) > 16384 {
		t.Fatal("MODELDEV_CLIENT_PREFLIGHT: handshake read failed; behavior NOT_RUN")
	}
	var fixture modelDevContractFixture
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&fixture) != nil || decoder.Decode(new(any)) != io.EOF {
		t.Fatal("MODELDEV_CLIENT_PREFLIGHT: handshake schema invalid; behavior NOT_RUN")
	}
	host, port, err := net.SplitHostPort(fixture.Address)
	portNumber, portErr := strconv.ParseUint(port, 10, 16)
	if err != nil || host != "127.0.0.1" || portErr != nil || portNumber == 0 ||
		fixture.Schema != "ani.cpu-p01.governance-resolve-fixture.v1" ||
		fixture.TLS.CAFile != filepath.Join(directory, "ca.pem") || fixture.TLS.CertFile != filepath.Join(directory, "governance.pem") || fixture.TLS.KeyFile != filepath.Join(directory, "governance.key") {
		t.Fatal("MODELDEV_CLIENT_PREFLIGHT: handshake connection scope invalid; behavior NOT_RUN")
	}
	release, seed := conformance.ReleaseV1(), conformance.SnapshotV1()
	parameters := []cpup01.Parameter{{Name: "learning_rate", Type: "DECIMAL", Value: "0.0200"}}
	wantIntent := cpup01.Intent{Name: "configured-admission", Kind: "GENERAL_TRAINING", PresetID: release.PresetID, DatasetVersionID: seed.Input.InputVersionID, GeneralParameters: &parameters}
	wantIntentCanonical, _, err := cpup01.CanonicalIntent(wantIntent)
	actualIntentCanonical, _, actualErr := cpup01.CanonicalIntent(fixture.Intent)
	if err != nil || actualErr != nil || !bytes.Equal(actualIntentCanonical, wantIntentCanonical) ||
		fixture.Scope.ResourceTenantID != "11111111-2222-4333-8444-555555555555" || fixture.Scope.Actor != "governance:user:42" ||
		fixture.Release.ReleaseID != release.ReleaseID || fixture.Release.ReleaseDigest != conformance.ReleaseSHA256V1 || fixture.Release.BindingGeneration != 7 ||
		!fixture.AcceptedAt.Equal(time.Date(2026, 9, 30, 12, 3, 0, 123000, time.UTC)) {
		t.Fatal("MODELDEV_CLIENT_PREFLIGHT: handshake differs from fixed fixture facts; behavior NOT_RUN")
	}
	return fixture
}

func expectedModelDevContractSnapshot(t *testing.T) ([]byte, string) {
	t.Helper()
	release, seed := conformance.ReleaseV1(), conformance.SnapshotV1()
	// This expectation is authored from fixed fixture facts, not either RPC
	// response or the client. Only the normative source vectors are shared.
	want := cpup01.Snapshot{
		SchemaVersion: cpup01.SnapshotSchemaVersion, Kind: "GENERAL_TRAINING", DeliveryMode: "SAVE_ARTIFACTS",
		Release: cpup01.ReleaseSnapshot{
			ReleaseID: release.ReleaseID, ReleaseDigest: conformance.ReleaseSHA256V1, PresetID: release.PresetID, AcceptedBindingGeneration: 7,
			PipelineID: release.PipelineID, PipelineVersionID: release.PipelineVersionID, PipelineIRSHA256: release.PipelineIRSHA256, Runtime: release.Runtime,
		},
		Input: seed.Input,
		Program: cpup01.ProgramRef{
			ImageVersionID: release.Program.ImageVersionID, ImageDigest: release.Program.ImageDigest, Command: release.Program.Command,
			ResolvedArgs: []string{"--data", "/startup-input/data.csv", "--output", "/startup-output", "--expected-input-sha256", "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", "--expected-input-bytes", "192456", "--learning-rate", "0.02"},
			ResolvedParameters: []cpup01.Parameter{{Name: "batch_size", Type: "INTEGER", Value: "64"}, {Name: "epochs", Type: "INTEGER", Value: "3"}, {Name: "learning_rate", Type: "DECIMAL", Value: "0.02"}},
		},
		Resources: release.Resources, Environment: seed.Environment, Workspace: release.Workspace,
		PublicationScope: seed.PublicationScope, OutputContract: release.OutputContract,
		DeadlineAt: time.Date(2026, 9, 30, 12, 33, 0, 123000, time.UTC),
	}
	canonical, err := want.Canonical()
	if err != nil {
		t.Fatal("MODELDEV_CLIENT_PREFLIGHT: independent snapshot fixture invalid; behavior NOT_RUN")
	}
	digest := sha256.Sum256(canonical)
	return canonical, hex.EncodeToString(digest[:])
}

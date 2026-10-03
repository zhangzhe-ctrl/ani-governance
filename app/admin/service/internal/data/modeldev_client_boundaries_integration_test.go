//go:build modeldev_contract

package data

import (
	"bytes"
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"go-wind-admin/app/admin/service/tests/testutil"
	modeldevv1 "github.com/zhangzhe-ctrl/ani-modeldev-service/api/ani/modeldev/v1"
	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01"
	contractpb "github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01/protobuf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The runner starts a fresh real provider for this entire suite. Read the
// handshake exactly once: its existing cleanup is the sole stop-signal owner.
func TestModelDevResolveRealProviderBoundaries(t *testing.T) {
	fixture := testutil.ReadModelDevContractFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	config := ModelDevClientConfig{Address: fixture.Address, CAFile: fixture.TLS.CAFile, CertFile: fixture.TLS.CertFile, KeyFile: fixture.TLS.KeyFile, Timeout: 2 * time.Second}
	scope := ModelDevResolveScope{ResourceTenantID: fixture.Scope.ResourceTenantID, Actor: fixture.Scope.Actor}
	selection := ModelDevReleaseSelection{ReleaseID: fixture.Release.ReleaseID, ReleaseDigest: fixture.Release.ReleaseDigest, BindingGeneration: fixture.Release.BindingGeneration}
	wantCanonical, wantHash := testutil.ExpectedModelDevContractSnapshot(t)
	wireIntent, err := contractpb.EncodeIntent(fixture.Intent)
	if err != nil {
		t.Fatal("MODELDEV_BOUNDARY_PREFLIGHT: fixed intent cannot encode; behavior NOT_RUN")
	}
	direct := modeldevv1.NewModelDevAdmissionServiceClient(modelDevDirectTLSConnection(t, config, "ani-modeldev-service"))
	directContext := metadata.NewOutgoingContext(ctx, metadata.Pairs("x-ani-tenant-id", scope.ResourceTenantID, "x-ani-actor", scope.Actor, "x-ani-request-id", uuid.NewString()))
	control, err := direct.ResolveAdmission(directContext, &modeldevv1.ResolveAdmissionRequest{
		Intent: wireIntent, Release: &modeldevv1.AdmissionReleaseSelection{ReleaseId: selection.ReleaseID, ReleaseDigest: selection.ReleaseDigest, BindingGeneration: selection.BindingGeneration}, AcceptedAt: timestamppb.New(fixture.AcceptedAt),
	}, grpc.WaitForReady(true))
	if err != nil || control == nil {
		t.Fatalf("MODELDEV_BOUNDARY_PREFLIGHT: real generated RPC failed (%s); behavior NOT_RUN", status.Code(err))
	}
	snapshot, err := contractpb.DecodeSnapshot(control.Snapshot)
	if err != nil {
		t.Fatal("MODELDEV_BOUNDARY_PREFLIGHT: real provider returned invalid snapshot; behavior NOT_RUN")
	}
	canonical, err := snapshot.Canonical()
	if err != nil || !bytes.Equal(canonical, wantCanonical) || control.ExecutionSpecHash != wantHash {
		t.Fatal("MODELDEV_BOUNDARY_PREFLIGHT: real provider differs from authored complete expected facts; behavior NOT_RUN")
	}
	t.Log("MODELDEV_BOUNDARY_PREFLIGHT PASS: generated direct RPC, actual separate ModelDev buildApp, mTLS, pinned files and recovered PostgreSQL READY")
	client := newModelDevBoundaryClient(t, config)

	t.Run("rebuild metadata and retain caller values", func(t *testing.T) {
		poison := metadata.Pairs(
			"x-ani-tenant-id", uuid.NewString(), "x-ani-tenant-id", uuid.NewString(),
			"x-ani-actor", "governance:user:900", "x-ani-actor", "governance:user:901",
			"x-ani-request-id", uuid.NewString(), "x-ani-request-id", uuid.NewString(),
			"authorization", "Bearer caller-private-sentinel", "cookie", "caller-private-sentinel",
			"x-forwarded-client-cert", "caller-private-sentinel", "ani-workload-token", "caller-private-sentinel",
		)
		beforeMetadata := poison.Copy()
		beforeIntent, _, err := cpup01.CanonicalIntent(fixture.Intent)
		if err != nil {
			t.Fatal("MODELDEV_BOUNDARY_PREFLIGHT: original intent cannot canonicalize")
		}
		polluted := metadata.NewOutgoingContext(metadata.NewIncomingContext(ctx, poison), poison)
		resolved, err := client.Resolve(polluted, scope, fixture.Intent, selection, fixture.AcceptedAt)
		assertRealModelDevCandidate(t, resolved, err, wantCanonical, wantHash)
		afterIntent, _, canonicalErr := cpup01.CanonicalIntent(fixture.Intent)
		outgoing, _ := metadata.FromOutgoingContext(polluted)
		incoming, _ := metadata.FromIncomingContext(polluted)
		if canonicalErr != nil || !bytes.Equal(afterIntent, beforeIntent) || !reflect.DeepEqual(poison, beforeMetadata) || !reflect.DeepEqual(outgoing, beforeMetadata) || !reflect.DeepEqual(incoming, beforeMetadata) {
			t.Error("client mutated original caller context or intent")
		}
	})

	t.Run("tenant facts are not borrowed and request IDs are fresh", func(t *testing.T) {
		other := scope
		other.ResourceTenantID = uuid.NewString()
		lastID := ""
		for i := 0; i < 2; i++ {
			resolved, err := client.Resolve(ctx, other, fixture.Intent, selection, fixture.AcceptedAt)
			assertModelDevSafeFailure(t, resolved, err, codes.Unavailable, "managed admission environment is not ready", modeldevv1.ErrorReason_ERROR_REASON_ENVIRONMENT_NOT_READY)
			if err != nil {
				details := status.Convert(err).Details()
				if len(details) == 1 {
					if detail, ok := details[0].(*modeldevv1.ErrorDetail); ok {
						if detail.CorrelationId == lastID {
							t.Error("different RPCs reused the same request identity")
						}
						lastID = detail.CorrelationId
					}
				}
			}
		}
		resolved, err := client.Resolve(ctx, scope, fixture.Intent, selection, fixture.AcceptedAt)
		assertRealModelDevCandidate(t, resolved, err, wantCanonical, wantHash)
	})

	for _, test := range []struct {
		name    string
		change  func(*cpup01.Intent)
		code    codes.Code
		reason  modeldevv1.ErrorReason
		message string
	}{
		{"missing input", func(i *cpup01.Intent) { i.DatasetVersionID = uuid.NewString() }, codes.NotFound, modeldevv1.ErrorReason_ERROR_REASON_RESOURCE_NOT_FOUND, "input version unavailable"},
		{"incompatible intent preset", func(i *cpup01.Intent) { i.PresetID = uuid.NewString() }, codes.FailedPrecondition, modeldevv1.ErrorReason_ERROR_REASON_NO_COMPATIBLE_RELEASE, "selected Release is unavailable or incompatible"},
	} {
		t.Run(test.name, func(t *testing.T) {
			intent := fixture.Intent
			test.change(&intent)
			if _, _, err := cpup01.CanonicalIntent(intent); err != nil {
				t.Fatal("MODELDEV_BOUNDARY_PREFLIGHT: provider-denial request is not valid")
			}
			resolved, err := client.Resolve(ctx, scope, intent, selection, fixture.AcceptedAt)
			assertModelDevSafeFailure(t, resolved, err, test.code, test.message, test.reason)
		})
	}

	for _, field := range []string{"zero tenant", "actor exceeds uint32", "zero generation", "invalid digest", "zero time", "submicrosecond time"} {
		t.Run(field, func(t *testing.T) {
			badScope, badSelection, at := scope, selection, fixture.AcceptedAt
			switch field {
			case "zero tenant":
				badScope.ResourceTenantID = uuid.Nil.String()
			case "actor exceeds uint32":
				badScope.Actor = "governance:user:4294967296"
			case "zero generation":
				badSelection.BindingGeneration = 0
			case "invalid digest":
				badSelection.ReleaseDigest = "not-a-digest"
			case "zero time":
				at = time.Time{}
			case "submicrosecond time":
				at = at.Add(time.Nanosecond)
			}
			resolved, err := client.Resolve(ctx, badScope, fixture.Intent, badSelection, at)
			assertModelDevSafeFailure(t, resolved, err, codes.InvalidArgument, "invalid modeldev resolution request", 0)
		})
	}

	t.Run("caller cancellation wins over validation", func(t *testing.T) {
		canceled, stop := context.WithCancel(ctx)
		stop()
		resolved, err := client.Resolve(canceled, ModelDevResolveScope{}, fixture.Intent, selection, fixture.AcceptedAt)
		assertModelDevSafeFailure(t, resolved, err, codes.Canceled, "context canceled", 0)
		expired, stop := context.WithDeadline(ctx, time.Now().Add(-time.Second))
		defer stop()
		resolved, err = client.Resolve(expired, ModelDevResolveScope{}, fixture.Intent, selection, fixture.AcceptedAt)
		assertModelDevSafeFailure(t, resolved, err, codes.DeadlineExceeded, "context deadline exceeded", 0)
	})

	t.Run("foreign CA cannot reach real provider", func(t *testing.T) {
		foreign, _ := modelDevBoundaryTLS(t, []string{"ani-modeldev-service"}, []string{"ani-governance"})
		bad := config
		bad.CAFile = foreign.CAFile
		resolved, err := newModelDevBoundaryClient(t, bad).Resolve(ctx, scope, fixture.Intent, selection, fixture.AcceptedAt)
		assertModelDevSafeFailure(t, resolved, err, codes.Unavailable, "modeldev admission unavailable", 0)
	})
	resolved, err := client.Resolve(ctx, scope, fixture.Intent, selection, fixture.AcceptedAt)
	assertRealModelDevCandidate(t, resolved, err, wantCanonical, wantHash)
	// After this suite stops the provider, its separate process independently
	// checks READY equality, all execution-side tables empty, and pool cleanup.
}

func assertRealModelDevCandidate(t *testing.T, got ModelDevResolution, err error, wantCanonical []byte, wantHash string) {
	t.Helper()
	canonical, canonicalErr := got.Snapshot.Canonical()
	if err != nil || canonicalErr != nil || !bytes.Equal(canonical, wantCanonical) || got.ExecutionSpecHash != wantHash {
		t.Errorf("real provider candidate changed or failed: code=%s", status.Code(err))
	}
}

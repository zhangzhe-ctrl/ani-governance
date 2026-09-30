//go:build modeldev_pg

package data

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01"
	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01/conformance"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/modeldevacceptance"
	"go-wind-admin/app/admin/service/internal/data/ent/modeldevreleasebinding"
	appViewer "go-wind-admin/pkg/entgo/viewer"
	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"
)

func TestModelDevAcceptanceRejectsMissingCurrentBindingWithoutConsumingKey(t *testing.T) {
	ctx, client, scope, candidate := newModelDevAcceptanceFixture(t)
	// A different tenant's pointer for the same preset is not this tenant's
	// permission to submit. Both tenants use the same normative snapshot.
	_, otherClient, otherScope, otherCandidate := newModelDevAcceptanceFixture(t)
	bindModelDevAcceptanceFixture(t, ctx, otherClient, otherScope, otherCandidate)
	accepted, replayed, err := NewModelDevAcceptanceRepo(client).AcceptFrozen(ctx, scope, candidate)
	require.ErrorIs(t, err, ErrModelDevBindingNotFound)
	require.Nil(t, accepted)
	require.False(t, replayed)
	reader := newModelDevPGClient(t)
	count, err := reader.Client().ModelDevAcceptance.Query().Where(modeldevacceptance.TenantIDEQ(scope.TenantID)).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)

	bindModelDevAcceptanceFixture(t, ctx, client, scope, candidate)
	accepted, replayed, err = NewModelDevAcceptanceRepo(reader).AcceptFrozen(ctx, scope, candidate)
	require.NoError(t, err, "creating the binding must leave the rejected key available")
	require.False(t, replayed)
	require.Equal(t, candidate.OperationID, accepted.OperationID)
	require.Equal(t, candidate.ExecutionID, accepted.ExecutionID)
	require.Equal(t, conformance.SnapshotSHA256V1, accepted.ExecutionSpecHash)
}

func TestModelDevAcceptanceRejectsDisabledOrChangedBindingWithoutConsumingKey(t *testing.T) {
	cases := []struct {
		name string
		want error
	}{
		{"disabled gate", ErrModelDevBindingDisabled},
		{"stale generation after pause and resume", ErrModelDevBindingGenerationConflict},
		{"different release ID", ErrModelDevBindingGenerationConflict},
		{"different release digest", ErrModelDevBindingGenerationConflict},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx, client, scope, original := newModelDevAcceptanceFixture(t)
			binding := bindModelDevAcceptanceFixture(t, ctx, client, scope, original)
			candidate := original
			switch test.name {
			case "disabled gate", "stale generation after pause and resume":
				target := binding.Target
				target.NewSubmissionsEnabled = false
				binding = changeModelDevAcceptanceBinding(t, ctx, client, binding, target)
				if test.name == "disabled gate" {
					candidate.Snapshot.Release.AcceptedBindingGeneration = binding.Generation
				} else {
					target.NewSubmissionsEnabled = true
					binding = changeModelDevAcceptanceBinding(t, ctx, client, binding, target)
				}
			case "different release ID":
				candidate.Snapshot.Release.ReleaseID = uuid.NewString()
			case "different release digest":
				candidate.Snapshot.Release.ReleaseDigest = strings.Repeat("b", 64)
			}
			// These candidates satisfy the shared immutable envelope. The
			// failure must come from Governance's current persisted binding.
			requireValidModelDevCandidate(t, scope, candidate)
			accepted, replayed, err := NewModelDevAcceptanceRepo(client).AcceptFrozen(ctx, scope, candidate)
			require.ErrorIs(t, err, test.want)
			require.Nil(t, accepted)
			require.False(t, replayed)
			reader := newModelDevPGClient(t)
			count, err := reader.Client().ModelDevAcceptance.Query().Where(modeldevacceptance.TenantIDEQ(scope.TenantID)).Count(ctx)
			require.NoError(t, err)
			require.Zero(t, count, "binding rejection must not persist a queued delivery")
			actual, err := NewModelDevReleaseBindingRepo(reader).Get(ctx, binding.Scope)
			require.NoError(t, err)
			require.Equal(t, binding, actual, "admission does not modify the current pointer")

			if !binding.Target.NewSubmissionsEnabled {
				target := binding.Target
				target.NewSubmissionsEnabled = true
				binding = changeModelDevAcceptanceBinding(t, ctx, client, binding, target)
			}
			corrected := original
			corrected.Snapshot.Release.AcceptedBindingGeneration = binding.Generation
			accepted, replayed, err = NewModelDevAcceptanceRepo(reader).AcceptFrozen(ctx, scope, corrected)
			require.NoError(t, err, "corrected binding resolution reuses the same key and IDs")
			require.False(t, replayed)
			require.Equal(t, original.OperationID, accepted.OperationID)
			require.Equal(t, original.ExecutionID, accepted.ExecutionID)
			require.Equal(t, binding.Generation, accepted.Snapshot.Release.AcceptedBindingGeneration)
			require.Equal(t, "QUEUED", accepted.DispatchState)
		})
	}
}

func TestModelDevAcceptanceReplaysAcrossBindingChangesAndPause(t *testing.T) {
	ctx, client, scope, candidate := newModelDevAcceptanceFixture(t)
	binding := bindModelDevAcceptanceFixture(t, ctx, client, scope, candidate)
	original, replayed, err := NewModelDevAcceptanceRepo(client).AcceptFrozen(ctx, scope, candidate)
	require.NoError(t, err)
	require.False(t, replayed)

	target := ModelDevReleaseBindingTarget{ReleaseID: uuid.NewString(), ReleaseDigest: strings.Repeat("c", 64), NewSubmissionsEnabled: false}
	binding = changeModelDevAcceptanceBinding(t, ctx, client, binding, target)
	reader := newModelDevPGClient(t)
	// A retry only needs its original intent; it must not resolve or validate
	// a replacement snapshot while the current release is switched or paused.
	actual, replayed, err := NewModelDevAcceptanceRepo(reader).AcceptFrozen(ctx, scope, ModelDevFrozenCandidate{Intent: candidate.Intent})
	require.NoError(t, err)
	require.True(t, replayed)
	require.Equal(t, original.OperationID, actual.OperationID)
	require.Equal(t, original.ExecutionID, actual.ExecutionID)
	require.Equal(t, original.IntentHash, actual.IntentHash)
	require.Equal(t, conformance.SnapshotSHA256V1, actual.ExecutionSpecHash)
	canonical, err := actual.Snapshot.Canonical()
	require.NoError(t, err)
	require.Equal(t, conformance.SnapshotCanonicalV1(), canonical)

	conflicting := ModelDevFrozenCandidate{Intent: candidate.Intent}
	conflicting.Intent.Name = "different CPU request"
	rejected, replayed, err := NewModelDevAcceptanceRepo(reader).AcceptFrozen(ctx, scope, conflicting)
	require.ErrorIs(t, err, ErrModelDevIdempotencyConflict)
	require.Nil(t, rejected)
	require.False(t, replayed)

	nextScope, nextCandidate := scope, candidate
	nextScope.IdempotencyKey = uuid.NewString()
	nextCandidate.OperationID, nextCandidate.ExecutionID = uuid.NewString(), uuid.NewString()
	nextCandidate.Snapshot.Release.ReleaseID = binding.Target.ReleaseID
	nextCandidate.Snapshot.Release.ReleaseDigest = binding.Target.ReleaseDigest
	nextCandidate.Snapshot.Release.AcceptedBindingGeneration = binding.Generation
	requireValidModelDevCandidate(t, nextScope, nextCandidate)
	rejected, replayed, err = NewModelDevAcceptanceRepo(reader).AcceptFrozen(ctx, nextScope, nextCandidate)
	require.ErrorIs(t, err, ErrModelDevBindingDisabled)
	require.Nil(t, rejected)
	require.False(t, replayed)
	count, err := reader.Client().ModelDevAcceptance.Query().Where(modeldevacceptance.TenantIDEQ(scope.TenantID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count, "pause preserves the original record and rejects only new admissions")
}

// Only acceptance scenarios opt in to this setup. The shared unbound fixture
// remains usable by binding repository tests and missing-binding negatives.
// CAS advances the real pointer to the normative snapshot's generation; it
// never rewrites the shared snapshot/hash or claims synthetic assets exist.
func bindModelDevAcceptanceFixture(t *testing.T, ctx context.Context, client *entCrud.EntClient[*ent.Client], scope ModelDevAdmissionScope, candidate ModelDevFrozenCandidate) *ModelDevReleaseBinding {
	t.Helper()
	require.Equal(t, uint64(7), candidate.Snapshot.Release.AcceptedBindingGeneration)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(appViewer.NewSystemViewerContext(context.Background()), 5*time.Second)
		defer cancel()
		_, err := client.Client().ModelDevReleaseBinding.Delete().Where(modeldevreleasebinding.TenantIDEQ(scope.TenantID)).Exec(cleanup)
		require.NoError(t, err)
	})
	bindingScope := ModelDevReleaseBindingScope{TenantID: scope.TenantID, ResourceTenantID: scope.ResourceTenantID, PresetID: candidate.Snapshot.Release.PresetID}
	var binding *ModelDevReleaseBinding
	for generation := uint64(1); generation <= candidate.Snapshot.Release.AcceptedBindingGeneration; generation++ {
		change, err := NewModelDevReleaseBindingRepo(client).CompareAndSwap(ctx, bindingScope, ModelDevReleaseBindingUpdate{
			ExpectedGeneration: generation - 1,
			Target: ModelDevReleaseBindingTarget{
				ReleaseID: candidate.Snapshot.Release.ReleaseID, ReleaseDigest: candidate.Snapshot.Release.ReleaseDigest,
				NewSubmissionsEnabled: generation%2 == 1,
			},
			Actor: scope.Actor, RequestedAt: candidate.AcceptedAt.Add(-time.Minute).Add(time.Duration(generation) * time.Second),
			Reason: "synthetic acceptance fixture", EvidenceReference: "test:cpu-p01-conformance-v1",
		})
		require.NoError(t, err)
		require.False(t, change.Replayed)
		require.Equal(t, generation, change.After.Generation)
		binding = change.After
	}
	return binding
}

func changeModelDevAcceptanceBinding(t *testing.T, ctx context.Context, client *entCrud.EntClient[*ent.Client], binding *ModelDevReleaseBinding, target ModelDevReleaseBindingTarget) *ModelDevReleaseBinding {
	t.Helper()
	change, err := NewModelDevReleaseBindingRepo(client).CompareAndSwap(ctx, binding.Scope, ModelDevReleaseBindingUpdate{
		ExpectedGeneration: binding.Generation, Target: target,
		Actor: "governance:user:8", RequestedAt: binding.UpdatedAt.Add(time.Second),
		Reason: "synthetic acceptance binding change", EvidenceReference: "test:cpu-p01-binding-change",
	})
	require.NoError(t, err)
	require.False(t, change.Replayed)
	require.Equal(t, binding.Generation+1, change.After.Generation)
	return change.After
}

func requireValidModelDevCandidate(t *testing.T, scope ModelDevAdmissionScope, candidate ModelDevFrozenCandidate) {
	t.Helper()
	_, intentHash, err := cpup01.CanonicalIntent(candidate.Intent)
	require.NoError(t, err)
	specHash, err := candidate.Snapshot.Digest()
	require.NoError(t, err)
	envelope := cpup01.AdmissionEnvelope{
		TenantID: scope.ResourceTenantID, Actor: scope.Actor,
		OperationID: candidate.OperationID, ExecutionID: candidate.ExecutionID,
		Intent: candidate.Intent, IntentHash: intentHash,
		Snapshot: candidate.Snapshot, SpecHash: specHash, AcceptedAt: candidate.AcceptedAt,
	}
	_, _, err = envelope.CanonicalPayloads()
	require.NoError(t, err)
}

//go:build modeldev_pg

package data

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go-wind-admin/app/admin/service/internal/data/ent/modeldevreleasebinding"
	appViewer "go-wind-admin/pkg/entgo/viewer"
)

func TestModelDevReleaseBindingPersistsCASAndReplaysSameTarget(t *testing.T) {
	ctx, client, admissionScope, candidate := newModelDevAcceptanceFixture(t)
	_, err := client.Client().ModelDevReleaseBinding.Query().Limit(1).Exist(ctx)
	require.NoError(t, err, "binding migration and runtime read grant must exist before product RED")
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(appViewer.NewSystemViewerContext(context.Background()), 5*time.Second)
		defer stop()
		_, err := client.Client().ModelDevReleaseBinding.Delete().Where(modeldevreleasebinding.TenantIDEQ(admissionScope.TenantID)).Exec(cleanup)
		require.NoError(t, err)
	})
	scope := ModelDevReleaseBindingScope{
		TenantID: admissionScope.TenantID, ResourceTenantID: admissionScope.ResourceTenantID,
		PresetID: candidate.Snapshot.Release.PresetID,
	}
	initial := ModelDevReleaseBindingUpdate{
		ExpectedGeneration: 0,
		Target: ModelDevReleaseBindingTarget{
			ReleaseID: candidate.Snapshot.Release.ReleaseID, ReleaseDigest: candidate.Snapshot.Release.ReleaseDigest,
			NewSubmissionsEnabled: true,
		},
		Actor: "governance:user:7", RequestedAt: candidate.AcceptedAt,
		Reason: "contract fixture activation", EvidenceReference: "contract:cpu-p01:release-a",
	}
	// Synthetic catalogue references exercise persistence only. No test value
	// asserts real release verification or enables a live environment.
	change, err := NewModelDevReleaseBindingRepo(client).CompareAndSwap(ctx, scope, initial)
	require.NoError(t, err)
	require.NotNil(t, change)
	require.Nil(t, change.Before)
	require.False(t, change.Replayed)
	first := &ModelDevReleaseBinding{
		Scope: scope, Target: initial.Target, Generation: 1,
		UpdatedBy: initial.Actor, UpdatedAt: initial.RequestedAt,
		Reason: initial.Reason, EvidenceReference: initial.EvidenceReference,
	}
	require.Equal(t, first, change.After)

	reader := newModelDevPGClient(t)
	reconstructed := NewModelDevReleaseBindingRepo(reader)
	stored, err := reconstructed.Get(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, first, stored, "an independent connection observes the committed binding")
	replay := initial
	replay.Actor = "governance:access-key:9"
	replay.RequestedAt = replay.RequestedAt.Add(time.Minute)
	replay.Reason = "same target after an ambiguous response"
	replay.EvidenceReference = "contract:cpu-p01:retry-evidence"
	change, err = reconstructed.CompareAndSwap(ctx, scope, replay)
	require.NoError(t, err)
	require.True(t, change.Replayed)
	require.Equal(t, first, change.Before)
	require.Equal(t, first, change.After, "same target preserves the original generation and audit")

	next := initial
	next.ExpectedGeneration = 1
	next.Target.ReleaseID = uuid.NewString()
	next.Target.ReleaseDigest = strings.Repeat("b", 64)
	next.RequestedAt = initial.RequestedAt.Add(2 * time.Minute)
	next.Reason, next.EvidenceReference = "contract fixture switch", "contract:cpu-p01:release-b"
	change, err = reconstructed.CompareAndSwap(ctx, scope, next)
	require.NoError(t, err)
	require.False(t, change.Replayed)
	require.Equal(t, first, change.Before)
	second := &ModelDevReleaseBinding{
		Scope: scope, Target: next.Target, Generation: 2,
		UpdatedBy: next.Actor, UpdatedAt: next.RequestedAt,
		Reason: next.Reason, EvidenceReference: next.EvidenceReference,
	}
	require.Equal(t, second, change.After)

	stale := next
	stale.Target.ReleaseID = uuid.NewString()
	stale.Target.ReleaseDigest = strings.Repeat("c", 64)
	stale.RequestedAt = next.RequestedAt.Add(time.Minute)
	stale.Reason, stale.EvidenceReference = "stale fixture switch", "contract:cpu-p01:release-c"
	change, err = reconstructed.CompareAndSwap(ctx, scope, stale)
	require.ErrorIs(t, err, ErrModelDevBindingGenerationConflict)
	require.Nil(t, change)
	stored, err = NewModelDevReleaseBindingRepo(client).Get(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, second, stored, "a stale generation cannot overwrite current target or audit")
	count, err := reader.Client().ModelDevReleaseBinding.Query().Where(modeldevreleasebinding.TenantIDEQ(scope.TenantID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

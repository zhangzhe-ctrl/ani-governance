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
	"go-wind-admin/app/admin/service/internal/data/ent/modeldevreleasebinding"
	appViewer "go-wind-admin/pkg/entgo/viewer"
)

func TestModelDevReleaseBindingRejectsGenerationOverflowAtomically(t *testing.T) {
	ctx, client, admissionScope, candidate := newModelDevAcceptanceFixture(t)
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
		Target: ModelDevReleaseBindingTarget{
			ReleaseID: candidate.Snapshot.Release.ReleaseID, ReleaseDigest: candidate.Snapshot.Release.ReleaseDigest,
			NewSubmissionsEnabled: true,
		},
		Actor: "governance:user:7", RequestedAt: candidate.AcceptedAt,
		Reason: "contract boundary fixture", EvidenceReference: "contract:cpu-p01:release-a",
	}
	repo := NewModelDevReleaseBindingRepo(client)
	_, err := repo.CompareAndSwap(ctx, scope, initial)
	require.NoError(t, err)
	const maximum uint64 = 1<<63 - 1
	// Seed the final representable generation as test setup; reaching it by
	// that many product calls is impractical. Assertions use the repository
	// interface and an independent reader, not the setup mutation as behavior.
	count, err := client.Client().ModelDevReleaseBinding.Update().Where(
		modeldevreleasebinding.TenantIDEQ(scope.TenantID),
		modeldevreleasebinding.ResourceTenantIDEQ(scope.ResourceTenantID),
		modeldevreleasebinding.PresetIDEQ(scope.PresetID),
	).SetGeneration(maximum).Save(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	reader := NewModelDevReleaseBindingRepo(newModelDevPGClient(t))
	before, err := reader.Get(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, maximum, before.Generation)

	request := initial
	request.ExpectedGeneration = maximum
	request.Actor = "governance:access-key:9"
	request.RequestedAt = initial.RequestedAt.Add(time.Minute)
	request.Reason = "boundary retry"
	request.EvidenceReference = "contract:cpu-p01:boundary-retry"
	t.Run("same target remains replayable at the maximum", func(t *testing.T) {
		change, err := repo.CompareAndSwap(ctx, scope, request)
		require.NoError(t, err)
		require.True(t, change.Replayed)
		require.Equal(t, before, change.Before)
		require.Equal(t, before, change.After)
	})
	t.Run("out of range expected generation cannot replay", func(t *testing.T) {
		invalid := request
		invalid.ExpectedGeneration = maximum + 1
		change, err := repo.CompareAndSwap(ctx, scope, invalid)
		require.ErrorIs(t, err, cpup01.ErrInvalidArgument)
		require.Nil(t, change)
		stored, err := reader.Get(ctx, scope)
		require.NoError(t, err)
		require.Equal(t, before, stored)
	})
	t.Run("different target cannot consume an overflowing generation", func(t *testing.T) {
		next := request
		next.Target.ReleaseID = uuid.NewString()
		next.Target.ReleaseDigest = strings.Repeat("b", 64)
		change, err := repo.CompareAndSwap(ctx, scope, next)
		require.ErrorIs(t, err, ErrModelDevBindingGenerationExhausted)
		require.Nil(t, change)
		stored, err := reader.Get(ctx, scope)
		require.NoError(t, err)
		require.Equal(t, before, stored, "overflow must preserve target, gate, generation and audit")
	})
	t.Run("out of range expected generation cannot initialize a scope", func(t *testing.T) {
		unbound := scope
		unbound.PresetID = uuid.NewString()
		invalid := request
		invalid.ExpectedGeneration = maximum + 1
		change, err := repo.CompareAndSwap(ctx, unbound, invalid)
		require.ErrorIs(t, err, cpup01.ErrInvalidArgument)
		require.Nil(t, change)
		stored, err := reader.Get(ctx, unbound)
		require.ErrorIs(t, err, ErrModelDevBindingNotFound)
		require.Nil(t, stored, "invalid generation must not create a current pointer")
	})
}

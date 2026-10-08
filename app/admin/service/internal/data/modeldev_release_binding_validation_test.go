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
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/modeldevreleasebinding"
	appViewer "go-wind-admin/pkg/entgo/viewer"
	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"
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

func TestModelDevReleaseBindingRejectsInvalidParametersWithoutMutation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ModelDevReleaseBindingScope, *ModelDevReleaseBindingUpdate)
	}{
		{"zero local tenant", func(scope *ModelDevReleaseBindingScope, _ *ModelDevReleaseBindingUpdate) {
			scope.TenantID = 0
		}},
		{"malformed resource tenant", func(scope *ModelDevReleaseBindingScope, _ *ModelDevReleaseBindingUpdate) {
			scope.ResourceTenantID = "not-a-uuid"
		}},
		{"zero resource tenant", func(scope *ModelDevReleaseBindingScope, _ *ModelDevReleaseBindingUpdate) {
			scope.ResourceTenantID = uuid.Nil.String()
		}},
		{"missing preset", func(scope *ModelDevReleaseBindingScope, _ *ModelDevReleaseBindingUpdate) {
			scope.PresetID = ""
		}},
		{"malformed preset", func(scope *ModelDevReleaseBindingScope, _ *ModelDevReleaseBindingUpdate) {
			scope.PresetID = "preset-name"
		}},
		{"zero preset", func(scope *ModelDevReleaseBindingScope, _ *ModelDevReleaseBindingUpdate) {
			scope.PresetID = uuid.Nil.String()
		}},
		{"compact preset UUID", func(scope *ModelDevReleaseBindingScope, _ *ModelDevReleaseBindingUpdate) {
			scope.PresetID = strings.ReplaceAll(scope.PresetID, "-", "")
		}},
		{"malformed release", func(_ *ModelDevReleaseBindingScope, update *ModelDevReleaseBindingUpdate) {
			update.Target.ReleaseID = "private-value-must-not-be-echoed"
		}},
		{"zero release", func(_ *ModelDevReleaseBindingScope, update *ModelDevReleaseBindingUpdate) {
			update.Target.ReleaseID = uuid.Nil.String()
		}},
		{"URN release UUID", func(_ *ModelDevReleaseBindingScope, update *ModelDevReleaseBindingUpdate) {
			update.Target.ReleaseID = "urn:uuid:" + update.Target.ReleaseID
		}},
		{"floating release digest", func(_ *ModelDevReleaseBindingScope, update *ModelDevReleaseBindingUpdate) {
			update.Target.ReleaseDigest = "latest"
		}},
		{"nonhex release digest", func(_ *ModelDevReleaseBindingScope, update *ModelDevReleaseBindingUpdate) {
			update.Target.ReleaseDigest = strings.Repeat("z", 64)
		}},
		{"uppercase release digest", func(_ *ModelDevReleaseBindingScope, update *ModelDevReleaseBindingUpdate) {
			update.Target.ReleaseDigest = strings.Repeat("A", 64)
		}},
		{"missing audit actor", func(_ *ModelDevReleaseBindingScope, update *ModelDevReleaseBindingUpdate) {
			update.Actor = ""
		}},
		{"audit actor control character", func(_ *ModelDevReleaseBindingScope, update *ModelDevReleaseBindingUpdate) {
			update.Actor = "governance:user:7\n"
		}},
		{"zero audit time", func(_ *ModelDevReleaseBindingScope, update *ModelDevReleaseBindingUpdate) {
			update.RequestedAt = time.Time{}
		}},
		{"audit time loses precision", func(_ *ModelDevReleaseBindingScope, update *ModelDevReleaseBindingUpdate) {
			update.RequestedAt = update.RequestedAt.Add(time.Nanosecond)
		}},
		{"audit time outside public timestamp range", func(_ *ModelDevReleaseBindingScope, update *ModelDevReleaseBindingUpdate) {
			update.RequestedAt = time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
		}},
		{"missing reason", func(_ *ModelDevReleaseBindingScope, update *ModelDevReleaseBindingUpdate) {
			update.Reason = ""
		}},
		{"blank reason", func(_ *ModelDevReleaseBindingScope, update *ModelDevReleaseBindingUpdate) {
			update.Reason = "  \t  "
		}},
		{"invalid UTF8 reason", func(_ *ModelDevReleaseBindingScope, update *ModelDevReleaseBindingUpdate) {
			update.Reason = string([]byte{0xff})
		}},
		{"NUL reason cannot roundtrip PostgreSQL text", func(_ *ModelDevReleaseBindingScope, update *ModelDevReleaseBindingUpdate) {
			update.Reason = "reason\x00suffix"
		}},
		{"missing evidence reference", func(_ *ModelDevReleaseBindingScope, update *ModelDevReleaseBindingUpdate) {
			update.EvidenceReference = ""
		}},
		{"blank evidence reference", func(_ *ModelDevReleaseBindingScope, update *ModelDevReleaseBindingUpdate) {
			update.EvidenceReference = "  "
		}},
		{"invalid UTF8 evidence reference", func(_ *ModelDevReleaseBindingScope, update *ModelDevReleaseBindingUpdate) {
			update.EvidenceReference = string([]byte{0xff})
		}},
		{"NUL evidence cannot roundtrip PostgreSQL text", func(_ *ModelDevReleaseBindingScope, update *ModelDevReleaseBindingUpdate) {
			update.EvidenceReference = "contract:release\x00suffix"
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx, client, scope, initial := newModelDevBindingValidationFixture(t)
			repo := NewModelDevReleaseBindingRepo(client)
			invalidScope, invalid := scope, initial
			test.mutate(&invalidScope, &invalid)
			change, err := repo.CompareAndSwap(ctx, invalidScope, invalid)
			require.ErrorIs(t, err, cpup01.ErrInvalidArgument)
			require.NotContains(t, err.Error(), "private-value-must-not-be-echoed")
			require.Nil(t, change)
			if invalidScope != scope {
				stored, err := repo.Get(ctx, invalidScope)
				require.ErrorIs(t, err, cpup01.ErrInvalidArgument)
				require.Nil(t, stored)
			}
			count, err := client.Client().ModelDevReleaseBinding.Query().Where(modeldevreleasebinding.TenantIDEQ(scope.TenantID)).Count(ctx)
			require.NoError(t, err)
			require.Zero(t, count, "invalid parameters must not initialize a pointer")
			first, err := repo.CompareAndSwap(ctx, scope, initial)
			require.NoError(t, err, "a corrected request can still initialize generation one")
			require.False(t, first.Replayed)
			require.Equal(t, uint64(1), first.After.Generation)

			// Parameter validation still applies when the requested target is
			// already current. Replay must not turn a malformed request into success.
			invalid.ExpectedGeneration = 1
			change, err = repo.CompareAndSwap(ctx, invalidScope, invalid)
			require.ErrorIs(t, err, cpup01.ErrInvalidArgument)
			require.Nil(t, change)
			reader := NewModelDevReleaseBindingRepo(newModelDevPGClient(t))
			stored, err := reader.Get(ctx, scope)
			require.NoError(t, err)
			require.Equal(t, first.After, stored, "a rejected retry must preserve all binding and audit fields")
		})
	}
}

func TestModelDevReleaseBindingCanonicalizesIDsBeforeLookupAndReplay(t *testing.T) {
	ctx, client, scope, initial := newModelDevBindingValidationFixture(t)
	scope.PresetID = "aaaaaaaa-1111-4111-8111-111111111111"
	initial.Target.ReleaseID = "bbbbbbbb-2222-4222-8222-222222222222"
	upperScope, upper := scope, initial
	upperScope.ResourceTenantID = strings.ToUpper(scope.ResourceTenantID)
	upperScope.PresetID = strings.ToUpper(scope.PresetID)
	upper.Target.ReleaseID = strings.ToUpper(initial.Target.ReleaseID)
	upper.RequestedAt = initial.RequestedAt.In(time.FixedZone("fixture-offset", 3600))
	repo := NewModelDevReleaseBindingRepo(client)
	change, err := repo.CompareAndSwap(ctx, upperScope, upper)
	require.NoError(t, err)
	require.False(t, change.Replayed)
	expected := &ModelDevReleaseBinding{
		Scope: scope, Target: initial.Target, Generation: 1,
		UpdatedBy: initial.Actor, UpdatedAt: initial.RequestedAt.UTC(),
		Reason: initial.Reason, EvidenceReference: initial.EvidenceReference,
	}
	require.Equal(t, expected, change.After)
	reader := NewModelDevReleaseBindingRepo(newModelDevPGClient(t))
	for _, query := range []ModelDevReleaseBindingScope{scope, upperScope} {
		stored, err := reader.Get(ctx, query)
		require.NoError(t, err)
		require.Equal(t, expected, stored)
	}
	for _, request := range []ModelDevReleaseBindingUpdate{initial, upper} {
		change, err := repo.CompareAndSwap(ctx, scope, request)
		require.NoError(t, err)
		require.True(t, change.Replayed)
		require.Equal(t, expected, change.Before)
		require.Equal(t, expected, change.After, "UUID spelling must not consume a generation")
	}
}

func newModelDevBindingValidationFixture(t *testing.T) (context.Context, *entCrud.EntClient[*ent.Client], ModelDevReleaseBindingScope, ModelDevReleaseBindingUpdate) {
	t.Helper()
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
		Reason: "contract validation fixture", EvidenceReference: "contract:cpu-p01:release-a",
	}
	return ctx, client, scope, initial
}

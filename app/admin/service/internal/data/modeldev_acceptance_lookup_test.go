//go:build modeldev_pg

package data

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01/conformance"
	"go-wind-admin/app/admin/service/internal/data/ent/modeldevacceptance"
)

func TestModelDevAcceptanceFindsOriginalBeforeCatalogueResolution(t *testing.T) {
	ctx, client, scope, candidate := newModelDevAcceptanceFixture(t)
	repo := NewModelDevAcceptanceRepo(client)
	// The preliminary read needs only intent. A miss must work before any
	// current binding exists and leave the key available for real acceptance.
	missing, err := repo.FindAccepted(ctx, scope, candidate.Intent)
	require.ErrorIs(t, err, ErrModelDevAcceptanceNotFound)
	require.Nil(t, missing)
	count, err := client.Client().ModelDevAcceptance.Query().Where(modeldevacceptance.TenantIDEQ(scope.TenantID)).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)

	binding := bindModelDevAcceptanceFixture(t, ctx, client, scope, candidate)
	original, replayed, err := repo.AcceptFrozen(ctx, scope, candidate)
	require.NoError(t, err)
	require.False(t, replayed)
	target := ModelDevReleaseBindingTarget{
		ReleaseID: uuid.NewString(), ReleaseDigest: strings.Repeat("c", 64),
		NewSubmissionsEnabled: false,
	}
	binding = changeModelDevAcceptanceBinding(t, ctx, client, binding, target)

	reader := newModelDevPGClient(t)
	actual, err := NewModelDevAcceptanceRepo(reader).FindAccepted(ctx, scope, candidate.Intent)
	require.NoError(t, err)
	require.NotNil(t, actual)
	require.Equal(t, original.Scope, actual.Scope)
	require.Equal(t, original.OperationID, actual.OperationID)
	require.Equal(t, original.ExecutionID, actual.ExecutionID)
	require.Equal(t, original.IntentHash, actual.IntentHash)
	require.Equal(t, conformance.SnapshotSHA256V1, actual.ExecutionSpecHash)
	require.Equal(t, "QUEUED", actual.DispatchState)
	require.True(t, original.AcceptedAt.Equal(actual.AcceptedAt))
	canonical, err := actual.Snapshot.Canonical()
	require.NoError(t, err)
	require.Equal(t, conformance.SnapshotCanonicalV1(), canonical)
	count, err = reader.Client().ModelDevAcceptance.Query().Where(modeldevacceptance.TenantIDEQ(scope.TenantID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count, "lookup must neither requeue nor create another acceptance")
	current, err := NewModelDevReleaseBindingRepo(reader).Get(ctx, binding.Scope)
	require.NoError(t, err)
	require.Equal(t, binding, current, "lookup must leave the changed current pointer and gate untouched")
}

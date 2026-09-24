//go:build quota_pg

package data

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Runs only on a separately restored old-populated.dump after the three new
// migrations. This gate deliberately has no fixture insert or cleanup step.
func TestQuotaGpuOldPopulatedUpgradeReplay(t *testing.T) {
	if os.Getenv("QUOTA_UPGRADE_VERIFY") != "1" {
		t.Skip("requires separately upgraded populated old backup")
	}
	c := newLedgerPGClient(t)
	r := newTestRepo(c)
	ctx := context.Background()
	const id = "90000000-0000-4000-8000-000000000001"
	const key = "90000000-0000-4000-8000-000000000003"
	const chargeID = "90000000-0000-4000-8000-000000000004"
	old, e := r.FindIdempotentOperation(ctx, 900, "user", "900", "LAB_GPU_CREATE", key)
	require.NoError(t, e)
	require.Equal(t, id, old.OperationID)
	require.Equal(t, "old-fixture-hash", old.RequestHash)
	accepted, e := r.Occupy(ctx, &QuotaOccupyInput{TenantID: 900, ResourceTenantID: "90000000-0000-4000-8000-000000000900", ActorType: "user", ActorID: "900", OwnerService: "ani-gpu-simulator", Action: "LAB_GPU_CREATE", IdempotencyKey: key, RequestHash: "old-fixture-hash", CanonicalRequest: `{"schema_version":1}`, ResourceID: "90000000-0000-4000-8000-000000000002", Items: []QuotaOccupyItem{{QuotaCode: "gpu.count", Units: 3}}})
	require.NoError(t, e)
	require.Equal(t, id, accepted.OperationID)
	require.Equal(t, int64(2), accountOccupied(t, c, 900, "gpu.count"))
	receipt := &QuotaReleaseInput{OwnerService: "ani-gpu-simulator", OperationID: id, ReleaseEventID: "90000000-0000-4000-8000-000000000006", Reason: "RESOURCE_RELEASED", PayloadHash: "old-release-hash", PayloadJSON: `{"released_total":1}`, Items: []QuotaReleaseItemInput{{ChargeID: chargeID, QuotaCode: "gpu.count", ReleasedTotal: 1}}}
	rows, e := r.Release(ctx, receipt)
	require.NoError(t, e)
	require.Len(t, rows, 1)
	require.Zero(t, rows[0].AppliedDelta)
	require.Equal(t, int64(1), rows[0].ReleasedTotal)
	receipt.PayloadHash = "conflicting-old-event"
	_, e = r.Release(ctx, receipt)
	require.Error(t, e)
	require.Equal(t, int64(2), accountOccupied(t, c, 900, "gpu.count"))
	receipt.ReleaseEventID = uuid.NewString()
	receipt.PayloadHash = "upgrade-next-release"
	receipt.Items[0].ReleasedTotal = 2
	rows, e = r.Release(ctx, receipt)
	require.NoError(t, e)
	require.Equal(t, int64(1), rows[0].AppliedDelta)
	require.Equal(t, int64(1), accountOccupied(t, c, 900, "gpu.count"))
	invariant, e := r.RecomputeInvariants(ctx, 900)
	require.NoError(t, e)
	require.Len(t, invariant, 1)
	require.True(t, invariant[0].Balanced)
}

//go:build quota_pg

package data

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	appViewer "go-wind-admin/pkg/entgo/viewer"
)

func TestPendingSandboxOperationIsScopedAndNotAReceipt(t *testing.T) {
	ctx := context.Background()
	client := newLedgerPGClient(t)
	cleanLedger(t, client)
	repo := newTestRepo(client)
	system := appViewer.NewSystemViewerContext(ctx)
	firstTenant, err := client.Client().Tenant.Create().SetName("pending-one").SetCode("pending-one").Save(system)
	require.NoError(t, err)
	secondTenant, err := client.Client().Tenant.Create().SetName("pending-two").SetCode("pending-two").Save(system)
	require.NoError(t, err)
	tenant1, tenant2 := firstTenant.ID, secondTenant.ID
	resourceTenant1, resourceTenant2 := uuid.NewString(), uuid.NewString()
	operation := uuid.NewString()
	first := SandboxCreateIntent{TenantID: tenant1, ResourceTenantID: resourceTenant1, SandboxID: uuid.NewString(), OperationID: operation, IntentDigest: "one", Actor: "governance:user:1", Command: []byte(`{}`)}
	second := SandboxCreateIntent{TenantID: tenant2, ResourceTenantID: resourceTenant2, SandboxID: uuid.NewString(), OperationID: operation, IntentDigest: "two", Actor: "governance:user:2", Command: []byte(`{}`), CommandKind: "checkpoint_clone"}
	_, _, err = repo.RegisterSandboxCreateIntent(ctx, first)
	require.NoError(t, err)
	_, _, err = repo.RegisterSandboxCreateIntent(ctx, second)
	require.NoError(t, err)
	a, err := repo.FindPendingSandboxOperation(ctx, tenant1, resourceTenant1, operation)
	require.NoError(t, err)
	require.NotNil(t, a)
	require.Equal(t, first.SandboxID, a.SandboxID)
	require.Equal(t, "create", a.CommandKind)
	require.False(t, a.CreatedAt.IsZero())
	b, err := repo.FindPendingSandboxOperation(ctx, tenant2, resourceTenant2, operation)
	require.NoError(t, err)
	require.NotNil(t, b)
	require.Equal(t, second.SandboxID, b.SandboxID)
	require.Equal(t, "checkpoint_clone", b.CommandKind)
	for _, tc := range []struct {
		tenant                    uint32
		resourceTenant, operation string
	}{
		{tenant1, resourceTenant2, operation}, {tenant2, resourceTenant1, operation}, {tenant1, resourceTenant1, uuid.NewString()},
	} {
		got, err := repo.FindPendingSandboxOperation(ctx, tc.tenant, tc.resourceTenant, tc.operation)
		require.NoError(t, err)
		require.Nil(t, got)
	}
	require.NoError(t, repo.AcknowledgeSandboxCreate(ctx, tenant1, first.SandboxID, operation))
	a, err = repo.FindPendingSandboxOperation(ctx, tenant1, resourceTenant1, operation)
	require.NoError(t, err)
	require.Nil(t, a, "an acknowledged intent is not an unconfirmed observation")
	b, err = repo.FindPendingSandboxOperation(ctx, tenant2, resourceTenant2, operation)
	require.NoError(t, err)
	require.NotNil(t, b, "another tenant remains independent")
}

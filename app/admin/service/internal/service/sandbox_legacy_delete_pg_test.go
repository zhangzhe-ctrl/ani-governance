//go:build quota_pg

package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/tests/testutil"
	appViewer "go-wind-admin/pkg/entgo/viewer"
)

func TestSandboxLegacyInstanceDeleteDispatchIsBlocked(t *testing.T) {
	c := testutil.NewQuotaPGClient(t)
	testutil.ResetQuotaFixture(t, c)
	ctx := context.Background()
	sys := appViewer.NewSystemViewerContext(ctx)
	name := "legacy-instance-delete-" + uuid.NewString()
	tenant, err := c.Client().Tenant.Create().SetName(name).SetCode(name).Save(sys)
	require.NoError(t, err)
	createID, deleteID, resource := uuid.NewString(), uuid.NewString(), uuid.NewString()
	// A row predating the migration has no preflight marker. The raw INSERT is
	// the external historical fixture; production still uses ledger acceptance.
	for _, row := range []struct{ id, action, original, state string }{{createID, InstanceCreateAction, "", "ACKED"}, {deleteID, InstanceDeleteAction, createID, "DISPATCHING"}} {
		_, err = c.DB().ExecContext(ctx, `INSERT INTO sys_quota_operations(tenant_id,operation_id,resource_tenant_id,resource_id,create_operation_id,actor_type,actor_id,owner_service,action,idempotency_key,request_hash,canonical_request,dispatch_state,instance_contract_version,created_at,updated_at) VALUES($1,$2,$3,$4,NULLIF($5,''),'user','1',$6,$7,$8,'hash','{}',$9,2,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, tenant.ID, row.id, tenant.ResourceTenantID, resource, row.original, InstanceOwner, row.action, uuid.NewString(), row.state)
		require.NoError(t, err)
	}
	ledger := data.NewQuotaLedgerRepo(testutil.NewBootstrapContext(nil), c)
	// This tests dispatch after the historical row is already claimed; the
	// fixture's canonical body deliberately does not model the quota contract.
	claimed := data.ClaimedOperation{TenantID: tenant.ID, OperationID: deleteID, ResourceID: resource, OwnerService: InstanceOwner, Action: InstanceDeleteAction}
	worker := NewQuotaDispatchWorker(testutil.NewBootstrapContext(nil), ledger, NewQuotaAdapterRegistry())
	worker.dispatchOne(ctx, &claimed)
	op, err := ledger.GetOperationForUser(ctx, tenant.ID, deleteID)
	require.NoError(t, err)
	require.Equal(t, "UNKNOWN", string(op.DispatchState))
	require.True(t, op.RetryBlocked)
	require.NotNil(t, op.LastErrorCode)
	require.Equal(t, "SANDBOX_DELETE_CHECK_MISSING", *op.LastErrorCode)
}

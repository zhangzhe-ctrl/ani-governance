//go:build quota_pg

package data

import (
	"context"
	"fmt"
	"sync"
	"testing"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSandboxIntentDoesNotBlockInstanceDelete(t *testing.T) {
	ctx := context.Background()
	c := newLedgerPGClient(t)
	cleanLedger(t, c)
	repo := newTestRepo(c)
	tenant, plan := seedTenantPlan(t, c, "sandbox-independent", 100)
	instance := gpuLedgerInput(t, c.Client(), tenant, plan)
	accepted, err := repo.Occupy(ctx, instance)
	require.NoError(t, err)

	// Retain an unacknowledged historical command while accepting an unrelated
	// new standalone command. Neither creates a deletion dependency.
	legacy := SandboxCreateIntent{TenantID: tenant, ResourceTenantID: instance.ResourceTenantID, InstanceID: accepted.ResourceID, SandboxID: uuid.NewString(), OperationID: uuid.NewString(), IntentDigest: "legacy", Actor: "governance:user:7", Command: []byte(`{}`)}
	_, _, err = repo.RegisterSandboxCreateIntent(ctx, legacy)
	require.NoError(t, err)
	independent := legacy
	independent.InstanceID, independent.SandboxID, independent.OperationID, independent.IntentDigest = "", uuid.NewString(), uuid.NewString(), "independent"
	_, replayed, err := repo.RegisterSandboxCreateIntent(ctx, independent)
	require.NoError(t, err)
	require.False(t, replayed)
	_, replayed, err = repo.RegisterSandboxCreateIntent(ctx, independent)
	require.NoError(t, err)
	require.True(t, replayed)
	changed := independent
	changed.IntentDigest = "changed"
	_, _, err = repo.RegisterSandboxCreateIntent(ctx, changed)
	require.Equal(t, "SANDBOX_IDEMPOTENCY_CONFLICT", kratoserrors.Reason(err))

	_, err = repo.CreateDeleteOperation(ctx, gpuDelete(t, instance))
	require.NoError(t, err, "pending Sandbox operations do not block instance deletion")
	for _, intent := range []SandboxCreateIntent{legacy, independent} {
		stored, err := repo.FindSandboxCreateIntent(ctx, tenant, intent.SandboxID)
		require.NoError(t, err)
		require.NotNil(t, stored)
		require.False(t, stored.Acknowledged, "instance deletion must not acknowledge a Sandbox operation")
	}
}

func TestIndependentSandboxAndInstanceDeleteConcurrent(t *testing.T) {
	ctx := context.Background()
	c := newLedgerPGClient(t)
	cleanLedger(t, c)
	repo := newTestRepo(c)
	tenant, plan := seedTenantPlan(t, c, "sandbox-independent-race", 100)
	template := gpuLedgerInput(t, c.Client(), tenant, plan)
	for n := 0; n < 8; n++ {
		instance := *template
		instance.IdempotencyKey, instance.ResourceID = uuid.NewString(), uuid.NewString()
		_, err := repo.Occupy(ctx, &instance)
		require.NoError(t, err)
		intent := SandboxCreateIntent{TenantID: tenant, ResourceTenantID: instance.ResourceTenantID, SandboxID: uuid.NewString(), OperationID: uuid.NewString(), IntentDigest: fmt.Sprint(n), Actor: "governance:user:7", Command: []byte(`{}`)}
		deletion := gpuDelete(t, &instance)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var createErr, deleteErr error
		wg.Add(2)
		go func() { defer wg.Done(); <-start; _, _, createErr = repo.RegisterSandboxCreateIntent(ctx, intent) }()
		go func() { defer wg.Done(); <-start; _, deleteErr = repo.CreateDeleteOperation(ctx, deletion) }()
		close(start)
		wg.Wait()
		require.NoError(t, createErr)
		require.NoError(t, deleteErr)
	}
}

func TestSandboxCreateDispatchLeasePostgres(t *testing.T) {
	ctx := context.Background()
	c := newLedgerPGClient(t)
	cleanLedger(t, c)
	repo := newTestRepo(c)
	tenant, plan := seedTenantPlan(t, c, "sandbox-dispatch-lease", 100)
	instance := gpuLedgerInput(t, c.Client(), tenant, plan)
	for n := 0; n < 2; n++ {
		intent := SandboxCreateIntent{TenantID: tenant, ResourceTenantID: instance.ResourceTenantID, SandboxID: uuid.NewString(), OperationID: uuid.NewString(), IntentDigest: fmt.Sprint(n), Actor: "governance:user:7", Command: []byte(`{}`)}
		_, _, err := repo.RegisterSandboxCreateIntent(ctx, intent)
		require.NoError(t, err)
	}
	first, err := repo.ClaimSandboxCreate(ctx)
	require.NoError(t, err)
	require.NotNil(t, first)
	second, err := repo.ClaimSandboxCreate(ctx)
	require.NoError(t, err)
	require.NotNil(t, second)
	require.NotEqual(t, first.SandboxID, second.SandboxID)
	third, err := repo.ClaimSandboxCreate(ctx)
	require.NoError(t, err)
	require.Nil(t, third)
	_, err = c.DB().ExecContext(ctx, `UPDATE sys_sandbox_create_intents SET lease_until='-infinity' WHERE tenant_id=$1 AND sandbox_id=$2`, tenant, first.SandboxID)
	require.NoError(t, err)
	recovered, err := repo.ClaimSandboxCreate(ctx)
	require.NoError(t, err)
	require.NotNil(t, recovered)
	require.Equal(t, first.SandboxID, recovered.SandboxID)
	require.Greater(t, recovered.LeaseGeneration, first.LeaseGeneration)
	require.NoError(t, repo.RetrySandboxCreate(ctx, tenant, first.SandboxID, first.LeaseGeneration, "stale"))
	var code string
	var live bool
	require.NoError(t, c.DB().QueryRowContext(ctx, `SELECT last_error_code,lease_until>CURRENT_TIMESTAMP FROM sys_sandbox_create_intents WHERE tenant_id=$1 AND sandbox_id=$2`, tenant, first.SandboxID).Scan(&code, &live))
	require.Empty(t, code)
	require.True(t, live)
	require.NoError(t, repo.RetrySandboxCreate(ctx, tenant, first.SandboxID, recovered.LeaseGeneration, "DeadlineExceeded"))
	require.NoError(t, c.DB().QueryRowContext(ctx, `SELECT last_error_code FROM sys_sandbox_create_intents WHERE tenant_id=$1 AND sandbox_id=$2`, tenant, first.SandboxID).Scan(&code))
	require.Equal(t, "DeadlineExceeded", code)
	stored, err := repo.FindSandboxCreateIntent(ctx, tenant, first.SandboxID)
	require.NoError(t, err)
	require.False(t, stored.Acknowledged, "timeout never proves non-delivery")
	require.NoError(t, repo.AcknowledgeSandboxCreate(ctx, tenant, second.SandboxID, second.OperationID))
}

func TestRejectedSandboxCloneIsTerminalAcrossDispatchRestart(t *testing.T) {
	ctx := context.Background()
	c := newLedgerPGClient(t)
	cleanLedger(t, c)
	repo := newTestRepo(c)
	tenant, _ := seedTenantPlan(t, c, "sandbox-clone-wffc-reject", 100)
	intent := SandboxCreateIntent{TenantID: tenant, ResourceTenantID: uuid.NewString(), SandboxID: uuid.NewString(), OperationID: uuid.NewString(), IntentDigest: "digest", Actor: "governance:user:7", Command: []byte(`{}`), CommandKind: "checkpoint_clone"}
	_, _, err := repo.RegisterSandboxCreateIntent(ctx, intent)
	require.NoError(t, err)
	claimed, err := repo.ClaimSandboxCreate(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.NoError(t, repo.RejectSandboxClone(ctx, tenant, intent.SandboxID, intent.OperationID, "SANDBOX_TRANSFER_WFFC_UNSUPPORTED"))
	require.NoError(t, repo.RejectSandboxClone(ctx, tenant, intent.SandboxID, intent.OperationID, "SANDBOX_TRANSFER_WFFC_UNSUPPORTED"), "duplicate trusted rejection must be idempotent")
	require.NoError(t, repo.RetrySandboxCreate(ctx, tenant, intent.SandboxID, claimed.LeaseGeneration, "late timeout"))
	after, err := repo.FindSandboxCreateIntent(ctx, tenant, intent.SandboxID)
	require.NoError(t, err)
	require.True(t, after.Acknowledged)
	require.Equal(t, "SANDBOX_TRANSFER_WFFC_UNSUPPORTED", after.LastErrorCode)
	require.Error(t, repo.AcknowledgeSandboxCreate(ctx, tenant, intent.SandboxID, intent.OperationID), "late success must not replace rejection")
	restarted := newTestRepo(c)
	next, err := restarted.ClaimSandboxCreate(ctx)
	require.NoError(t, err)
	require.Nil(t, next, "terminal rejection must never be dispatched after restart")
}

func TestCloneCommittedRejectionsUnblockTenantClosePostgres(t *testing.T) {
	c := newLedgerPGClient(t)
	cleanLedger(t, c)
	ctx := context.Background()
	ledger := newTestRepo(c)
	tenantRepo := newTenantRepoForLedgerTest(c)
	for _, reason := range []string{"SANDBOX_CLONE_REJECTED_NOT_FOUND", "SANDBOX_CLONE_REJECTED_VERSION", "SANDBOX_CLONE_REJECTED_CONFLICT"} {
		t.Run(reason, func(t *testing.T) {
			tenant, _ := seedTenantPlan(t, c, reason, 100)
			_, err := c.DB().ExecContext(ctx, `UPDATE sys_tenants SET resource_tenant_id=$2, tenant_space_state='LEGACY',tenant_space_operation_id=NULL WHERE id=$1`, tenant, uuid.NewString())
			require.NoError(t, err)
			intent := SandboxCreateIntent{TenantID: tenant, ResourceTenantID: uuid.NewString(), SandboxID: uuid.NewString(), OperationID: uuid.NewString(), IntentDigest: "digest", Actor: "governance:user:7", Command: []byte(`{}`), CommandKind: "checkpoint_clone"}
			_, _, err = ledger.RegisterSandboxCreateIntent(ctx, intent)
			require.NoError(t, err)
			_, _, err = tenantRepo.RequestTenantSpaceClose(ctx, tenant)
			require.Error(t, err)
			require.NoError(t, ledger.RejectSandboxClone(ctx, tenant, intent.SandboxID, intent.OperationID, reason))
			require.NoError(t, ledger.RejectSandboxClone(ctx, tenant, intent.SandboxID, intent.OperationID, reason))
			require.Error(t, ledger.RejectSandboxClone(ctx, tenant, intent.SandboxID, intent.OperationID, "SANDBOX_CLONE_REJECTED_VERSION-other"))
			require.Error(t, ledger.AcknowledgeSandboxCreate(ctx, tenant, intent.SandboxID, intent.OperationID))
			stored, err := ledger.FindSandboxCreateIntent(ctx, tenant, intent.SandboxID)
			require.NoError(t, err)
			require.True(t, stored.Acknowledged)
			require.Equal(t, reason, stored.LastErrorCode)
			_, closed, err := tenantRepo.RequestTenantSpaceClose(ctx, tenant)
			require.NoError(t, err)
			require.False(t, closed)
		})
	}
}

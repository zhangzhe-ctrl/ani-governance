//go:build quota_pg

package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"entgo.io/ent/dialect"
	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/pkg/audit"
	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"
)

type sandboxMarkerFailDriver struct{ dialect.Driver }
type sandboxMarkerFailTx struct{ dialect.Tx }

func (d sandboxMarkerFailDriver) Tx(ctx context.Context) (dialect.Tx, error) {
	tx, err := d.Driver.Tx(ctx)
	if err != nil {
		return nil, err
	}
	return sandboxMarkerFailTx{tx}, nil
}
func (tx sandboxMarkerFailTx) Exec(ctx context.Context, query string, args, result any) error {
	if strings.Contains(query, "INSERT INTO sys_sandbox_guarded_instance_deletes") {
		return errors.New("injected marker insert failure")
	}
	return tx.Tx.Exec(ctx, query, args, result)
}

func TestSandboxParentFencePostgres(t *testing.T) {
	ctx := context.Background()
	c := newLedgerPGClient(t)
	cleanLedger(t, c)
	repo := newTestRepo(c)
	tenant, plan := seedTenantPlan(t, c, "sandbox-parent-fence", 100)
	parent := gpuLedgerInput(t, c.Client(), tenant, plan)
	accepted, err := repo.Occupy(ctx, parent)
	require.NoError(t, err)
	intent := SandboxCreateIntent{TenantID: tenant, ResourceTenantID: parent.ResourceTenantID, InstanceID: accepted.ResourceID, SandboxID: uuid.NewString(), OperationID: uuid.NewString(), IntentDigest: "intent", Actor: "governance:user:7", Command: []byte(`{}`)}
	_, replayed, err := repo.RegisterSandboxCreateIntent(ctx, intent)
	require.NoError(t, err)
	require.False(t, replayed)
	_, err = repo.BeginSandboxParentDelete(ctx, tenant, accepted.ResourceID)
	require.Equal(t, "INSTANCE_SANDBOX_PENDING", kratoserrors.Reason(err))
	require.NoError(t, repo.AcknowledgeSandboxCreate(ctx, tenant, intent.SandboxID, intent.OperationID))
	token1, err := repo.BeginSandboxParentDelete(ctx, tenant, accepted.ResourceID)
	require.NoError(t, err)
	other := intent
	other.SandboxID = uuid.NewString()
	other.OperationID = uuid.NewString()
	token2, err := repo.BeginSandboxParentDelete(ctx, tenant, accepted.ResourceID)
	require.NoError(t, err)
	require.NotEqual(t, token1, token2)
	require.NoError(t, repo.CancelSandboxParentDelete(ctx, tenant, accepted.ResourceID, token1))
	deletion := gpuDelete(t, parent)
	deletion.SandboxDeleteCheckID = token1
	_, err = repo.CreateDeleteOperation(ctx, deletion)
	require.Equal(t, "INSTANCE_SANDBOX_CHECK_EXPIRED", kratoserrors.Reason(err))
	deletion.SandboxDeleteCheckID = token2
	_, _, err = repo.RegisterSandboxCreateIntent(ctx, other)
	require.NoError(t, err, "new create safely invalidates abandoned or in-flight check")
	_, err = repo.CreateDeleteOperation(ctx, deletion)
	require.Equal(t, "INSTANCE_SANDBOX_CHECK_EXPIRED", kratoserrors.Reason(err))
	require.NoError(t, repo.AcknowledgeSandboxCreate(ctx, tenant, other.SandboxID, other.OperationID))
	token3, err := repo.BeginSandboxParentDelete(ctx, tenant, accepted.ResourceID)
	require.NoError(t, err)
	deletion.SandboxDeleteCheckID = token3
	deleted, err := repo.CreateDeleteOperation(ctx, deletion)
	require.NoError(t, err)
	guarded, err := repo.IsSandboxGuardedInstanceDelete(ctx, tenant, accepted.ResourceID, deleted.OperationID)
	require.NoError(t, err)
	require.True(t, guarded, "preflight marker must commit with the parent delete")
	guarded, err = repo.IsSandboxGuardedInstanceDelete(ctx, tenant, accepted.ResourceID, uuid.NewString())
	require.NoError(t, err)
	require.False(t, guarded, "historical deletes have no marker")
	require.NoError(t, repo.CancelSandboxParentDelete(ctx, tenant, accepted.ResourceID, token3))
	other.SandboxID = uuid.NewString()
	other.OperationID = uuid.NewString()
	_, _, err = repo.RegisterSandboxCreateIntent(ctx, other)
	require.Equal(t, "INSTANCE_DELETING", kratoserrors.Reason(err))
	_, replayed, err = repo.RegisterSandboxCreateIntent(ctx, intent)
	require.NoError(t, err)
	require.True(t, replayed)
	changed := intent
	changed.IntentDigest = "changed"
	_, _, err = repo.RegisterSandboxCreateIntent(ctx, changed)
	require.Equal(t, "SANDBOX_IDEMPOTENCY_CONFLICT", kratoserrors.Reason(err))
}

func TestSandboxParentFencePostgresConcurrent(t *testing.T) {
	ctx := context.Background()
	c := newLedgerPGClient(t)
	cleanLedger(t, c)
	repo := newTestRepo(c)
	tenant, plan := seedTenantPlan(t, c, "sandbox-parent-race", 100)
	template := gpuLedgerInput(t, c.Client(), tenant, plan)
	for n := 0; n < 8; n++ {
		copy := *template
		parent := &copy
		parent.IdempotencyKey = uuid.NewString()
		parent.ResourceID = uuid.NewString()
		accepted, err := repo.Occupy(ctx, parent)
		require.NoError(t, err)
		intent := SandboxCreateIntent{TenantID: tenant, ResourceTenantID: parent.ResourceTenantID, InstanceID: accepted.ResourceID, SandboxID: uuid.NewString(), OperationID: uuid.NewString(), IntentDigest: fmt.Sprint(n), Actor: "governance:user:7", Command: []byte(`{}`)}
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var createErr, deleteErr error
		var token string
		go func() { defer wg.Done(); <-start; _, _, createErr = repo.RegisterSandboxCreateIntent(ctx, intent) }()
		go func() {
			defer wg.Done()
			<-start
			token, deleteErr = repo.BeginSandboxParentDelete(ctx, tenant, accepted.ResourceID)
		}()
		close(start)
		wg.Wait()
		require.NoError(t, createErr)
		if token != "" && deleteErr == nil {
			deletion := gpuDelete(t, parent)
			deletion.SandboxDeleteCheckID = token
			_, err := repo.CreateDeleteOperation(ctx, deletion)
			require.Equal(t, "INSTANCE_SANDBOX_CHECK_EXPIRED", kratoserrors.Reason(err))
			require.NoError(t, repo.CancelSandboxParentDelete(ctx, tenant, accepted.ResourceID, token))
		} else {
			require.Equal(t, "INSTANCE_SANDBOX_PENDING", kratoserrors.Reason(deleteErr))
		}
	}
}

func TestSandboxCreateDispatchLeasePostgres(t *testing.T) {
	ctx := context.Background()
	c := newLedgerPGClient(t)
	cleanLedger(t, c)
	repo := newTestRepo(c)
	tenant, plan := seedTenantPlan(t, c, "sandbox-dispatch-lease", 100)
	parent := gpuLedgerInput(t, c.Client(), tenant, plan)
	accepted, err := repo.Occupy(ctx, parent)
	require.NoError(t, err)
	for n := 0; n < 2; n++ {
		intent := SandboxCreateIntent{TenantID: tenant, ResourceTenantID: parent.ResourceTenantID, InstanceID: accepted.ResourceID, SandboxID: uuid.NewString(), OperationID: uuid.NewString(), IntentDigest: fmt.Sprint(n), Actor: "governance:user:7", Command: []byte(`{}`)}
		_, _, err = repo.RegisterSandboxCreateIntent(ctx, intent)
		require.NoError(t, err)
	}
	// Two independent workers claim different records even if one has stalled.
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
	// Crash simulation: only the scheduling lease expires; the pending intent
	// still prevents parent deletion and can be claimed with a higher generation.
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
	_, err = repo.BeginSandboxParentDelete(ctx, tenant, accepted.ResourceID)
	require.Equal(t, "INSTANCE_SANDBOX_PENDING", kratoserrors.Reason(err))
	require.NoError(t, repo.AcknowledgeSandboxCreate(ctx, tenant, second.SandboxID, second.OperationID))
}

func TestSandboxParentFencePostgresFinalCommitRace(t *testing.T) {
	ctx := context.Background()
	c := newLedgerPGClient(t)
	cleanLedger(t, c)
	repo := newTestRepo(c)
	tenant, plan := seedTenantPlan(t, c, "sandbox-parent-commit-race", 100)
	template := gpuLedgerInput(t, c.Client(), tenant, plan)
	for n := 0; n < 8; n++ {
		copy := *template
		parent := &copy
		parent.IdempotencyKey = uuid.NewString()
		parent.ResourceID = uuid.NewString()
		accepted, err := repo.Occupy(ctx, parent)
		require.NoError(t, err)
		check, err := repo.BeginSandboxParentDelete(ctx, tenant, accepted.ResourceID)
		require.NoError(t, err)
		deletion := gpuDelete(t, parent)
		deletion.SandboxDeleteCheckID = check
		intent := SandboxCreateIntent{TenantID: tenant, ResourceTenantID: parent.ResourceTenantID, InstanceID: accepted.ResourceID, SandboxID: uuid.NewString(), OperationID: uuid.NewString(), IntentDigest: fmt.Sprint(n), Actor: "governance:user:7", Command: []byte(`{}`)}
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var createErr, deleteErr error
		go func() { defer wg.Done(); <-start; _, _, createErr = repo.RegisterSandboxCreateIntent(ctx, intent) }()
		go func() { defer wg.Done(); <-start; _, deleteErr = repo.CreateDeleteOperation(ctx, deletion) }()
		close(start)
		wg.Wait()
		require.NotEqual(t, createErr == nil, deleteErr == nil, "create and parent DELETE cannot both commit")
		if createErr == nil {
			require.Equal(t, "INSTANCE_SANDBOX_CHECK_EXPIRED", kratoserrors.Reason(deleteErr))
		} else {
			require.Equal(t, "INSTANCE_DELETING", kratoserrors.Reason(createErr))
		}
		require.NoError(t, repo.CancelSandboxParentDelete(ctx, tenant, accepted.ResourceID, check))
	}
}

func TestSandboxParentFenceMarkerFailureRollsBackAndAudits(t *testing.T) {
	ctx := context.Background()
	c := newLedgerPGClient(t)
	cleanLedger(t, c)
	setup := newTestRepo(c)
	tenant, plan := seedTenantPlan(t, c, "sandbox-marker-rollback", 100)
	parent := gpuLedgerInput(t, c.Client(), tenant, plan)
	original, err := setup.Occupy(ctx, parent)
	require.NoError(t, err)
	balancesBefore, err := setup.RecomputeInvariants(ctx, tenant)
	require.NoError(t, err)
	chargesBefore, err := setup.GetChargesForOperation(ctx, tenant, original.OperationID)
	require.NoError(t, err)
	// The driver injects failure at the external DB boundary after local cancel
	// and parent DELETE have been written in the same real transaction.
	client := ent.NewClient(ent.Driver(&auditDriver{sandboxMarkerFailDriver{c.Driver()}}))
	wrapped := entCrud.NewEntClient(client, c.Driver())
	repo := newTestRepo(wrapped)
	events := make([]audit.AuditEvent, 0)
	auditedCtx := context.WithValue(ctx, audit.AccumulatorKey(), &events)
	token, err := repo.BeginSandboxParentDelete(auditedCtx, tenant, original.ResourceID)
	require.NoError(t, err)
	foundPreflightWrite := false
	for _, event := range events {
		if event.IsWrite && strings.Contains(event.SqlText, "sys_sandbox_parent_delete_checks") {
			foundPreflightWrite = true
			require.True(t, event.DataMasked)
		}
	}
	require.True(t, foundPreflightWrite, "preflight passes through auditTx")
	deletion := gpuDelete(t, parent)
	deletion.SandboxDeleteCheckID = token
	_, err = repo.CreateDeleteOperation(auditedCtx, deletion)
	require.ErrorContains(t, err, "injected marker insert failure")
	foundFailedMarker := false
	for _, event := range events {
		if event.IsWrite && strings.Contains(event.SqlText, "sys_sandbox_guarded_instance_deletes") {
			foundFailedMarker = true
			require.True(t, event.DataMasked)
		}
	}
	require.True(t, foundFailedMarker, "failed write is also captured, independently of rollback")
	persisted, err := setup.GetOperationForUser(ctx, tenant, original.OperationID)
	require.NoError(t, err)
	require.Equal(t, "QUEUED", string(persisted.DispatchState))
	var deletions, markers int
	require.NoError(t, c.DB().QueryRowContext(ctx, `SELECT count(*) FROM sys_quota_operations WHERE tenant_id=$1 AND create_operation_id=$2`, tenant, original.OperationID).Scan(&deletions))
	require.NoError(t, c.DB().QueryRowContext(ctx, `SELECT count(*) FROM sys_sandbox_guarded_instance_deletes WHERE tenant_id=$1 AND instance_id=$2`, tenant, original.ResourceID).Scan(&markers))
	require.Zero(t, deletions)
	require.Zero(t, markers)
	balancesAfter, err := setup.RecomputeInvariants(ctx, tenant)
	require.NoError(t, err)
	require.Equal(t, balancesBefore, balancesAfter, "failed marker insert rolls back local quota cancellation")
	chargesAfter, err := setup.GetChargesForOperation(ctx, tenant, original.OperationID)
	require.NoError(t, err)
	require.Len(t, chargesAfter, len(chargesBefore))
	for i := range chargesBefore {
		require.Equal(t, chargesBefore[i].ChargeID, chargesAfter[i].ChargeID)
		require.Equal(t, chargesBefore[i].ReleasedUnits, chargesAfter[i].ReleasedUnits)
	}
}

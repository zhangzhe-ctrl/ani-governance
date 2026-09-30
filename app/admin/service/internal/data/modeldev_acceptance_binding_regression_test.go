//go:build modeldev_pg

package data

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	entgo "entgo.io/ent"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01/conformance"
	"go-wind-admin/app/admin/service/internal/data/ent/modeldevacceptance"
	"go-wind-admin/app/admin/service/internal/data/ent/modeldevreleasebinding"
	"go-wind-admin/app/admin/service/internal/data/ent/tenant"
)

func TestModelDevAcceptanceDoesNotBorrowAnotherPresetBinding(t *testing.T) {
	ctx, client, scope, candidate := newModelDevAcceptanceFixture(t)
	other := candidate
	other.Intent.PresetID = uuid.NewString()
	other.Snapshot.Release.PresetID = other.Intent.PresetID
	bindModelDevAcceptanceFixture(t, ctx, client, scope, other)
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
	require.NoError(t, err)
	require.False(t, replayed)
	require.Equal(t, conformance.SnapshotSHA256V1, accepted.ExecutionSpecHash)
	count, err = reader.Client().ModelDevReleaseBinding.Query().Where(modeldevreleasebinding.TenantIDEQ(scope.TenantID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, count, "each preset keeps its own current pointer")
}

func TestModelDevAcceptanceMatchesCanonicalBindingUUIDs(t *testing.T) {
	ctx, client, scope, candidate := newModelDevAcceptanceFixture(t)
	binding := bindModelDevAcceptanceFixture(t, ctx, client, scope, candidate)
	upper := candidate
	upper.Intent.PresetID = strings.ToUpper(candidate.Intent.PresetID)
	upper.Snapshot.Release.PresetID = strings.ToUpper(candidate.Snapshot.Release.PresetID)
	upper.Snapshot.Release.ReleaseID = strings.ToUpper(candidate.Snapshot.Release.ReleaseID)
	requireValidModelDevCandidate(t, scope, upper)
	accepted, replayed, err := NewModelDevAcceptanceRepo(client).AcceptFrozen(ctx, scope, upper)
	require.NoError(t, err)
	require.False(t, replayed)
	require.Equal(t, conformance.SnapshotSHA256V1, accepted.ExecutionSpecHash)
	require.Equal(t, binding.Target.ReleaseID, accepted.Snapshot.Release.ReleaseID)
	require.Equal(t, binding.Scope.PresetID, accepted.Snapshot.Release.PresetID)
	reader := newModelDevPGClient(t)
	actual, replayed, err := NewModelDevAcceptanceRepo(reader).AcceptFrozen(ctx, scope, ModelDevFrozenCandidate{Intent: candidate.Intent})
	require.NoError(t, err)
	require.True(t, replayed)
	require.Equal(t, accepted.OperationID, actual.OperationID)
	require.Equal(t, accepted.ExecutionSpecHash, actual.ExecutionSpecHash)
}

func TestModelDevAcceptanceHoldsCurrentBindingUntilCommit(t *testing.T) {
	baseContext, client, scope, candidate := newModelDevAcceptanceFixture(t)
	binding := bindModelDevAcceptanceFixture(t, baseContext, client, scope, candidate)
	ctx, cancel := context.WithTimeout(baseContext, 10*time.Second)
	entered, release := make(chan struct{}), make(chan struct{})
	var enterOnce, releaseOnce sync.Once
	var workers sync.WaitGroup
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer func() {
		unblock()
		cancel()
		workers.Wait()
	}()
	// Ent's existing mutation hook pauses the real create before its write.
	// It changes neither production code nor the result returned by PostgreSQL.
	client.Client().ModelDevAcceptance.Use(func(next entgo.Mutator) entgo.Mutator {
		return entgo.MutateFunc(func(ctx context.Context, mutation entgo.Mutation) (entgo.Value, error) {
			if mutation.Op().Is(entgo.OpCreate) {
				enterOnce.Do(func() { close(entered) })
				select {
				case <-release:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return next.Mutate(ctx, mutation)
		})
	})
	type admissionOutcome struct {
		accepted *ModelDevAcceptance
		replayed bool
		err      error
	}
	admissions := make(chan admissionOutcome, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		accepted, replayed, err := NewModelDevAcceptanceRepo(client).AcceptFrozen(ctx, scope, candidate)
		admissions <- admissionOutcome{accepted, replayed, err}
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("admission did not reach the guarded persistence boundary")
	}
	reader := newModelDevPGClient(t)
	// NOWAIT observes actual conflicting row locks rather than inferring
	// mutual exclusion from a sleep or from the goroutine's scheduling order.
	_, err := reader.Client().Tenant.Query().Where(tenant.IDEQ(scope.TenantID), tenant.ResourceTenantIDEQ(scope.ResourceTenantID)).ForUpdate(entsql.WithLockAction(entsql.NoWait)).Only(ctx)
	var tenantLockError *pgconn.PgError
	require.ErrorAs(t, err, &tenantLockError)
	require.Equal(t, "55P03", tenantLockError.Code)
	_, err = reader.Client().ModelDevReleaseBinding.Query().Where(
		modeldevreleasebinding.TenantIDEQ(scope.TenantID),
		modeldevreleasebinding.ResourceTenantIDEQ(scope.ResourceTenantID),
		modeldevreleasebinding.PresetIDEQ(binding.Scope.PresetID),
	).ForUpdate(entsql.WithLockAction(entsql.NoWait)).Only(ctx)
	var bindingLockError *pgconn.PgError
	require.ErrorAs(t, err, &bindingLockError)
	require.Equal(t, "55P03", bindingLockError.Code)
	count, err := reader.Client().ModelDevAcceptance.Query().Where(modeldevacceptance.TenantIDEQ(scope.TenantID)).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count, "the held transaction has not committed its acceptance")

	type bindingOutcome struct {
		change *ModelDevReleaseBindingChange
		err    error
	}
	changes := make(chan bindingOutcome, 1)
	started := make(chan struct{})
	workers.Add(1)
	go func() {
		defer workers.Done()
		close(started)
		target := binding.Target
		target.NewSubmissionsEnabled = false
		change, err := NewModelDevReleaseBindingRepo(reader).CompareAndSwap(ctx, binding.Scope, ModelDevReleaseBindingUpdate{
			ExpectedGeneration: binding.Generation, Target: target,
			Actor: "governance:user:8", RequestedAt: binding.UpdatedAt.Add(time.Second),
			Reason: "synthetic concurrent pause", EvidenceReference: "test:cpu-p01-concurrent-pause",
		})
		changes <- bindingOutcome{change, err}
	}()
	<-started
	unblock()
	var admission admissionOutcome
	select {
	case admission = <-admissions:
	case <-ctx.Done():
		t.Fatal("admission did not commit within its bound")
	}
	require.NoError(t, admission.err)
	require.False(t, admission.replayed)
	require.NotNil(t, admission.accepted)
	require.Equal(t, conformance.SnapshotSHA256V1, admission.accepted.ExecutionSpecHash)
	var changed bindingOutcome
	select {
	case changed = <-changes:
	case <-ctx.Done():
		t.Fatal("binding CAS did not finish after admission released its locks")
	}
	require.NoError(t, changed.err)
	require.NotNil(t, changed.change)
	require.False(t, changed.change.Replayed)
	require.Equal(t, binding.Generation+1, changed.change.After.Generation)
	require.False(t, changed.change.After.Target.NewSubmissionsEnabled)
	workers.Wait()

	// The committed old execution remains replayable after the racing pause;
	// a new key resolved against the paused generation still cannot queue.
	actual, replayed, err := NewModelDevAcceptanceRepo(reader).AcceptFrozen(ctx, scope, ModelDevFrozenCandidate{Intent: candidate.Intent})
	require.NoError(t, err)
	require.True(t, replayed)
	require.Equal(t, admission.accepted.OperationID, actual.OperationID)
	nextScope, next := scope, candidate
	nextScope.IdempotencyKey = uuid.NewString()
	next.OperationID, next.ExecutionID = uuid.NewString(), uuid.NewString()
	next.Snapshot.Release.AcceptedBindingGeneration = changed.change.After.Generation
	rejected, replayed, err := NewModelDevAcceptanceRepo(reader).AcceptFrozen(ctx, nextScope, next)
	require.ErrorIs(t, err, ErrModelDevBindingDisabled)
	require.Nil(t, rejected)
	require.False(t, replayed)
	count, err = reader.Client().ModelDevAcceptance.Query().Where(modeldevacceptance.TenantIDEQ(scope.TenantID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

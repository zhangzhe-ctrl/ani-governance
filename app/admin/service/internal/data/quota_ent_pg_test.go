//go:build quota_pg

package data

import (
	"context"
	"errors"
	"testing"

	entgo "entgo.io/ent"
	"github.com/stretchr/testify/require"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/quotaaccount"
	"go-wind-admin/app/admin/service/internal/data/ent/quotaoperation"
	appViewer "go-wind-admin/pkg/entgo/viewer"
)

// A mutation hook must see the real admission writes. Rejecting the second
// charge must roll back the first charge, its account and the operation too.
func TestQuotaEntMutationHookRollback(t *testing.T) {
	c := newLedgerPGClient(t)
	cleanLedger(t, c)
	r := newTestRepo(c)
	r.log = bLogger.NewHelper(bLogger.NewStdLogger())
	tid, pid := seedTenantPlan(t, c, "ent-hook-rollback", 20)
	in := gpuLedgerInput(t, c.Client(), tid, pid)
	calls := 0
	c.Client().QuotaCharge.Use(func(next entgo.Mutator) entgo.Mutator {
		return entgo.MutateFunc(func(ctx context.Context, m entgo.Mutation) (entgo.Value, error) {
			calls++
			if calls == 2 {
				return nil, errors.New("test charge hook rejection")
			}
			return next.Mutate(ctx, m)
		})
	})
	_, err := r.Occupy(context.Background(), in)
	require.Error(t, err)
	require.Equal(t, 2, calls)
	ctx := appViewer.NewSystemViewerContext(context.Background())
	require.Zero(t, c.Client().QuotaOperation.Query().Where(quotaoperation.TenantIDEQ(tid)).CountX(ctx))
	require.Zero(t, c.Client().QuotaAccount.Query().Where(quotaaccount.TenantIDEQ(tid)).CountX(ctx))
}

func TestQuotaEntTransactionHooks(t *testing.T) {
	c := newLedgerPGClient(t)
	cleanLedger(t, c)
	ctx := appViewer.NewSystemViewerContext(context.Background())
	for _, abort := range []bool{false, true} {
		commits, rollbacks := 0, 0
		failure := errors.New("test abort")
		err := quotaTransaction(ctx, c.Client(), func(tx *ent.Tx) error {
			tx.OnCommit(func(next ent.Committer) ent.Committer {
				return ent.CommitFunc(func(ctx context.Context, tx *ent.Tx) error { commits++; return next.Commit(ctx, tx) })
			})
			tx.OnRollback(func(next ent.Rollbacker) ent.Rollbacker {
				return ent.RollbackFunc(func(ctx context.Context, tx *ent.Tx) error { rollbacks++; return next.Rollback(ctx, tx) })
			})
			if abort {
				return failure
			}
			return nil
		})
		if abort {
			require.ErrorIs(t, err, failure)
			require.Equal(t, 0, commits)
			require.Equal(t, 1, rollbacks)
		} else {
			require.NoError(t, err)
			require.Equal(t, 1, commits)
			require.Equal(t, 0, rollbacks)
		}
	}
}

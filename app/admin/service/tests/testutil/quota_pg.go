//go:build quota_pg

package testutil

import (
	"context"
	"database/sql"
	_ "embed"
	"os"
	"testing"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"go-wind-admin/app/admin/service/internal/data/ent"
	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"
)

//go:embed testdata/quota_cleanup.sql
var quotaCleanup string

// NewQuotaPGClient connects to an explicitly migrated, task-isolated PostgreSQL
// database. It never mutates data or schema. The caller owns fixture reset.
func NewQuotaPGClient(t *testing.T) *entCrud.EntClient[*ent.Client] {
	t.Helper()
	dsn := os.Getenv("ANI_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Fatal("ANI_TEST_DATABASE_DSN is required")
	}
	if os.Getenv("ANI_TEST_DATABASE_EXCLUSIVE") != "1" {
		t.Fatal("ANI_TEST_DATABASE_EXCLUSIVE=1 is required for selected quota_pg tests")
	}
	db, e := sql.Open("pgx", dsn)
	require.NoError(t, e)
	drv := entsql.OpenDB("postgres", db)
	client := ent.NewClient(ent.Driver(drv))
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, db.PingContext(context.Background()))
	return entCrud.NewEntClient(client, drv)
}

// ResetQuotaFixture clears only a database whose exclusive ownership has been
// asserted by the test runner. Callers choose when to reset their fixture.
func ResetQuotaFixture(t *testing.T, client *entCrud.EntClient[*ent.Client]) {
	t.Helper()
	if os.Getenv("ANI_TEST_DATABASE_EXCLUSIVE") != "1" {
		t.Fatal("quota fixture reset requires ANI_TEST_DATABASE_EXCLUSIVE=1")
	}
	_, err := client.DB().ExecContext(context.Background(), quotaCleanup)
	require.NoError(t, err)
}

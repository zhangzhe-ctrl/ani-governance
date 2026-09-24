//go:build quota_pg

package enttest

import (
	"context"
	"database/sql"
	_ "embed"
	"os"
	"testing"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	entCrud "github.com/tx7do/go-crud/entgo"
	"go-wind-admin/app/admin/service/internal/data/ent"
)

//go:embed testdata/quota_cleanup.sql
var quotaCleanup string

// NewQuotaPGClient requires an explicitly migrated, task-isolated PostgreSQL
// database. The caller must serialize suites that share that database. This
// helper never creates schema or uses elevated connection credentials.
func NewQuotaPGClient(t *testing.T) *entCrud.EntClient[*ent.Client] {
	t.Helper()
	dsn := os.Getenv("QUOTA_LAB_PG_DSN")
	if dsn == "" {
		t.Fatal("QUOTA_LAB_PG_DSN is required")
	}
	db, e := sql.Open("pgx", dsn)
	require.NoError(t, e)
	t.Cleanup(func() { db.Close() })
	_, e = db.ExecContext(context.Background(), quotaCleanup)
	require.NoError(t, e)
	drv := entsql.OpenDB("postgres", db)
	client := ent.NewClient(ent.Driver(drv))
	return entCrud.NewEntClient(client, drv)
}

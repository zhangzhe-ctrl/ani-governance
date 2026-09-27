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
	_, e = db.ExecContext(context.Background(), quotaCleanup)
	if e != nil {
		_ = db.Close()
		t.Fatalf("reset isolated quota fixture: %v", e)
	}
	drv := entsql.OpenDB("postgres", db)
	client := ent.NewClient(ent.Driver(drv))
	t.Cleanup(func() { _ = client.Close() })
	return entCrud.NewEntClient(client, drv)
}

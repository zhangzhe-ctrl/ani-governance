package data

import (
	entCrud "github.com/tx7do/go-crud/entgo"
	"go-wind-admin/app/admin/service/internal/data/ent"
)

// PostgreSQL tenant and plan writes use Ent row locks. SQLite test fixtures
// rely on their single-writer transaction instead.
func supportsRowLock(c *entCrud.EntClient[*ent.Client]) bool {
	return c.Driver() != nil && c.Driver().Dialect() == "postgres"
}

package data

import (
	"go-wind-admin/app/admin/service/internal/data/ent"
	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"
)

// PostgreSQL tenant and plan writes use Ent row locks. SQLite test fixtures
// rely on their single-writer transaction instead.
func supportsRowLock(c *entCrud.EntClient[*ent.Client]) bool {
	return c.Driver() != nil && c.Driver().Dialect() == "postgres"
}

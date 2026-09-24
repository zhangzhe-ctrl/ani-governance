package data

import (
	entCrud "github.com/tx7do/go-crud/entgo"
	"go-wind-admin/app/admin/service/internal/data/ent"
)

// Unrelated Ent tenant/plan CRUD retains its existing dialect lock handling.
// Quota persistence itself is PostgreSQL-only and always locks via sqlc.
func supportsRowLock(c *entCrud.EntClient[*ent.Client]) bool {
	return c.Driver() != nil && c.Driver().Dialect() == "postgres"
}

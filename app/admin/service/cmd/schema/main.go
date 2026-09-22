// Command schema emits the complete declarative PostgreSQL schema for Atlas.
// It never connects to a database or starts the application.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql/schema"
	"go-wind-admin/app/admin/service/internal/data/ent/migrate"
)

func main() {
	// Ent edges cannot express a tenant-preserving composite foreign key. Keep
	// the extra constraint in this source exporter so future Atlas diffs retain
	// it; never use ent:// directly for Governance migration generation.
	keys, roles := migrate.SysAccessKeysTable, migrate.SysRolesTable
	column := func(table *schema.Table, name string) *schema.Column {
		for _, c := range table.Columns {
			if c.Name == name {
				return c
			}
		}
		panic("missing required schema column: " + table.Name + "." + name)
	}
	keys.ForeignKeys = append(keys.ForeignKeys, &schema.ForeignKey{
		Symbol:     "sys_access_keys_tenant_role_fkey",
		Columns:    []*schema.Column{column(keys, "tenant_id"), column(keys, "role_id")},
		RefTable:   roles,
		RefColumns: []*schema.Column{column(roles, "tenant_id"), column(roles, "id")},
		OnDelete:   schema.Restrict,
	})
	ddl, err := schema.DDL(context.Background(), schema.DDLArgs{Dialect: dialect.Postgres, Version: "16.0.0", Tables: migrate.Tables})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// Ent emits position comments with an empty trailing source location.
	// Normalize whitespace in the exporter so the checked-in SQL is reproducible.
	lines := strings.Split(ddl, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	fmt.Print(strings.Join(lines, "\n"))
}

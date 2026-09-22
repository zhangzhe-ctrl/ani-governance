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

	// 配额账本复合约束：Ent 边无法表达含 tenant_id 的复合外键与跨表引用，
	// 与 AK 同租户角色复合外键一样在此显式补充，保证 Atlas diff 保留。
	accounts := mustTable(migrate.Tables, "sys_quota_accounts")
	operations := mustTable(migrate.Tables, "sys_quota_operations")
	charges := mustTable(migrate.Tables, "sys_quota_charges")
	receipts := mustTable(migrate.Tables, "sys_quota_release_receipts")
	planQuotas := mustTable(migrate.Tables, "sys_plan_quotas")
	definitions := mustTable(migrate.Tables, "sys_quota_definitions")
	tenants := mustTable(migrate.Tables, "sys_tenants")

	// account/operation/charge/receipt 到 tenant 的 FK 使用 RESTRICT：
	// 存在配额账本的租户不得物理删除。
	for _, tbl := range []*schema.Table{accounts, operations, charges, receipts} {
		tbl.ForeignKeys = append(tbl.ForeignKeys, &schema.ForeignKey{
			Symbol:     tbl.Name + "_tenant_fkey",
			Columns:    []*schema.Column{column(tbl, "tenant_id")},
			RefTable:   tenants,
			RefColumns: []*schema.Column{column(tenants, "id")},
			OnDelete:   schema.Restrict,
		})
	}

	// charge 通过 (tenant_id,operation_id) 复合 FK 引用 operation，
	// 通过 (tenant_id,quota_code) 复合 FK 引用 account。
	charges.ForeignKeys = append(charges.ForeignKeys,
		&schema.ForeignKey{
			Symbol:     "sys_quota_charges_tenant_operation_fkey",
			Columns:    []*schema.Column{column(charges, "tenant_id"), column(charges, "operation_id")},
			RefTable:   operations,
			RefColumns: []*schema.Column{column(operations, "tenant_id"), column(operations, "operation_id")},
			OnDelete:   schema.Restrict,
		},
		&schema.ForeignKey{
			Symbol:     "sys_quota_charges_tenant_account_fkey",
			Columns:    []*schema.Column{column(charges, "tenant_id"), column(charges, "quota_code")},
			RefTable:   accounts,
			RefColumns: []*schema.Column{column(accounts, "tenant_id"), column(accounts, "quota_code")},
			OnDelete:   schema.Restrict,
		},
	)

	// DELETE 操作通过 (tenant_id,create_operation_id) 复合 FK 关联原创建操作。
	operations.ForeignKeys = append(operations.ForeignKeys, &schema.ForeignKey{
		Symbol:     "sys_quota_operations_create_operation_fkey",
		Columns:    []*schema.Column{column(operations, "tenant_id"), column(operations, "create_operation_id")},
		RefTable:   operations,
		RefColumns: []*schema.Column{column(operations, "tenant_id"), column(operations, "operation_id")},
		OnDelete:   schema.Restrict,
	})

	// 套餐配额政策的 quota_code 引用目录（code 为目录业务唯一键）。
	planQuotas.ForeignKeys = append(planQuotas.ForeignKeys, &schema.ForeignKey{
		Symbol:     "sys_plan_quotas_quota_code_fkey",
		Columns:    []*schema.Column{column(planQuotas, "quota_code")},
		RefTable:   definitions,
		RefColumns: []*schema.Column{column(definitions, "code")},
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

// mustTable 在导出表中查找指定表，缺失即 panic（导出期失败优于生成不一致的 schema）。
func mustTable(tables []*schema.Table, name string) *schema.Table {
	for _, t := range tables {
		if t.Name == name {
			return t
		}
	}
	panic("missing required schema table: " + name)
}

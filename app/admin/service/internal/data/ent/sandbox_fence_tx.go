package ent

import (
	"context"
	"database/sql"
	"fmt"

	entsql "entgo.io/ent/dialect/sql"
)

// SandboxExec and SandboxScan keep the narrowly scoped SQL adapter on the
// same wrapped Ent transaction, including the data access audit driver.
func (tx *Tx) SandboxExec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	var result sql.Result
	err := tx.config.driver.Exec(ctx, query, args, &result)
	return result, err
}
func (tx *Tx) SandboxScan(ctx context.Context, query string, args []any, values ...any) error {
	var rows entsql.Rows
	if err := tx.config.driver.Query(ctx, query, args, &rows); err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if err := rows.Scan(values...); err != nil {
		return err
	}
	return rows.Err()
}

// RecordSandboxGuardedDelete writes through the existing Ent transaction.
// It deliberately validates the exact preflight token again in the INSERT;
// no database handle or transaction may be borrowed from outside Ent here.
func (tx *Tx) RecordSandboxGuardedDelete(ctx context.Context, tenantID uint32, instanceID, operationID, checkID string) error {
	result, err := tx.SandboxExec(ctx, `INSERT INTO sys_sandbox_guarded_instance_deletes(tenant_id,instance_id,operation_id,check_id)
  SELECT tenant_id,instance_id,$3,check_id FROM sys_sandbox_parent_delete_checks
  WHERE tenant_id=$1 AND instance_id=$2 AND check_id=$4`, tenantID, instanceID, operationID, checkID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("sandbox parent delete check is no longer current")
	}
	return nil
}

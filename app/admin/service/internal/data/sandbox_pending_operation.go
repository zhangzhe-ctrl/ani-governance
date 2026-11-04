package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go-wind-admin/app/admin/service/internal/data/ent"
)

// SandboxPendingOperation is a projection of a durable, unconfirmed Gov
// dispatch intent. It is not an owner acceptance or completion receipt.
type SandboxPendingOperation struct {
	TenantID         uint32
	ResourceTenantID string
	SandboxID        string
	OperationID      string
	CommandKind      string
	CreatedAt        time.Time
}

// FindPendingSandboxOperation requires authorization before invocation. Both
// tenant identities and the operation ID are matched in one database query.
func (r *QuotaLedgerRepo) FindPendingSandboxOperation(ctx context.Context, tenant uint32, resourceTenantID, operationID string) (out *SandboxPendingOperation, err error) {
	if tenant == 0 {
		return nil, fmt.Errorf("sandbox tenant required")
	}
	if _, e := uuid.Parse(resourceTenantID); e != nil {
		return nil, fmt.Errorf("invalid resource tenant identity")
	}
	if _, e := uuid.Parse(operationID); e != nil {
		return nil, fmt.Errorf("invalid sandbox operation identity")
	}
	err = r.transaction(ctx, func(tx *ent.Tx) error {
		row := new(SandboxPendingOperation)
		err := tx.SandboxScan(ctx, `SELECT tenant_id, resource_tenant_id, sandbox_id, operation_id, command_kind, created_at FROM sys_sandbox_create_intents WHERE tenant_id=$1 AND resource_tenant_id=$2 AND operation_id=$3 AND NOT acknowledged`, []any{tenant, resourceTenantID, operationID}, &row.TenantID, &row.ResourceTenantID, &row.SandboxID, &row.OperationID, &row.CommandKind, &row.CreatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		out = row
		return nil
	})
	return out, err
}

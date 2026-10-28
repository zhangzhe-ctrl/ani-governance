package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/google/uuid"
	"go-wind-admin/app/admin/service/internal/commandcontract"
	"go-wind-admin/app/admin/service/internal/data/ent"
	appViewer "go-wind-admin/pkg/entgo/viewer"
)

// SandboxCreateIntent retains a complete frozen owner command. A timeout or
// absent response is never proof that an owner call was not sent.
type SandboxCreateIntent struct {
	TenantID                                                                  uint32
	ResourceTenantID, InstanceID, SandboxID, OperationID, IntentDigest, Actor string
	Command                                                                   []byte
	Acknowledged                                                              bool
	LeaseGeneration                                                           int64
}

// Use the same Ent transaction and tenant lock as quota admission. All SQL is
// executed through the wrapped driver, preserving data-access audit capture.
func (r *QuotaLedgerRepo) sandboxTenantTx(ctx context.Context, tenant uint32, f func(context.Context, *ent.Tx) error) error {
	if tenant == 0 {
		return fmt.Errorf("sandbox tenant required")
	}
	return r.transaction(ctx, func(tx *ent.Tx) error {
		sys := appViewer.NewSystemViewerContext(ctx)
		if _, err := lockQuotaTenant(sys, tx, tenant); err != nil {
			return err
		}
		return f(sys, tx)
	})
}

const sandboxIntentColumns = `tenant_id,resource_tenant_id,instance_id,sandbox_id,operation_id,intent_digest,actor,command,acknowledged`

func sandboxScanIntent(ctx context.Context, tx *ent.Tx, query string, args ...any) (*SandboxCreateIntent, error) {
	i := new(SandboxCreateIntent)
	err := tx.SandboxScan(ctx, query, args, &i.TenantID, &i.ResourceTenantID, &i.InstanceID, &i.SandboxID, &i.OperationID, &i.IntentDigest, &i.Actor, &i.Command, &i.Acknowledged)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return i, err
}

// FindSandboxCreateIntent must be called only after authorizing tenant and parent.
func (r *QuotaLedgerRepo) FindSandboxCreateIntent(ctx context.Context, tenant uint32, id string) (out *SandboxCreateIntent, err error) {
	err = r.transaction(ctx, func(tx *ent.Tx) error {
		out, err = sandboxScanIntent(ctx, tx, `SELECT `+sandboxIntentColumns+` FROM sys_sandbox_create_intents WHERE tenant_id=$1 AND sandbox_id=$2`, tenant, id)
		return err
	})
	return
}

// Registration and parent preflight use the same tenant row lock as the
// final DELETE transaction. A create can invalidate only a read-only check.
func (r *QuotaLedgerRepo) RegisterSandboxCreateIntent(ctx context.Context, in SandboxCreateIntent) (out *SandboxCreateIntent, replay bool, err error) {
	if in.TenantID == 0 || in.ResourceTenantID == "" || in.InstanceID == "" || in.SandboxID == "" || in.OperationID == "" || in.IntentDigest == "" || in.Actor == "" || len(in.Command) == 0 {
		return nil, false, fmt.Errorf("incomplete sandbox intent")
	}
	err = r.sandboxTenantTx(ctx, in.TenantID, func(ctx context.Context, tx *ent.Tx) error {
		old, e := sandboxScanIntent(ctx, tx, `SELECT `+sandboxIntentColumns+` FROM sys_sandbox_create_intents WHERE tenant_id=$1 AND sandbox_id=$2`, in.TenantID, in.SandboxID)
		if e != nil {
			return e
		}
		if old != nil {
			if old.ResourceTenantID != in.ResourceTenantID || old.InstanceID != in.InstanceID || old.OperationID != in.OperationID || old.IntentDigest != in.IntentDigest {
				return kratoserrors.Conflict("SANDBOX_IDEMPOTENCY_CONFLICT", "sandbox create intent changed")
			}
			out, replay = old, true
			return nil
		}
		var parentID string
		e = tx.SandboxScan(ctx, `SELECT operation_id FROM sys_quota_operations WHERE tenant_id=$1 AND resource_tenant_id=$2 AND owner_service=$3 AND action=$4 AND resource_id=$5 AND create_operation_id IS NULL AND deleted_at IS NULL`, []any{in.TenantID, in.ResourceTenantID, commandcontract.InstanceOwner, commandcontract.InstanceCreateAction, in.InstanceID}, &parentID)
		if e != nil {
			return e
		}
		var blocked bool
		e = tx.SandboxScan(ctx, `SELECT EXISTS(SELECT 1 FROM sys_quota_operations WHERE tenant_id=$1 AND owner_service=$2 AND create_operation_id=$3 AND action=$4 AND deleted_at IS NULL)`, []any{in.TenantID, commandcontract.InstanceOwner, parentID, commandcontract.InstanceDeleteAction}, &blocked)
		if e != nil {
			return e
		}
		if blocked {
			return kratoserrors.Conflict("INSTANCE_DELETING", "instance deletion has been accepted")
		}
		// A stale checker cannot commit after this token is invalidated.
		_, e = tx.SandboxExec(ctx, `DELETE FROM sys_sandbox_parent_delete_checks WHERE tenant_id=$1 AND instance_id=$2`, in.TenantID, in.InstanceID)
		if e != nil {
			return e
		}
		_, e = tx.SandboxExec(ctx, `INSERT INTO sys_sandbox_create_intents(tenant_id,resource_tenant_id,instance_id,sandbox_id,operation_id,intent_digest,actor,command) VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb)`, in.TenantID, in.ResourceTenantID, in.InstanceID, in.SandboxID, in.OperationID, in.IntentDigest, in.Actor, string(in.Command))
		if e != nil {
			return e
		}
		out = &in
		return nil
	})
	return
}
func (r *QuotaLedgerRepo) AcknowledgeSandboxCreate(ctx context.Context, tenant uint32, sandboxID, operationID string) error {
	return r.transaction(ctx, func(tx *ent.Tx) error {
		result, err := tx.SandboxExec(ctx, `UPDATE sys_sandbox_create_intents SET acknowledged=true,updated_at=CURRENT_TIMESTAMP WHERE tenant_id=$1 AND sandbox_id=$2 AND operation_id=$3`, tenant, sandboxID, operationID)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("sandbox create intent missing")
		}
		return nil
	})
}

// The scheduling lease never declares a create absent or clears its fence.
func (r *QuotaLedgerRepo) ClaimSandboxCreate(ctx context.Context) (out *SandboxCreateIntent, err error) {
	err = r.transaction(ctx, func(tx *ent.Tx) error {
		i := new(SandboxCreateIntent)
		e := tx.SandboxScan(ctx, `UPDATE sys_sandbox_create_intents SET lease_generation=lease_generation+1,lease_until=CURRENT_TIMESTAMP+interval '90 seconds',updated_at=CURRENT_TIMESTAMP WHERE (tenant_id,sandbox_id)=(SELECT tenant_id,sandbox_id FROM sys_sandbox_create_intents WHERE NOT acknowledged AND retry_at<=CURRENT_TIMESTAMP AND lease_until<=CURRENT_TIMESTAMP ORDER BY retry_at,created_at,tenant_id,sandbox_id FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING `+sandboxIntentColumns+`,lease_generation`, nil, &i.TenantID, &i.ResourceTenantID, &i.InstanceID, &i.SandboxID, &i.OperationID, &i.IntentDigest, &i.Actor, &i.Command, &i.Acknowledged, &i.LeaseGeneration)
		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		out = i
		return nil
	})
	return
}
func (r *QuotaLedgerRepo) RetrySandboxCreate(ctx context.Context, tenant uint32, id string, generation int64, code string) error {
	return r.transaction(ctx, func(tx *ent.Tx) error {
		_, err := tx.SandboxExec(ctx, `UPDATE sys_sandbox_create_intents SET updated_at=CURRENT_TIMESTAMP,retry_at=CURRENT_TIMESTAMP+interval '3 seconds',lease_until='-infinity',last_error_code=$4 WHERE tenant_id=$1 AND sandbox_id=$2 AND lease_generation=$3 AND NOT acknowledged`, tenant, id, generation, code)
		return err
	})
}
func (r *QuotaLedgerRepo) IsSandboxGuardedInstanceDelete(ctx context.Context, tenant uint32, instanceID, operationID string) (guarded bool, err error) {
	err = r.transaction(ctx, func(tx *ent.Tx) error {
		return tx.SandboxScan(ctx, `SELECT EXISTS(SELECT 1 FROM sys_sandbox_guarded_instance_deletes WHERE tenant_id=$1 AND instance_id=$2 AND operation_id=$3)`, []any{tenant, instanceID, operationID}, &guarded)
	})
	return
}

func (r *QuotaLedgerRepo) BeginSandboxParentDelete(ctx context.Context, tenant uint32, instanceID string) (string, error) {
	checkID := uuid.NewString()
	err := r.sandboxTenantTx(ctx, tenant, func(ctx context.Context, tx *ent.Tx) error {
		var pending bool
		err := tx.SandboxScan(ctx, `SELECT EXISTS(SELECT 1 FROM sys_sandbox_create_intents WHERE tenant_id=$1 AND instance_id=$2 AND NOT acknowledged)`, []any{tenant, instanceID}, &pending)
		if err != nil {
			return err
		}
		if pending {
			return kratoserrors.Conflict("INSTANCE_SANDBOX_PENDING", "sandbox creation is pending confirmation")
		}
		// A replacement invalidates the old check. Its final DELETE validates the
		// exact token in the same tenant-locked transaction as acceptance.
		_, err = tx.SandboxExec(ctx, `INSERT INTO sys_sandbox_parent_delete_checks(tenant_id,instance_id,check_id) VALUES($1,$2,$3) ON CONFLICT (tenant_id,instance_id) DO UPDATE SET check_id=EXCLUDED.check_id,created_at=CURRENT_TIMESTAMP`, tenant, instanceID, checkID)
		return err
	})
	return checkID, err
}
func (r *QuotaLedgerRepo) CancelSandboxParentDelete(ctx context.Context, tenant uint32, instanceID, checkID string) error {
	return r.sandboxTenantTx(ctx, tenant, func(ctx context.Context, tx *ent.Tx) error {
		_, err := tx.SandboxExec(ctx, `DELETE FROM sys_sandbox_parent_delete_checks WHERE tenant_id=$1 AND instance_id=$2 AND check_id=$3`, tenant, instanceID, checkID)
		return err
	})
}

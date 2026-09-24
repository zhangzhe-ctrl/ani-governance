-- Read-only safe-pause checks for the task's joint-B fixture database.
-- This is explicit operator evidence, never a runtime query or migration.
SELECT 'gpu_original_charge_mismatch', count(*)
FROM sys_quota_operations o
WHERE o.create_operation_id IS NULL
  AND o.canonical_request::jsonb->>'schema_version'='2'
  AND (
    (SELECT count(*) FROM sys_quota_charges c WHERE c.tenant_id=o.tenant_id AND c.operation_id=o.operation_id)
      <> jsonb_array_length(o.canonical_request::jsonb->'quota_items')
    OR EXISTS (
      SELECT 1 FROM jsonb_array_elements(o.canonical_request::jsonb->'quota_items') AS item
      WHERE NOT EXISTS (
        SELECT 1 FROM sys_quota_charges c
        WHERE c.tenant_id=o.tenant_id AND c.operation_id=o.operation_id
          AND c.quota_code=item->>'quota_code' AND c.original_units=(item->>'units')::bigint
      )
    )
  );
SELECT 'account_charge_imbalance',count(*) FROM sys_quota_accounts a
WHERE a.occupied_units <> COALESCE((SELECT SUM(c.original_units-c.released_units) FROM sys_quota_charges c WHERE c.tenant_id=a.tenant_id AND c.quota_code=a.quota_code),0);
SELECT 'open_other_transactions',count(*) FROM pg_stat_activity
WHERE datname=current_database() AND pid<>pg_backend_pid() AND xact_start IS NOT NULL;
SELECT dispatch_state,count(*) FROM sys_quota_operations GROUP BY dispatch_state ORDER BY dispatch_state;

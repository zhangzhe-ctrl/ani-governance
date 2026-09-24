-- Explicit test fixture preparation after copying the software B identity
-- fixture into a separate formal-process database. Never run on production.
DELETE FROM sys_gpu_delete_acceptances;
DELETE FROM sys_gpu_usage_sync;
DELETE FROM sys_quota_release_receipts;
DELETE FROM sys_quota_charges;
DELETE FROM sys_quota_operations WHERE create_operation_id IS NOT NULL;
DELETE FROM sys_quota_operations;
DELETE FROM sys_quota_accounts;

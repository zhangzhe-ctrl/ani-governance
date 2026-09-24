
-- Deferred until the unique (tenant_id,operation_id) index exists.
ALTER TABLE "sys_quota_operations" ADD CONSTRAINT "sys_quota_operations_create_operation_fkey" FOREIGN KEY ("tenant_id", "create_operation_id") REFERENCES "sys_quota_operations" ("tenant_id", "operation_id") ON DELETE RESTRICT;

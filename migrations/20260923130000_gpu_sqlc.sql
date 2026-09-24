-- Modify "sys_plan_quotas" table
ALTER TABLE "public"."sys_plan_quotas" ALTER COLUMN "quota_value" SET NOT NULL, ALTER COLUMN "plan_id" SET NOT NULL;
-- Set comment to column: "quota_type" on table: "sys_plan_quotas"
COMMENT ON COLUMN "public"."sys_plan_quotas"."quota_type" IS '配额类型（deprecated：仅旧三项兼容投影）';
-- Modify "sys_quota_accounts" table
ALTER TABLE "public"."sys_quota_accounts" ALTER COLUMN "tenant_id" SET NOT NULL;
-- Set comment to column: "id" on table: "sys_quota_accounts"
COMMENT ON COLUMN "public"."sys_quota_accounts"."id" IS 'id';
-- Set comment to column: "created_at" on table: "sys_quota_accounts"
COMMENT ON COLUMN "public"."sys_quota_accounts"."created_at" IS '创建时间';
-- Set comment to column: "updated_at" on table: "sys_quota_accounts"
COMMENT ON COLUMN "public"."sys_quota_accounts"."updated_at" IS '更新时间';
-- Set comment to column: "deleted_at" on table: "sys_quota_accounts"
COMMENT ON COLUMN "public"."sys_quota_accounts"."deleted_at" IS '删除时间';
-- Set comment to column: "tenant_id" on table: "sys_quota_accounts"
COMMENT ON COLUMN "public"."sys_quota_accounts"."tenant_id" IS '租户ID';
-- Set comment to column: "quota_code" on table: "sys_quota_accounts"
COMMENT ON COLUMN "public"."sys_quota_accounts"."quota_code" IS '配额编码';
-- Modify "sys_quota_charges" table
ALTER TABLE "public"."sys_quota_charges" ALTER COLUMN "tenant_id" SET NOT NULL;
-- Set comment to column: "id" on table: "sys_quota_charges"
COMMENT ON COLUMN "public"."sys_quota_charges"."id" IS 'id';
-- Set comment to column: "created_at" on table: "sys_quota_charges"
COMMENT ON COLUMN "public"."sys_quota_charges"."created_at" IS '创建时间';
-- Set comment to column: "updated_at" on table: "sys_quota_charges"
COMMENT ON COLUMN "public"."sys_quota_charges"."updated_at" IS '更新时间';
-- Set comment to column: "deleted_at" on table: "sys_quota_charges"
COMMENT ON COLUMN "public"."sys_quota_charges"."deleted_at" IS '删除时间';
-- Set comment to column: "tenant_id" on table: "sys_quota_charges"
COMMENT ON COLUMN "public"."sys_quota_charges"."tenant_id" IS '租户ID';
-- Set comment to column: "charge_id" on table: "sys_quota_charges"
COMMENT ON COLUMN "public"."sys_quota_charges"."charge_id" IS '占额明细ID（UUID）';
-- Set comment to column: "operation_id" on table: "sys_quota_charges"
COMMENT ON COLUMN "public"."sys_quota_charges"."operation_id" IS '原创建操作ID（UUID）';
-- Set comment to column: "quota_code" on table: "sys_quota_charges"
COMMENT ON COLUMN "public"."sys_quota_charges"."quota_code" IS '配额编码';
-- Set comment to column: "original_units" on table: "sys_quota_charges"
COMMENT ON COLUMN "public"."sys_quota_charges"."original_units" IS '原始占额数量';
-- Set comment to column: "released_units" on table: "sys_quota_charges"
COMMENT ON COLUMN "public"."sys_quota_charges"."released_units" IS '累计已释放数量';
-- Set comment to column: "id" on table: "sys_quota_definitions"
COMMENT ON COLUMN "public"."sys_quota_definitions"."id" IS 'id';
-- Set comment to column: "created_at" on table: "sys_quota_definitions"
COMMENT ON COLUMN "public"."sys_quota_definitions"."created_at" IS '创建时间';
-- Set comment to column: "display_name" on table: "sys_quota_definitions"
COMMENT ON COLUMN "public"."sys_quota_definitions"."display_name" IS '展示名';
-- Set comment to column: "accounting_kind" on table: "sys_quota_definitions"
COMMENT ON COLUMN "public"."sys_quota_definitions"."accounting_kind" IS '计数模型';
-- Drop index "uix_sys_quota_release_receipts_owner_event" from table: "sys_quota_release_receipts"
DROP INDEX "public"."uix_sys_quota_release_receipts_owner_event";
-- Modify "sys_quota_release_receipts" table
ALTER TABLE "public"."sys_quota_release_receipts" ALTER COLUMN "tenant_id" SET NOT NULL;
-- Create index "uix_sys_quota_release_receipts_owner_event" to table: "sys_quota_release_receipts"
CREATE UNIQUE INDEX "uix_sys_quota_release_receipts_owner_event" ON "public"."sys_quota_release_receipts" ("tenant_id", "owner_service", "release_event_id");
-- Set comment to column: "id" on table: "sys_quota_release_receipts"
COMMENT ON COLUMN "public"."sys_quota_release_receipts"."id" IS 'id';
-- Set comment to column: "created_at" on table: "sys_quota_release_receipts"
COMMENT ON COLUMN "public"."sys_quota_release_receipts"."created_at" IS '创建时间';
-- Set comment to column: "tenant_id" on table: "sys_quota_release_receipts"
COMMENT ON COLUMN "public"."sys_quota_release_receipts"."tenant_id" IS '租户ID';
-- Set comment to column: "receipt_id" on table: "sys_quota_release_receipts"
COMMENT ON COLUMN "public"."sys_quota_release_receipts"."receipt_id" IS '回执ID（UUID）';
-- Set comment to column: "owner_service" on table: "sys_quota_release_receipts"
COMMENT ON COLUMN "public"."sys_quota_release_receipts"."owner_service" IS 'owner 服务标识（来自证书精确 SAN）';
-- Set comment to column: "release_event_id" on table: "sys_quota_release_receipts"
COMMENT ON COLUMN "public"."sys_quota_release_receipts"."release_event_id" IS '释放事件ID（UUID，重试不变）';
-- Set comment to column: "payload_hash" on table: "sys_quota_release_receipts"
COMMENT ON COLUMN "public"."sys_quota_release_receipts"."payload_hash" IS '批次负载哈希';
-- Set comment to column: "payload_json" on table: "sys_quota_release_receipts"
COMMENT ON COLUMN "public"."sys_quota_release_receipts"."payload_json" IS '批次负载原文';
-- Create index "uix_sys_tenants_id_resource_tenant" to table: "sys_tenants"
CREATE UNIQUE INDEX "uix_sys_tenants_id_resource_tenant" ON "public"."sys_tenants" ("id", "resource_tenant_id");
-- Modify "sys_quota_operations" table
ALTER TABLE "public"."sys_quota_operations" ALTER COLUMN "tenant_id" SET NOT NULL, ADD
CONSTRAINT "sys_quota_operations_tenant_resource_mapping_fkey" FOREIGN KEY ("tenant_id", "resource_tenant_id") REFERENCES "public"."sys_tenants" ("id", "resource_tenant_id") ON UPDATE NO ACTION ON DELETE RESTRICT;
-- Set comment to column: "id" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."id" IS 'id';
-- Set comment to column: "created_at" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."created_at" IS '创建时间';
-- Set comment to column: "updated_at" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."updated_at" IS '更新时间';
-- Set comment to column: "deleted_at" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."deleted_at" IS '删除时间';
-- Set comment to column: "tenant_id" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."tenant_id" IS '租户ID';
-- Set comment to column: "operation_id" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."operation_id" IS '操作ID（UUID）';
-- Set comment to column: "resource_tenant_id" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."resource_tenant_id" IS '下游资源租户UUID';
-- Set comment to column: "resource_id" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."resource_id" IS '资源ID（UUID）';
-- Set comment to column: "create_operation_id" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."create_operation_id" IS '原创建操作ID（DELETE 必填）';
-- Set comment to column: "actor_type" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."actor_type" IS '操作主体类型（user/access_key）';
-- Set comment to column: "actor_id" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."actor_id" IS '操作主体ID';
-- Set comment to column: "owner_service" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."owner_service" IS 'owner 服务标识（如 ani-gpu-simulator）';
-- Set comment to column: "action" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."action" IS '业务动作（adapter 注册表键）';
-- Set comment to column: "idempotency_key" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."idempotency_key" IS '幂等键（UUID）';
-- Set comment to column: "request_hash" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."request_hash" IS '规范请求哈希';
-- Set comment to column: "canonical_request" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."canonical_request" IS '规范请求（schema_version=1）';
-- Set comment to column: "dispatch_state" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."dispatch_state" IS '投递状态';
-- Set comment to column: "attempt_count" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."attempt_count" IS '已尝试发送次数';
-- Set comment to column: "lease_generation" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."lease_generation" IS '领取代次';
-- Set comment to column: "retry_blocked" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."retry_blocked" IS '永久合同错误暂停自动重试';
-- Set comment to column: "last_error_code" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."last_error_code" IS '最近错误码';
-- Set comment to column: "next_attempt_at" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."next_attempt_at" IS '下次尝试时间（退避）';
-- Set comment to column: "lease_owner" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."lease_owner" IS '当前租约持有者';
-- Set comment to column: "lease_until" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."lease_until" IS '租约到期时间';
-- Set comment to column: "ack_json" on table: "sys_quota_operations"
COMMENT ON COLUMN "public"."sys_quota_operations"."ack_json" IS 'ACK 响应（JSON）';
-- Create "sys_gpu_delete_acceptances" table
CREATE TABLE "public"."sys_gpu_delete_acceptances" (
  "id" bigint NOT NULL GENERATED BY DEFAULT AS IDENTITY,
  "created_at" timestamptz NULL,
  "tenant_id" bigint NOT NULL DEFAULT 0,
  "actor_type" character varying NOT NULL,
  "actor_id" character varying NOT NULL,
  "action" character varying NOT NULL,
  "idempotency_key" character varying NOT NULL,
  "request_hash" character varying NOT NULL,
  "create_operation_id" character varying NOT NULL,
  "delete_operation_id" character varying NOT NULL,
  "result" character varying NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "sys_gpu_delete_acceptances_create_operation_id_fkey" FOREIGN KEY ("tenant_id", "create_operation_id") REFERENCES "public"."sys_quota_operations" ("tenant_id", "operation_id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "sys_gpu_delete_acceptances_delete_operation_id_fkey" FOREIGN KEY ("tenant_id", "delete_operation_id") REFERENCES "public"."sys_quota_operations" ("tenant_id", "operation_id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "sys_gpu_delete_acceptances_result_ck" CHECK ((result)::text = ANY ((ARRAY['LOCAL_CANCELED'::character varying, 'OWNER_DELETE'::character varying])::text[])),
  CONSTRAINT "sys_gpu_delete_acceptances_tenant_positive_ck" CHECK (tenant_id > 0)
);
-- Create index "uix_sys_gpu_delete_acceptances_idempotency" to table: "sys_gpu_delete_acceptances"
CREATE UNIQUE INDEX "uix_sys_gpu_delete_acceptances_idempotency" ON "public"."sys_gpu_delete_acceptances" ("tenant_id", "actor_type", "actor_id", "action", "idempotency_key");
-- Create index "uix_sys_gpu_delete_acceptances_operation" to table: "sys_gpu_delete_acceptances"
CREATE UNIQUE INDEX "uix_sys_gpu_delete_acceptances_operation" ON "public"."sys_gpu_delete_acceptances" ("tenant_id", "delete_operation_id");
-- Create "sys_gpu_usage_sync" table
CREATE TABLE "public"."sys_gpu_usage_sync" (
  "id" bigint NOT NULL GENERATED BY DEFAULT AS IDENTITY,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "deleted_at" timestamptz NULL,
  "tenant_id" bigint NOT NULL DEFAULT 0,
  "operation_id" character varying NOT NULL,
  "revision" bigint NOT NULL DEFAULT 1,
  "state" character varying NOT NULL,
  "payload_json" text NOT NULL,
  "payload_hash" character varying NOT NULL,
  "acked_revision" bigint NOT NULL DEFAULT 0,
  "lease_generation" bigint NOT NULL DEFAULT 0,
  "lease_owner" character varying NULL,
  "lease_until" timestamptz NULL,
  "attempt_count" bigint NOT NULL DEFAULT 0,
  "next_attempt_at" timestamptz NULL,
  "last_error_code" character varying NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "sys_gpu_usage_sync_tenant_operation_fkey" FOREIGN KEY ("tenant_id", "operation_id") REFERENCES "public"."sys_quota_operations" ("tenant_id", "operation_id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "sys_gpu_usage_sync_revision_positive_ck" CHECK ((revision > 0) AND (acked_revision >= 0) AND (acked_revision <= revision)),
  CONSTRAINT "sys_gpu_usage_sync_tenant_positive_ck" CHECK (tenant_id > 0)
);
-- Create index "uix_sys_gpu_usage_sync_tenant_operation" to table: "sys_gpu_usage_sync"
CREATE UNIQUE INDEX "uix_sys_gpu_usage_sync_tenant_operation" ON "public"."sys_gpu_usage_sync" ("tenant_id", "operation_id");

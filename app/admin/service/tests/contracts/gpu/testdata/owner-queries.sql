-- name: EnsureResource
INSERT INTO test_gpu_resources(tenant_id,resource_id,create_operation_id,canonical_request)
VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,resource_id) DO NOTHING;
-- name: LockResource
SELECT create_operation_id::text,canonical_request,closed,create_executed FROM test_gpu_resources WHERE tenant_id=$1 AND resource_id=$2 FOR UPDATE;
-- name: GetCommand
SELECT request_hash,ack_json FROM test_gpu_commands WHERE tenant_id=$1 AND operation_id=$2;
-- name: SaveCommand
INSERT INTO test_gpu_commands(tenant_id,operation_id,resource_id,create_operation_id,request_hash,kind,ack_json) VALUES($1,$2,$3,$4,$5,$6,$7);
-- name: ExecuteCreate
UPDATE test_gpu_resources SET create_executed=true WHERE tenant_id=$1 AND resource_id=$2 AND create_operation_id=$3 AND closed=false;
-- name: CloseResource
UPDATE test_gpu_resources SET closed=true WHERE tenant_id=$1 AND resource_id=$2 AND create_operation_id=$3;
-- name: SaveNotification
INSERT INTO test_gpu_notifications(tenant_id,event_id,resource_id,create_operation_id,payload) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,event_id) DO NOTHING;
-- name: ScanNotifications
SELECT tenant_id::text,event_id::text,payload FROM test_gpu_notifications WHERE acknowledged=false ORDER BY tenant_id,event_id LIMIT 32;
-- name: AckNotification
UPDATE test_gpu_notifications SET acknowledged=true,attempt_count=attempt_count+1 WHERE tenant_id=$1 AND event_id=$2;
-- name: RetryNotification
UPDATE test_gpu_notifications SET attempt_count=attempt_count+1 WHERE tenant_id=$1 AND event_id=$2;
-- name: GetResource
SELECT closed,create_executed FROM test_gpu_resources WHERE tenant_id=$1 AND resource_id=$2;
-- name: GetSyncSnapshot
SELECT payload_json,payload_hash,acked_revision FROM sys_gpu_usage_sync WHERE tenant_id=$1 AND operation_id=$2;
-- name: GetSyncFailureState
SELECT payload_json,payload_hash,acked_revision,attempt_count,retry_blocked,COALESCE(last_error_code,'') FROM sys_gpu_usage_sync WHERE tenant_id=$1 AND operation_id=$2;
-- name: DeleteSyncFixture
DELETE FROM sys_gpu_usage_sync WHERE tenant_id=$1 AND operation_id=$2;
-- name: GetNotification
SELECT acknowledged,attempt_count,payload FROM test_gpu_notifications WHERE tenant_id=$1 AND create_operation_id=$2;
-- name: RemoveChargeFixture
DELETE FROM sys_quota_charges WHERE tenant_id=$1 AND operation_id=$2 AND quota_code=$3 RETURNING row_to_json(sys_quota_charges)::text;
-- name: RestoreChargeFixture
INSERT INTO sys_quota_charges SELECT * FROM json_populate_record(NULL::sys_quota_charges,$1::json);
-- name: HoldDispatchTableLock
LOCK TABLE sys_quota_operations IN ACCESS EXCLUSIVE MODE;
-- name: HasBlockedDispatchReader
SELECT EXISTS(SELECT 1 FROM pg_locks WHERE database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND relation='sys_quota_operations'::regclass AND NOT granted AND mode='AccessShareLock');
-- name: SnapshotRecoveryPolicy
SELECT json_build_object('status',t.status,'expired_at',t.expired_at,'plan_id',t.plan_id,'quota_value',q.quota_value)::text
FROM sys_tenants t JOIN sys_plan_quotas q ON q.plan_id=t.plan_id AND q.quota_code=$2 WHERE t.id=$1;
-- name: RestrictRecoveryTenant
UPDATE sys_tenants SET status='OFF',expired_at=CURRENT_TIMESTAMP-INTERVAL '1 hour' WHERE id=$1;
-- name: RestrictRecoveryQuota
UPDATE sys_plan_quotas SET quota_value=0 WHERE plan_id=(SELECT plan_id FROM sys_tenants WHERE id=$1) AND quota_code=$2;
-- name: RestoreRecoveryTenant
UPDATE sys_tenants SET status=($2::jsonb->>'status'),expired_at=($2::jsonb->>'expired_at')::timestamptz WHERE id=$1;
-- name: RestoreRecoveryQuota
UPDATE sys_plan_quotas SET quota_value=($2::jsonb->>'quota_value')::bigint WHERE plan_id=($2::jsonb->>'plan_id')::bigint AND quota_code=$1;

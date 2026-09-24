-- name: LockTenant :one
SELECT id, status, expired_at, plan_id, resource_tenant_id, now()::timestamptz AS database_now FROM sys_tenants WHERE id=$1 AND id>0 FOR UPDATE;
-- name: GetTenant :one
SELECT id, status, expired_at, plan_id, resource_tenant_id FROM sys_tenants WHERE id=$1 AND id>0;
-- name: LockPlanShared :one
SELECT id FROM sys_plans WHERE id=$1 FOR SHARE;
-- name: LockPlanExclusive :one
SELECT id FROM sys_plans WHERE id=$1 FOR UPDATE;
-- name: ListPlanPolicies :many
SELECT * FROM sys_plan_quotas WHERE plan_id=$1 ORDER BY quota_code;
-- name: GetIdempotentOperation :one
SELECT * FROM sys_quota_operations WHERE tenant_id=$1 AND actor_type=$2 AND actor_id=$3 AND action=$4 AND idempotency_key=$5;
-- name: GetOperation :one
SELECT * FROM sys_quota_operations WHERE tenant_id=$1 AND operation_id=$2;
-- name: LockOperation :one
SELECT * FROM sys_quota_operations WHERE tenant_id=$1 AND operation_id=$2 FOR UPDATE;
-- name: FindCreateOperation :one
SELECT * FROM sys_quota_operations WHERE tenant_id=$1 AND owner_service=$2 AND resource_id=$3 AND create_operation_id IS NULL;
-- name: LocateReleaseOwnerOperation :one
SELECT tenant_id FROM sys_quota_operations WHERE operation_id=$1 AND owner_service=$2 AND create_operation_id IS NULL;
-- name: ListCharges :many
SELECT * FROM sys_quota_charges WHERE tenant_id=$1 AND operation_id=$2 ORDER BY quota_code, charge_id;
-- name: LockCharges :many
SELECT * FROM sys_quota_charges WHERE tenant_id=$1 AND operation_id=$2 ORDER BY quota_code, charge_id FOR UPDATE;
-- name: EnsureAccount :exec
INSERT INTO sys_quota_accounts (tenant_id,quota_code,occupied_units,version,created_at,updated_at) VALUES($1,$2,0,0,now(),now()) ON CONFLICT (tenant_id,quota_code) DO NOTHING;
-- name: LockAccount :one
SELECT * FROM sys_quota_accounts WHERE tenant_id=$1 AND quota_code=$2 FOR UPDATE;
-- name: ChangeAccount :execrows
UPDATE sys_quota_accounts SET occupied_units=occupied_units+sqlc.arg(delta)::bigint,version=version+1,updated_at=now() WHERE tenant_id=$1 AND quota_code=$2 AND occupied_units+sqlc.arg(delta)::bigint>=0;
-- name: InsertOperation :one
INSERT INTO sys_quota_operations (operation_id,tenant_id,resource_tenant_id,resource_id,create_operation_id,actor_type,actor_id,owner_service,action,idempotency_key,request_hash,canonical_request,dispatch_state,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,now(),now()) RETURNING *;
-- name: InsertCharge :one
INSERT INTO sys_quota_charges (charge_id,tenant_id,operation_id,quota_code,original_units,released_units,created_at,updated_at) VALUES($1,$2,$3,$4,$5,0,now(),now()) RETURNING *;
-- name: SetReleasedTotal :execrows
UPDATE sys_quota_charges SET released_units=$3,updated_at=now() WHERE tenant_id=$1 AND charge_id=$2 AND released_units<=$3 AND original_units>=$3;
-- name: GetReleaseReceipt :one
SELECT * FROM sys_quota_release_receipts WHERE tenant_id=$1 AND owner_service=$2 AND release_event_id=$3;
-- name: InsertReleaseReceipt :exec
INSERT INTO sys_quota_release_receipts(receipt_id,tenant_id,owner_service,release_event_id,payload_hash,payload_json,created_at) VALUES($1,$2,$3,$4,$5,$6,now());
-- name: HasDeleteIntent :one
SELECT EXISTS(SELECT 1 FROM sys_quota_operations WHERE tenant_id=$1 AND create_operation_id=$2 AND owner_service=$3);
-- name: SetCanceledUnsent :execrows
UPDATE sys_quota_operations SET dispatch_state='CANCELED_UNSENT',last_error_code='LOCAL_CANCEL',updated_at=now() WHERE tenant_id=$1 AND operation_id=$2 AND dispatch_state='QUEUED' AND attempt_count=0;
-- name: ScanGlobalDispatchCandidates :many
SELECT tenant_id,operation_id FROM sys_quota_operations WHERE retry_blocked=false AND (next_attempt_at IS NULL OR next_attempt_at<=now()) AND (dispatch_state IN ('QUEUED','UNKNOWN') OR (dispatch_state='DISPATCHING' AND lease_until<=now())) ORDER BY created_at,id LIMIT $1;
-- name: ClaimOperation :one
UPDATE sys_quota_operations SET dispatch_state='DISPATCHING',attempt_count=attempt_count+1,lease_generation=lease_generation+1,lease_owner=$3,lease_until=now()+interval '1 microsecond'*sqlc.arg(lease_micros)::bigint,updated_at=now() WHERE tenant_id=$1 AND operation_id=$2 AND retry_blocked=false AND (next_attempt_at IS NULL OR next_attempt_at<=now()) AND (dispatch_state IN ('QUEUED','UNKNOWN') OR (dispatch_state='DISPATCHING' AND lease_until<=now())) RETURNING *;
-- name: AckOperation :execrows
UPDATE sys_quota_operations SET dispatch_state='ACKED',ack_json=$4,updated_at=now() WHERE tenant_id=$1 AND operation_id=$2 AND lease_generation=$3 AND dispatch_state='DISPATCHING';
-- name: MarkOperationUnknown :execrows
UPDATE sys_quota_operations SET dispatch_state='UNKNOWN',next_attempt_at=$4,last_error_code=$5,retry_blocked=$6,updated_at=now() WHERE tenant_id=$1 AND operation_id=$2 AND lease_generation=$3 AND dispatch_state='DISPATCHING';
-- name: ResumeOperation :execrows
UPDATE sys_quota_operations SET retry_blocked=false,next_attempt_at=now(),updated_at=now() WHERE tenant_id=$1 AND operation_id=$2 AND retry_blocked=true;
-- name: RecomputeTenantInvariants :many
SELECT a.tenant_id,a.quota_code,a.occupied_units,COALESCE(SUM(c.original_units-c.released_units),0)::bigint AS charge_remainder FROM sys_quota_accounts a LEFT JOIN sys_quota_charges c ON c.tenant_id=a.tenant_id AND c.quota_code=a.quota_code WHERE a.tenant_id=$1 GROUP BY a.tenant_id,a.quota_code,a.occupied_units ORDER BY a.quota_code;
-- name: ListDefinitions :many
SELECT * FROM sys_quota_definitions ORDER BY code;
-- name: ListAccounts :many
SELECT * FROM sys_quota_accounts WHERE tenant_id=$1 ORDER BY quota_code;
-- name: ScanGlobalGpuCreatePage :many
SELECT tenant_id,operation_id,id FROM sys_quota_operations o WHERE o.id>$1 AND o.create_operation_id IS NULL AND (canonical_request::jsonb->>'schema_version'='2' OR canonical_request::jsonb ? 'gpu_plan' OR EXISTS (SELECT 1 FROM sys_quota_charges c WHERE c.tenant_id=o.tenant_id AND c.operation_id=o.operation_id AND c.quota_code IN ('gpu.physical.count','gpu.shared_memory_mib'))) ORDER BY o.id LIMIT $2;
-- name: GetGpuUsageSync :one
SELECT * FROM sys_gpu_usage_sync WHERE tenant_id=$1 AND operation_id=$2;
-- name: UpsertGpuUsageSync :execrows
INSERT INTO sys_gpu_usage_sync(tenant_id,operation_id,revision,state,payload_json,payload_hash,resource_tenant_id,owner_service,resource_id,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,now(),now()) ON CONFLICT(tenant_id,operation_id) DO UPDATE SET revision=excluded.revision,state=excluded.state,payload_json=excluded.payload_json,payload_hash=excluded.payload_hash,retry_blocked=false,next_attempt_at=NULL,lease_owner=NULL,lease_until=NULL,lease_generation=sys_gpu_usage_sync.lease_generation+1,updated_at=now() WHERE sys_gpu_usage_sync.revision<excluded.revision;
-- name: ScanGlobalGpuSyncCandidates :many
SELECT tenant_id,operation_id FROM sys_gpu_usage_sync WHERE retry_blocked=false AND acked_revision<revision AND (next_attempt_at IS NULL OR next_attempt_at<=now()) AND (lease_until IS NULL OR lease_until<=now()) ORDER BY updated_at,id LIMIT $1;
-- name: ClaimGpuUsageSync :one
UPDATE sys_gpu_usage_sync SET lease_owner=$3,lease_until=now()+interval '1 microsecond'*sqlc.arg(lease_micros)::bigint,lease_generation=lease_generation+1,attempt_count=attempt_count+1,updated_at=now() WHERE tenant_id=$1 AND operation_id=$2 AND retry_blocked=false AND acked_revision<revision AND (next_attempt_at IS NULL OR next_attempt_at<=now()) AND (lease_until IS NULL OR lease_until<=now()) RETURNING *;
-- name: AckGpuUsageSync :execrows
UPDATE sys_gpu_usage_sync SET acked_revision=$3,lease_until=NULL,last_error_code=NULL,updated_at=now() WHERE tenant_id=$1 AND operation_id=$2 AND revision=$3 AND lease_generation=$4 AND lease_until IS NOT NULL AND acked_revision<revision;
-- name: RetryGpuUsageSync :execrows
UPDATE sys_gpu_usage_sync SET lease_until=NULL,next_attempt_at=$5,last_error_code=$6,updated_at=now() WHERE tenant_id=$1 AND operation_id=$2 AND revision=$3 AND lease_generation=$4 AND lease_until IS NOT NULL AND acked_revision<revision;
-- name: InsertGpuDeleteAcceptance :exec
INSERT INTO sys_gpu_delete_acceptances(tenant_id,actor_type,actor_id,action,idempotency_key,request_hash,create_operation_id,delete_operation_id,result,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,now());
-- name: GetGpuDeleteAcceptance :one
SELECT * FROM sys_gpu_delete_acceptances WHERE tenant_id=$1 AND delete_operation_id=$2;

-- name: BlockGpuUsageSync :execrows
UPDATE sys_gpu_usage_sync SET retry_blocked=true,lease_until=NULL,last_error_code=$5,updated_at=now() WHERE tenant_id=$1 AND operation_id=$2 AND revision=$3 AND lease_generation=$4 AND lease_until IS NOT NULL AND acked_revision<revision;

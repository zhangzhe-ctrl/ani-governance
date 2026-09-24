-- name: CreateRole
CREATE ROLE %s LOGIN PASSWORD '%s' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT;
-- name: DropDatabase
DROP DATABASE IF EXISTS %s WITH (FORCE);
-- name: DropRole
DROP ROLE %s;
-- name: CreateDatabase
CREATE DATABASE %s;
-- name: GrantRuntime
REVOKE ALL ON DATABASE %[1]s FROM PUBLIC;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT CONNECT ON DATABASE %[1]s TO %[2]s;
GRANT USAGE ON SCHEMA public TO %[2]s;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO %[2]s;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO %[2]s;
-- name: RuntimeAuthority
SELECT r.rolsuper,r.rolcreatedb,r.rolcreaterole,
 d.datdba=r.oid,has_schema_privilege(current_user,'public','CREATE'),
 has_database_privilege(current_user,current_database(),'TEMP')
FROM pg_roles r JOIN pg_database d ON d.datname=current_database()
WHERE r.rolname=current_user;
-- name: Occupied
SELECT coalesce(sum(occupied_units),0) FROM sys_quota_accounts WHERE quota_code='gpu.count' AND tenant_id=$1;
-- name: FactCount
SELECT count(*) FROM sim_release_facts WHERE charge_id=$1;
-- name: AllocationCount
SELECT count(*) FROM sim_allocations WHERE state='allocated';
-- name: PendingNotifyCount
SELECT count(*) FROM sim_notify_queue WHERE state='pending';
-- name: InsertAcceptedCommand
INSERT INTO sim_commands (operation_id,tenant_id,resource_id,kind,status,actor,request_hash,name,gpu_count,charge_id)
VALUES ($1,$2,$3,'create','accepted','a','h4','n',2,'charge-4');
-- name: InsertProviderOperation
INSERT INTO sim_provider_ops (operation_id,tenant_id,resource_id) VALUES ($1,$2,$3);
-- name: InsertAllocation
INSERT INTO sim_allocations (resource_id,ordinal,tenant_id,operation_id,state) VALUES ($1,$2,$3,$4,'allocated');
-- name: CommandStatus
SELECT status FROM sim_commands WHERE operation_id=$1;
-- name: ReleasedNotificationTotal
SELECT (payload_json::jsonb->'items'->0->>'released_total')::int
FROM sim_notify_queue WHERE charge_id=$1 AND state='pending' ORDER BY created_at DESC LIMIT 1;
-- name: ReleasedChargeUnits
SELECT coalesce(sum(released_units),0) FROM sys_quota_charges WHERE tenant_id=$1 AND charge_id=$2;
-- name: OperationCharge
SELECT charge_id FROM sys_quota_charges WHERE tenant_id=$1 AND operation_id=$2;
-- name: InjectBlockedDispatch
UPDATE sys_quota_operations SET dispatch_state='UNKNOWN',retry_blocked=true,last_error_code='SIMULATOR_CONFLICT'
WHERE tenant_id=$1 AND operation_id=$2;
-- name: BlockedOperationCount
SELECT count(*) FROM sys_quota_operations WHERE tenant_id=$1 AND operation_id=$2 AND retry_blocked;
-- name: CloseProviderOperation
UPDATE sim_provider_ops SET closed=true,execution_generation=execution_generation+1 WHERE operation_id=$1;
-- name: DuplicateFacts
SELECT count(*) FROM (SELECT charge_id,unit_ordinal FROM sim_release_facts GROUP BY charge_id,unit_ordinal HAVING count(*)>1) d;

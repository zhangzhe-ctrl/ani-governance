-- Fedora/operator only; explicit tenant-owned role, stable incremental API registration.
-- Apply to the final schema, then restart Governance and re-login affected users.
-- Existing API/permission IDs are never truncated or renumbered.
BEGIN;
SELECT pg_advisory_xact_lock(71919002);
-- Upstream seeds explicit API IDs without advancing PostgreSQL's sequence.
-- Serialize table writers while reconciling it; never rewrite existing IDs.
LOCK TABLE sys_apis IN SHARE ROW EXCLUSIVE MODE;
SELECT setval(pg_get_serial_sequence('sys_apis','id')::regclass,
 GREATEST(COALESCE((SELECT max(id) FROM sys_apis),0),
 nextval(pg_get_serial_sequence('sys_apis','id')::regclass)));
SELECT set_config('ani.bootstrap_tenant', :'tenant_id', true);
SELECT set_config('ani.bootstrap_role', :'role_id', true);
DO $$
DECLARE
 tid bigint := current_setting('ani.bootstrap_tenant')::bigint;
 rid bigint := current_setting('ani.bootstrap_role')::bigint;
 aid bigint; pid bigint; planid bigint;
BEGIN
 IF tid<=0 OR NOT EXISTS(SELECT 1 FROM sys_roles WHERE id=rid AND tenant_id=tid) THEN
   RAISE EXCEPTION 'explicit tenant-owned role required';
 END IF;
 SELECT plan_id INTO STRICT planid FROM sys_tenants WHERE id=tid AND resource_tenant_id IS NOT NULL;
 IF planid IS NULL THEN RAISE EXCEPTION 'assign an explicit plan before granting Network access'; END IF;
 -- Refuse conflicting registration instead of replacing unrelated inventory.
 IF EXISTS(SELECT 1 FROM sys_apis WHERE path='/api/v1/networks/vpcs/{vpc_id}' AND method='GET'
   AND (module IS DISTINCT FROM 'NetworkService' OR business_module IS DISTINCT FROM 'NETWORK')) THEN
   RAISE EXCEPTION 'conflicting Network route registration';
 END IF;
 INSERT INTO sys_apis(module,business_module,operation,path,method,scope,status,description)
 VALUES('NetworkService','NETWORK','NetworkService_GetVPC','/api/v1/networks/vpcs/{vpc_id}','GET','ADMIN','ON','Get tenant VPC detail')
 ON CONFLICT(module,path,method,scope) DO NOTHING;
 SELECT id INTO STRICT aid FROM sys_apis WHERE path='/api/v1/networks/vpcs/{vpc_id}' AND method='GET' AND business_module='NETWORK';
 INSERT INTO sys_permissions(name,code,status) VALUES('查看VPC详情','network:vpc:get','ON') ON CONFLICT(code) DO NOTHING;
 SELECT id INTO STRICT pid FROM sys_permissions WHERE code='network:vpc:get' AND status='ON';
 INSERT INTO sys_permission_apis(permission_id,api_id) VALUES(pid,aid) ON CONFLICT(permission_id,api_id) DO NOTHING;
 INSERT INTO sys_role_permissions(role_id,permission_id,tenant_id,effect)
 SELECT rid,pid,tid,'ALLOW' WHERE NOT EXISTS(SELECT 1 FROM sys_role_permissions WHERE role_id=rid AND permission_id=pid AND tenant_id=tid);
 -- Module activation is explicit and separate: do not open a shared plan here.
 IF NOT EXISTS(SELECT 1 FROM sys_plan_modules WHERE plan_id=planid AND module='NETWORK') THEN
   RAISE EXCEPTION 'open NETWORK on the chosen plan explicitly before granting access';
 END IF;
END $$;
COMMIT;

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
 aid bigint; pid bigint; planid bigint; r record;
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
 -- GOV-RESOURCE-20260922: tenant read+write surface beyond VPC detail.
 -- Register each route, its permission, and bind to the same tenant role.
 -- Insert order pairs sys_apis rows with their permission codes.
 DROP TABLE IF EXISTS temp_api_permission_pairs;
 CREATE TEMP TABLE temp_api_permission_pairs(module text, operation text, path text, method text, perm_name text, perm_code text, description text) ON COMMIT DROP;
 INSERT INTO temp_api_permission_pairs VALUES
  ('NetworkService','NetworkService_ListVPCs','/api/v1/networks/vpcs','GET','VPC列表','network:vpc:list','List tenant VPCs'),
  ('NetworkService','NetworkService_CreateVPC','/api/v1/networks/vpcs','POST','创建VPC','network:vpc:create','Create a tenant VPC'),
  ('NetworkService','NetworkService_DeleteVPC','/api/v1/networks/vpcs/{vpc_id}','DELETE','删除VPC','network:vpc:delete','Delete a tenant VPC'),
  ('NetworkService','NetworkService_GetOperation','/api/v1/networks/operations/{operation_id}','GET','查询网络操作','network:operation:get','Get one tenant network operation'),
  ('NetworkService','NetworkService_GetEIP','/api/v1/networks/eips/{eip_id}','GET','查看EIP详情','network:eip:get','Get a tenant EIP'),
  ('NetworkService','NetworkService_ListEIPs','/api/v1/networks/eips','GET','EIP列表','network:eip:list','List tenant EIPs'),
  ('NetworkService','NetworkService_CreateEIP','/api/v1/networks/eips','POST','申请EIP','network:eip:create','Allocate a tenant public EIP'),
  ('NetworkService','NetworkService_DeleteEIP','/api/v1/networks/eips/{eip_id}','DELETE','释放EIP','network:eip:delete','Release a tenant EIP'),
  ('NetworkService','NetworkService_GetVPCSnat','/api/v1/networks/vpcs/{vpc_id}/snat','GET','查看SNAT','network:snat:get','Get a VPC SNAT binding'),
  ('NetworkService','NetworkService_BindVPCSnat','/api/v1/networks/vpcs/{vpc_id}/snat/bindings','POST','绑定SNAT','network:snat:bind','Bind an EIP as a VPC SNAT egress');
 FOR r IN SELECT * FROM temp_api_permission_pairs LOOP
   IF EXISTS(SELECT 1 FROM sys_apis WHERE path=r.path AND method=r.method
     AND (module IS DISTINCT FROM r.module OR business_module IS DISTINCT FROM 'NETWORK')) THEN
     RAISE EXCEPTION 'conflicting Network route registration: % %', r.method, r.path;
   END IF;
   INSERT INTO sys_apis(module,business_module,operation,path,method,scope,status,description)
   VALUES(r.module,'NETWORK',r.operation,r.path,r.method,'ADMIN','ON',r.description)
   ON CONFLICT(module,path,method,scope) DO NOTHING;
   SELECT id INTO aid FROM sys_apis WHERE path=r.path AND method=r.method AND business_module='NETWORK';
   IF aid IS NULL THEN RAISE EXCEPTION 'route registration failed: % %', r.method, r.path; END IF;
   INSERT INTO sys_permissions(name,code,status) VALUES(r.perm_name,r.perm_code,'ON') ON CONFLICT(code) DO NOTHING;
   SELECT id INTO pid FROM sys_permissions WHERE code=r.perm_code AND status='ON';
   IF pid IS NULL THEN RAISE EXCEPTION 'permission registration failed: %', r.perm_code; END IF;
   INSERT INTO sys_permission_apis(permission_id,api_id) VALUES(pid,aid) ON CONFLICT(permission_id,api_id) DO NOTHING;
   INSERT INTO sys_role_permissions(role_id,permission_id,tenant_id,effect)
   SELECT rid,pid,tid,'ALLOW' WHERE NOT EXISTS(SELECT 1 FROM sys_role_permissions WHERE role_id=rid AND permission_id=pid AND tenant_id=tid);
   aid := NULL; pid := NULL;
 END LOOP;
END $$;
COMMIT;

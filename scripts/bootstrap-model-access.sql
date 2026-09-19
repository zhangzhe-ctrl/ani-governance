-- psql -v ON_ERROR_STOP=1 -v tenant_id=<trusted tenant primary key>
--      -v role_id=<existing role belonging to that tenant> -f bootstrap-model-access.sql
-- Run after final OpenAPI API inventory seeding. Retains all existing IDs and
-- bindings. Reload authorizer/restart Governance and force affected sessions
-- to re-login after permission changes. No full SyncApis/SyncPermissions.
BEGIN;
SELECT pg_advisory_xact_lock(71919001);
SELECT set_config('ani.bootstrap_tenant', :'tenant_id', true);
SELECT set_config('ani.bootstrap_role', :'role_id', true);
DO $$
DECLARE
  tid bigint := current_setting('ani.bootstrap_tenant')::bigint;
  rid bigint := current_setting('ani.bootstrap_role')::bigint;
  aid bigint;
  pid bigint;
  planid bigint;
BEGIN
  IF tid <= 0 OR NOT EXISTS (SELECT 1 FROM sys_tenants WHERE id=tid AND resource_tenant_id IS NOT NULL)
    OR NOT EXISTS (SELECT 1 FROM sys_roles WHERE id=rid AND tenant_id=tid) THEN
    RAISE EXCEPTION 'explicit tenant and tenant-owned role are required';
  END IF;
  SELECT id INTO STRICT aid FROM sys_apis WHERE path='/api/v1/models' AND method='GET' AND business_module='MODEL';
  INSERT INTO sys_permissions(name,code,status) VALUES ('查看模型目录','model:catalog:list','ON') ON CONFLICT(code) DO NOTHING;
  SELECT id INTO STRICT pid FROM sys_permissions WHERE code='model:catalog:list' AND status='ON';
  INSERT INTO sys_permission_apis(permission_id,api_id) VALUES(pid,aid) ON CONFLICT(permission_id,api_id) DO NOTHING;
  IF NOT EXISTS(SELECT 1 FROM sys_role_permissions WHERE role_id=rid AND permission_id=pid AND tenant_id=tid) THEN
    INSERT INTO sys_role_permissions(role_id,permission_id,tenant_id,effect) VALUES(rid,pid,tid,'ALLOW');
  END IF;
  INSERT INTO sys_plans(name,version,expiry_policy,description) VALUES('ANI Model Catalog','FREE','READONLY','Model catalog read access') ON CONFLICT(name) DO NOTHING;
  SELECT id INTO STRICT planid FROM sys_plans WHERE name='ANI Model Catalog';
  IF NOT EXISTS(SELECT 1 FROM sys_plan_modules WHERE plan_id=planid AND module='MODEL') THEN
    INSERT INTO sys_plan_modules(plan_id,module) VALUES(planid,'MODEL');
  END IF;
  -- An existing plan is retained; an operator must explicitly open MODEL on it.
  UPDATE sys_tenants SET plan_id=planid WHERE id=tid AND plan_id IS NULL;
END $$;
COMMIT;

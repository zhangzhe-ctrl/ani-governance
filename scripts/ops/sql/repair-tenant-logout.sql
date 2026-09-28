-- Explicit data repair for installations initialized before the tenant logout fix.
-- Run with psql -v ON_ERROR_STOP=1 -f sql/patches/20260921_tenant_logout.sql.
-- Atomic and repeatable; never invoked by service startup or admin init.
DO $patch$
DECLARE
  target_permission_id bigint;
  target_api_id bigint;
BEGIN
  PERFORM pg_advisory_xact_lock(718294632);
  -- Missing or duplicate prerequisites must fail instead of silently changing nothing.
  SELECT id INTO STRICT target_permission_id FROM sys_permissions
  WHERE code='sys:tenant_manager';
  SELECT id INTO STRICT target_api_id FROM sys_apis
  WHERE method='POST' AND path='/api/v1/auth/logout';

  INSERT INTO sys_permission_apis(permission_id,api_id,created_at)
  VALUES (target_permission_id,target_api_id,NOW())
  ON CONFLICT (permission_id,api_id) DO NOTHING;
END
$patch$;

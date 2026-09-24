-- Explicit isolated A-layer fixture only. Never a deployment permission grant.
BEGIN;
DO $$
DECLARE aid integer; pid integer; rid integer; uid integer; preflight_role integer;
BEGIN
 IF current_database() <> 'gov_acc_formal' THEN
  RAISE EXCEPTION 'formal BFF fixture requires its isolated database';
 END IF;
 SELECT id INTO STRICT aid FROM sys_apis WHERE path='/admin/v1/quota-definitions' AND method='GET';
 SELECT id INTO STRICT rid FROM sys_roles WHERE code='gov-acc-bff-platform' AND tenant_id=0;
 INSERT INTO sys_permissions(name,code,status) VALUES('formal quota read','formal:quota:read','ON') ON CONFLICT(code) DO NOTHING;
 SELECT id INTO STRICT pid FROM sys_permissions WHERE code='formal:quota:read';
 INSERT INTO sys_permission_apis(permission_id,api_id) VALUES(pid,aid) ON CONFLICT(permission_id,api_id) DO NOTHING;
 INSERT INTO sys_role_permissions(tenant_id,role_id,permission_id,status,effect)
 VALUES(0,rid,pid,'ON','ALLOW') ON CONFLICT(role_id,permission_id) DO NOTHING;
 -- A valid explicit platform fixture satisfies the unchanged read-only
 -- startup preflight. Its random password is discarded after bcrypt hashing.
 INSERT INTO sys_roles(tenant_id,name,code,status,type)
 VALUES(0,'Formal preflight fixture','platform:formal-preflight','ON','SYSTEM') ON CONFLICT DO NOTHING;
 SELECT id INTO STRICT preflight_role FROM sys_roles WHERE tenant_id=0 AND code='platform:formal-preflight';
 INSERT INTO sys_users(tenant_id,username,nickname,status)
 VALUES(0,'formal-preflight','Formal preflight fixture','NORMAL') ON CONFLICT DO NOTHING;
 SELECT id INTO STRICT uid FROM sys_users WHERE tenant_id=0 AND username='formal-preflight';
 IF NOT EXISTS(SELECT 1 FROM sys_user_credentials WHERE tenant_id=0 AND user_id=uid) THEN
  INSERT INTO sys_user_credentials(tenant_id,user_id,identity_type,identifier,credential_type,credential,is_primary,status)
  VALUES(0,uid,'USERNAME','formal-preflight','PASSWORD_HASH','__PASSWORD_HASH__',true,'ENABLED');
 END IF;
 INSERT INTO sys_user_roles(tenant_id,user_id,role_id,is_primary,status)
 VALUES(0,uid,preflight_role,true,'ACTIVE') ON CONFLICT DO NOTHING;
 INSERT INTO sys_permissions(name,code,status) VALUES('Backend fixture','sys:access_backend','ON'),('Formal preflight routes','formal:preflight:read','ON') ON CONFLICT(code) DO NOTHING;
 INSERT INTO sys_role_permissions(tenant_id,role_id,permission_id,status,effect)
 SELECT 0,preflight_role,id,'ON','ALLOW' FROM sys_permissions WHERE code IN ('sys:access_backend','formal:preflight:read') ON CONFLICT DO NOTHING;
 INSERT INTO sys_permission_apis(permission_id,api_id)
 SELECT p.id,a.id FROM sys_permissions p CROSS JOIN sys_apis a
 WHERE p.code='formal:preflight:read' AND a.method='GET' AND a.path IN ('/admin/v1/me','/admin/v1/initial-context','/admin/v1/plans','/admin/v1/tenants') ON CONFLICT DO NOTHING;
END $$;
COMMIT;

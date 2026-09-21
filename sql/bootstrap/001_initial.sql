-- First-install data only. Executed transactionally by `admin init` after Atlas.
-- No DDL, no fixed database IDs, no embedded administrator password.
-- Frozen v1 seed: new releases use separate reviewed data changes, never edit
-- this script to repair an existing installation or grant newly added APIs.
DO $seed$
DECLARE
  administrator_id bigint;
  platform_role_id bigint;
  template_role_id bigint;
  group_id bigint;
  plan_id bigint;
  parent_id bigint;
  m jsonb;
BEGIN
  PERFORM pg_advisory_xact_lock(718294632);
  IF EXISTS (SELECT 1 FROM sys_configs WHERE key='deployment.bootstrap.v1') THEN RETURN; END IF;
  IF EXISTS (SELECT 1 FROM sys_users) OR EXISTS (SELECT 1 FROM sys_roles)
     OR EXISTS (SELECT 1 FROM sys_permissions) OR EXISTS (SELECT 1 FROM sys_permission_groups)
     OR EXISTS (SELECT 1 FROM sys_menus) OR EXISTS (SELECT 1 FROM sys_plans)
     OR EXISTS (SELECT 1 FROM sys_user_credentials) OR EXISTS (SELECT 1 FROM sys_user_roles)
     OR EXISTS (SELECT 1 FROM sys_role_metadata) OR EXISTS (SELECT 1 FROM sys_role_permissions)
     OR EXISTS (SELECT 1 FROM sys_permission_apis) OR EXISTS (SELECT 1 FROM sys_permission_menus)
     OR EXISTS (SELECT 1 FROM sys_plan_modules) OR EXISTS (SELECT 1 FROM sys_tenants)
     OR EXISTS (SELECT 1 FROM sys_configs) OR EXISTS (SELECT 1 FROM sys_languages) THEN
    RAISE EXCEPTION 'initial seed requires an uninitialized database; existing installation must be checked and upgraded explicitly, never re-seeded';
  END IF;
  IF COALESCE(current_setting('ani.bootstrap_username',true),'')='' OR
     COALESCE(current_setting('ani.bootstrap_password_hash',true),'') !~ '^\$2[aby]\$[0-9]{2}\$.{53}$' THEN
    RAISE EXCEPTION 'administrator username and bcrypt password hash are required; use admin init --password-file';
  END IF;

  INSERT INTO sys_permission_groups(name,module,path,status,sort_order,created_at)
  VALUES ('系统权限','sys','/','ON',1,NOW()) RETURNING id INTO group_id;
  UPDATE sys_permission_groups SET path='/'||group_id||'/' WHERE id=group_id;
  INSERT INTO sys_permissions(name,code,group_id,status,created_at) VALUES
    ('访问后台','sys:access_backend',group_id,'ON',NOW()),
    ('平台管理员权限','sys:platform_admin',group_id,'ON',NOW()),
    ('租户管理员权限','sys:tenant_manager',group_id,'ON',NOW()),
    ('管理租户','sys:manage_tenants',group_id,'ON',NOW()),
    ('查看审计日志','sys:audit_logs',group_id,'ON',NOW()),
    ('重置他人密码','sys:reset_others_credential',group_id,'ON',NOW()),
    ('重置他人 MFA','sys:reset_others_mfa',group_id,'ON',NOW());

  INSERT INTO sys_roles(tenant_id,name,code,status,type,is_protected,data_scope,created_at)
  VALUES (0,'平台管理员','platform:admin','ON','SYSTEM',true,'ALL',NOW()) RETURNING id INTO platform_role_id;
  INSERT INTO sys_roles(tenant_id,name,code,status,type,is_protected,data_scope,created_at)
  VALUES (0,'租户管理员模板','template:tenant:manager','ON','TEMPLATE',true,'ALL',NOW()) RETURNING id INTO template_role_id;
  INSERT INTO sys_role_metadata(tenant_id,role_id,is_template,template_for,template_version,scope,sync_policy,custom_overrides,created_at)
  VALUES (0,platform_role_id,false,NULL,1,'PLATFORM','AUTO','{}',NOW()),
         (0,template_role_id,true,'tenant:manager',1,'TENANT','AUTO','{}',NOW());
  INSERT INTO sys_role_permissions(tenant_id,role_id,permission_id,effect,status,created_at)
  SELECT 0,platform_role_id,id,'ALLOW','ON',NOW() FROM sys_permissions
  WHERE code IN ('sys:access_backend','sys:platform_admin','sys:manage_tenants','sys:audit_logs','sys:reset_others_credential','sys:reset_others_mfa');
  INSERT INTO sys_role_permissions(tenant_id,role_id,permission_id,effect,status,created_at)
  SELECT 0,template_role_id,id,'ALLOW','ON',NOW() FROM sys_permissions
  WHERE code IN ('sys:access_backend','sys:tenant_manager');

  -- This initial plan provides login, profile, navigation and user management.
  -- No downstream service entitlement or invented billing/quota configuration.
  INSERT INTO sys_plans(name,version,expiry_policy,description,created_at)
  VALUES ('基础管理','FREE','READONLY','后台入口、个人资料与租户用户管理',NOW()) RETURNING id INTO plan_id;
  INSERT INTO sys_plan_modules(plan_id,module,created_at)
  VALUES (plan_id,'DASHBOARD',NOW()),(plan_id,'OPM',NOW());

  -- Route names are stable seed references; all persistent IDs are allocated by PostgreSQL.
  FOR m IN SELECT value FROM jsonb_array_elements($menus$[
  {"component":"BasicLayout","meta":{"authority":["sys:platform_admin","sys:tenant_manager"],"icon":"lucide:layout-dashboard","order":-1,"title":"page.dashboard.title"},"name":"Dashboard","parent_name":"","path":"/dashboard","status":"ON","type":"CATALOG"},
  {"component":"dashboard/analytics/index.vue","meta":{"affixTab":true,"authority":["sys:platform_admin","sys:tenant_manager"],"icon":"lucide:area-chart","order":-1,"title":"page.dashboard.analytics"},"module":"DASHBOARD","name":"Analytics","parent_name":"Dashboard","path":"/analytics","type":"MENU"},
  {"component":"BasicLayout","meta":{"hideInMenu":true,"title":"menu.profile.settings"},"name":"Profile","parent_name":"","path":"/profile","type":"CATALOG"},
  {"component":"app/opm/user/profile/index.vue","meta":{"hideInMenu":true,"icon":"lucide:user-pen","title":"menu.profile.settings"},"module":"OPM","name":"ProfilePage","parent_name":"Profile","path":"/profile","type":"MENU"},
  {"component":"BasicLayout","meta":{"hideInMenu":true,"title":"menu.profile.internalMessage"},"name":"Inbox","parent_name":"","path":"/inbox","type":"CATALOG"},
  {"component":"app/internal_message/inbox/index.vue","meta":{"hideInMenu":true,"icon":"lucide:message-circle-more","title":"menu.profile.internalMessage"},"module":"INTERNAL_MESSAGE","name":"InboxPage","parent_name":"Inbox","path":"/inbox","type":"MENU"},
  {"component":"BasicLayout","meta":{"authority":["sys:platform_admin"],"icon":"lucide:building-2","order":2000,"title":"menu.tenant.moduleName"},"name":"TenantManagement","parent_name":"","path":"/tenant","redirect":"/tenant/members","type":"CATALOG"},
  {"component":"app/tenant/tenant/index.vue","meta":{"affixTab":true,"authority":["sys:platform_admin"],"icon":"lucide:users","order":1,"title":"menu.tenant.member"},"module":"TENANT","name":"TenantMemberManagement","parent_name":"TenantManagement","path":"members","type":"MENU"},
  {"component":"app/tenant/plan/index.vue","meta":{"authority":["sys:platform_admin"],"icon":"lucide:package","order":2,"title":"menu.tenant.plan"},"module":"TENANT","name":"PlanManagement","parent_name":"TenantManagement","path":"plans","type":"MENU"},
  {"component":"BasicLayout","meta":{"authority":["sys:platform_admin","sys:tenant_manager"],"icon":"lucide:users","keepAlive":true,"order":2001,"title":"menu.opm.moduleName"},"name":"OrganizationalPersonnelManagement","parent_name":"","path":"/opm","redirect":"/opm/users","type":"CATALOG"},
  {"component":"app/opm/org_unit/index.vue","meta":{"authority":["sys:platform_admin","sys:tenant_manager"],"icon":"lucide:layers","order":1,"title":"menu.opm.orgUnit"},"module":"OPM","name":"OrgUnitManagement","parent_name":"OrganizationalPersonnelManagement","path":"org-units","type":"MENU"},
  {"component":"app/opm/position/index.vue","meta":{"authority":["sys:platform_admin","sys:tenant_manager"],"icon":"lucide:briefcase","order":2,"title":"menu.opm.position"},"module":"OPM","name":"PositionManagement","parent_name":"OrganizationalPersonnelManagement","path":"positions","type":"MENU"},
  {"component":"app/opm/user/list/index.vue","meta":{"authority":["sys:platform_admin","sys:tenant_manager"],"icon":"lucide:user","order":3,"title":"menu.opm.user"},"module":"OPM","name":"UserManagement","parent_name":"OrganizationalPersonnelManagement","path":"users","type":"MENU"},
  {"component":"app/opm/user/detail/index.vue","meta":{"authority":["sys:platform_admin","sys:tenant_manager"],"hideInMenu":true,"title":"menu.opm.userDetail"},"module":"OPM","name":"UserDetail","parent_name":"OrganizationalPersonnelManagement","path":"users/detail/:id","type":"MENU"},
  {"component":"BasicLayout","meta":{"authority":["sys:platform_admin","sys:tenant_manager"],"icon":"lucide:shield-check","keepAlive":true,"order":2002,"title":"menu.permission.moduleName"},"name":"PermissionManagement","parent_name":"","path":"/permission","redirect":"/permission/codes","type":"CATALOG"},
  {"component":"app/permission/permission/index.vue","meta":{"authority":["sys:platform_admin"],"icon":"lucide:shield-ellipsis","order":1,"title":"menu.permission.permission"},"module":"PERMISSION","name":"PermissionPointManagement","parent_name":"PermissionManagement","path":"codes","type":"MENU"},
  {"component":"app/permission/role/index.vue","meta":{"authority":["sys:platform_admin","sys:tenant_manager"],"icon":"lucide:shield-user","order":2,"title":"menu.permission.role"},"module":"PERMISSION","name":"RoleManagement","parent_name":"PermissionManagement","path":"roles","type":"MENU"},
  {"component":"app/permission/menu/index.vue","meta":{"authority":["sys:platform_admin"],"icon":"lucide:square-menu","order":1,"title":"menu.permission.menu"},"module":"PERMISSION","name":"MenuManagement","parent_name":"PermissionManagement","path":"menus","type":"MENU"},
  {"component":"app/permission/api/index.vue","meta":{"authority":["sys:platform_admin"],"icon":"lucide:route","order":2,"title":"menu.permission.api"},"module":"PERMISSION","name":"APIManagement","parent_name":"PermissionManagement","path":"apis","type":"MENU"},
  {"component":"BasicLayout","meta":{"authority":["sys:platform_admin","sys:tenant_manager"],"icon":"lucide:mail","keepAlive":true,"order":2003,"title":"menu.internalMessage.moduleName"},"name":"InternalMessageManagement","parent_name":"","path":"/internal-message","redirect":"/internal-message/messages","type":"CATALOG"},
  {"component":"app/internal_message/message/index.vue","meta":{"authority":["sys:platform_admin","sys:tenant_manager"],"icon":"lucide:message-circle-more","order":1,"title":"menu.internalMessage.internalMessage"},"module":"INTERNAL_MESSAGE","name":"InternalMessageList","parent_name":"InternalMessageManagement","path":"messages","type":"MENU"},
  {"component":"app/internal_message/category/index.vue","meta":{"authority":["sys:platform_admin"],"icon":"lucide:calendar-check","order":2,"title":"menu.internalMessage.internalMessageCategory"},"module":"INTERNAL_MESSAGE","name":"InternalMessageCategoryManagement","parent_name":"InternalMessageManagement","path":"categories","type":"MENU"},
  {"component":"BasicLayout","meta":{"authority":["sys:platform_admin"],"icon":"lucide:logs","keepAlive":true,"order":2004,"title":"menu.log.moduleName"},"name":"LogAuditManagement","parent_name":"","path":"/log","redirect":"/log/login-audit-logs","type":"CATALOG"},
  {"component":"app/log/login_audit_log/index.vue","meta":{"authority":["sys:platform_admin"],"icon":"lucide:user-lock","order":1,"title":"menu.log.loginAuditLog"},"module":"LOG","name":"LoginAuditLog","parent_name":"LogAuditManagement","path":"login-audit-logs","type":"MENU"},
  {"component":"app/log/api_audit_log/index.vue","meta":{"authority":["sys:platform_admin"],"icon":"lucide:file-clock","order":2,"title":"menu.log.apiAuditLog"},"module":"LOG","name":"ApiAuditLog","parent_name":"LogAuditManagement","path":"api-audit-logs","type":"MENU"},
  {"component":"app/log/operation_audit_log/index.vue","meta":{"authority":["sys:platform_admin"],"icon":"lucide:shield-ellipsis","order":3,"title":"menu.log.operationAuditLog"},"module":"LOG","name":"OperationAuditLog","parent_name":"LogAuditManagement","path":"operation-audit-logs","type":"MENU"},
  {"component":"app/log/data_access_audit_log/index.vue","meta":{"authority":["sys:platform_admin"],"icon":"lucide:shield-check","order":4,"title":"menu.log.dataAccessAuditLog"},"module":"LOG","name":"DataAccessAuditLog","parent_name":"LogAuditManagement","path":"data-access-audit-logs","type":"MENU"},
  {"component":"app/log/permission_audit_log/index.vue","meta":{"authority":["sys:platform_admin"],"icon":"lucide:shield-alert","order":5,"title":"menu.log.permissionAuditLog"},"module":"LOG","name":"PermissionAuditLog","parent_name":"LogAuditManagement","path":"permission-audit-logs","type":"MENU"},
  {"component":"app/log/policy_evaluation_log/index.vue","meta":{"authority":["sys:platform_admin"],"icon":"lucide:gavel","order":6,"title":"menu.log.policyEvaluationLog"},"module":"LOG","name":"PolicyEvaluationLog","parent_name":"LogAuditManagement","path":"policy-evaluation-logs","type":"MENU"},
  {"component":"app/log/redis_cache_monitor/index.vue","meta":{"authority":["sys:platform_admin"],"icon":"lucide:database","order":7,"title":"menu.log.redisCacheMonitor"},"module":"LOG","name":"RedisCacheMonitor","parent_name":"LogAuditManagement","path":"redis-cache-monitor","type":"MENU"},
  {"component":"BasicLayout","meta":{"authority":["sys:platform_admin","sys:tenant_manager"],"icon":"lucide:settings","keepAlive":true,"order":2005,"title":"menu.system.moduleName"},"name":"System","parent_name":"","path":"/system","redirect":"/system/dict","type":"CATALOG"},
  {"component":"app/system/dict/index.vue","meta":{"authority":["sys:platform_admin"],"icon":"lucide:library-big","order":3,"title":"menu.system.dict"},"module":"SYSTEM","name":"DictManagement","parent_name":"System","path":"dict","type":"MENU"},
  {"component":"app/system/task/index.vue","meta":{"authority":["sys:platform_admin","sys:tenant_manager"],"icon":"lucide:list-todo","order":5,"title":"menu.system.task"},"module":"SYSTEM","name":"TaskManagement","parent_name":"System","path":"tasks","type":"MENU"},
  {"component":"app/system/login_policy/index.vue","meta":{"authority":["sys:platform_admin"],"icon":"lucide:shield-x","order":6,"title":"menu.system.loginPolicy"},"module":"SYSTEM","name":"LoginPolicyManagement","parent_name":"System","path":"login-policies","type":"MENU"},
  {"component":"app/system/language/index.vue","meta":{"authority":["sys:platform_admin"],"icon":"lucide:globe","order":7,"title":"menu.system.language"},"module":"SYSTEM","name":"LanguageManagement","parent_name":"System","path":"languages","type":"MENU"},
  {"component":"app/system/access_key/index.vue","meta":{"authority":["sys:platform_admin"],"icon":"lucide:key-round","order":9,"title":"menu.system.accessKeys"},"module":"SYSTEM","name":"AccessKeyManagement","parent_name":"System","path":"access-keys","type":"MENU"},
  {"component":"app/system/config/index.vue","meta":{"authority":["sys:platform_admin"],"icon":"lucide:sliders-horizontal","order":8,"title":"menu.system.config"},"module":"SYSTEM","name":"ConfigManagement","parent_name":"System","path":"configs","type":"MENU"},
  {"component":"app/system/notification_channel/index.vue","meta":{"authority":["sys:platform_admin"],"icon":"lucide:mail","order":10,"title":"menu.system.notificationChannels"},"module":"SYSTEM","name":"NotificationChannelManagement","parent_name":"System","path":"notification-channels","type":"MENU"},
  {"component":"app/system/online_session/index.vue","meta":{"authority":["sys:platform_admin"],"icon":"lucide:monitor","order":8,"title":"menu.system.onlineSessions"},"module":"SYSTEM","name":"OnlineSessionManagement","parent_name":"System","path":"online-sessions","type":"MENU"},
  {"component":"app/system/server_monitor/index.vue","meta":{"authority":["sys:platform_admin"],"icon":"lucide:activity","order":9,"title":"menu.system.serverMonitor"},"module":"SYSTEM","name":"ServerMonitor","parent_name":"System","path":"server-monitor","type":"MENU"}
  ]$menus$::jsonb) LOOP
    parent_id := NULL;
    IF m->>'parent_name' <> '' THEN
      SELECT id INTO STRICT parent_id FROM sys_menus WHERE name=m->>'parent_name';
    END IF;
    INSERT INTO sys_menus(name,parent_id,path,component,type,status,module,meta,created_at)
    VALUES (m->>'name',parent_id,m->>'path',m->>'component',COALESCE(m->>'type','MENU'),'ON',m->>'module',m->'meta',NOW());
  END LOOP;
  INSERT INTO sys_permission_menus(permission_id,menu_id,created_at)
  SELECT p.id,m.id,NOW() FROM sys_permissions p CROSS JOIN sys_menus m WHERE p.code='sys:platform_admin';
  INSERT INTO sys_permission_menus(permission_id,menu_id,created_at)
  SELECT p.id,m.id,NOW() FROM sys_permissions p CROSS JOIN sys_menus m WHERE p.code='sys:tenant_manager'
  AND m.name IN ('Dashboard','Analytics','Profile','ProfilePage','OrganizationalPersonnelManagement','UserManagement','UserDetail');

  -- Frozen API grants by (method,path), not by insertion order or numeric IDs.
  -- Missing catalog entries cause rollback rather than a silently incomplete seed.
  IF EXISTS (SELECT 1 FROM (VALUES
  ('DELETE','/admin/v1/access-keys/{id}'),
  ('DELETE','/admin/v1/apis/{id}'),
  ('DELETE','/admin/v1/configs/{id}'),
  ('DELETE','/admin/v1/dict/entries'),
  ('DELETE','/admin/v1/dict/langs'),
  ('DELETE','/admin/v1/dict/types'),
  ('DELETE','/admin/v1/internal-message/categories/{id}'),
  ('DELETE','/admin/v1/internal-message/messages/{id}'),
  ('DELETE','/admin/v1/login-policies/{id}'),
  ('DELETE','/admin/v1/menus/{id}'),
  ('DELETE','/admin/v1/mfa/{credentialId}'),
  ('DELETE','/admin/v1/notification-channels/{id}'),
  ('DELETE','/admin/v1/org-units/{id}'),
  ('DELETE','/admin/v1/permission-groups/{id}'),
  ('DELETE','/admin/v1/permissions/{id}'),
  ('DELETE','/admin/v1/plan-modules'),
  ('DELETE','/admin/v1/plan-quotas'),
  ('DELETE','/admin/v1/plans'),
  ('DELETE','/admin/v1/positions/{id}'),
  ('DELETE','/admin/v1/roles/{id}'),
  ('DELETE','/admin/v1/tasks/{id}'),
  ('DELETE','/admin/v1/tenants/{id}'),
  ('DELETE','/admin/v1/users/username/{username}'),
  ('DELETE','/admin/v1/users/{id}'),
  ('GET','/admin/v1/access-keys'),
  ('GET','/admin/v1/access-keys/{id}'),
  ('GET','/admin/v1/api-audit-logs'),
  ('GET','/admin/v1/api-audit-logs/{id}'),
  ('GET','/admin/v1/apis'),
  ('GET','/admin/v1/apis/walk-route'),
  ('GET','/admin/v1/apis/{id}'),
  ('GET','/admin/v1/configs'),
  ('GET','/admin/v1/configs/{id}'),
  ('GET','/admin/v1/dashboard/login-status-distribution'),
  ('GET','/admin/v1/dashboard/login-trend'),
  ('GET','/admin/v1/dashboard/operation-action-distribution'),
  ('GET','/admin/v1/dashboard/overview'),
  ('GET','/admin/v1/data-access-audit-logs'),
  ('GET','/admin/v1/data-access-audit-logs/{id}'),
  ('GET','/admin/v1/dict/entries'),
  ('GET','/admin/v1/dict/entries/by-type-code'),
  ('GET','/admin/v1/dict/langs'),
  ('GET','/admin/v1/dict/langs/{id}'),
  ('GET','/admin/v1/dict/types'),
  ('GET','/admin/v1/dict/types/code/{code}'),
  ('GET','/admin/v1/dict/types/{id}'),
  ('GET','/admin/v1/initial-context'),
  ('GET','/admin/v1/internal-message/categories'),
  ('GET','/admin/v1/internal-message/categories/{id}'),
  ('GET','/admin/v1/internal-message/inbox'),
  ('GET','/admin/v1/internal-message/messages'),
  ('GET','/admin/v1/internal-message/messages/{id}'),
  ('GET','/admin/v1/login-audit-logs'),
  ('GET','/admin/v1/login-audit-logs/{id}'),
  ('GET','/admin/v1/login-policies'),
  ('GET','/admin/v1/login-policies/{id}'),
  ('GET','/admin/v1/me'),
  ('GET','/admin/v1/menus'),
  ('GET','/admin/v1/menus/{id}'),
  ('GET','/admin/v1/mfa/methods'),
  ('GET','/admin/v1/mfa/status'),
  ('GET','/admin/v1/notification-channels'),
  ('GET','/admin/v1/notification-channels/{id}'),
  ('GET','/admin/v1/online-session/my-sessions'),
  ('GET','/admin/v1/online-session/sessions'),
  ('GET','/admin/v1/operation-audit-logs'),
  ('GET','/admin/v1/operation-audit-logs/{id}'),
  ('GET','/admin/v1/org-units'),
  ('GET','/admin/v1/org-units/{id}'),
  ('GET','/admin/v1/perm-codes'),
  ('GET','/admin/v1/permission-audit-logs'),
  ('GET','/admin/v1/permission-audit-logs/{id}'),
  ('GET','/admin/v1/permission-groups'),
  ('GET','/admin/v1/permission-groups/{id}'),
  ('GET','/admin/v1/permissions'),
  ('GET','/admin/v1/permissions/{id}'),
  ('GET','/admin/v1/plan-modules'),
  ('GET','/admin/v1/plan-modules/{id}'),
  ('GET','/admin/v1/plan-quotas'),
  ('GET','/admin/v1/plans'),
  ('GET','/admin/v1/plans/{id}'),
  ('GET','/admin/v1/policy-evaluation-logs'),
  ('GET','/admin/v1/policy-evaluation-logs/{id}'),
  ('GET','/admin/v1/positions'),
  ('GET','/admin/v1/positions/{id}'),
  ('GET','/admin/v1/redis-cache-monitor'),
  ('GET','/admin/v1/roles'),
  ('GET','/admin/v1/roles/{id}'),
  ('GET','/admin/v1/routes'),
  ('GET','/admin/v1/server-monitor'),
  ('GET','/admin/v1/tasks'),
  ('GET','/admin/v1/tasks/type-name/{typeName}'),
  ('GET','/admin/v1/tasks/{id}'),
  ('GET','/admin/v1/tasks:type-names'),
  ('GET','/admin/v1/tenants'),
  ('GET','/admin/v1/tenants/{id}'),
  ('GET','/admin/v1/tenants/{id}/usage'),
  ('GET','/admin/v1/tenants:exists'),
  ('GET','/admin/v1/users'),
  ('GET','/admin/v1/users/username/{username}'),
  ('GET','/admin/v1/users/{id}'),
  ('GET','/admin/v1/users:exists'),
  ('GET','/api/v1/auth/captcha'),
  ('GET','/api/v1/networks/vpcs/{vpc_id}'),
  ('POST','/admin/v1/access-keys'),
  ('POST','/admin/v1/access-keys/token'),
  ('POST','/admin/v1/apis'),
  ('POST','/admin/v1/apis/sync'),
  ('POST','/admin/v1/configs'),
  ('POST','/admin/v1/dict/entries'),
  ('POST','/admin/v1/dict/langs'),
  ('POST','/admin/v1/dict/langs/batch'),
  ('POST','/admin/v1/dict/types'),
  ('POST','/admin/v1/internal-message/categories'),
  ('POST','/admin/v1/internal-message/inbox/delete'),
  ('POST','/admin/v1/internal-message/read'),
  ('POST','/admin/v1/internal-message/revoke'),
  ('POST','/admin/v1/internal-message/send'),
  ('POST','/admin/v1/internal-message/status'),
  ('POST','/admin/v1/login-policies'),
  ('POST','/admin/v1/me/contact'),
  ('POST','/admin/v1/me/contact/verify'),
  ('POST','/admin/v1/me/password'),
  ('POST','/admin/v1/menus'),
  ('POST','/admin/v1/menus/sync'),
  ('POST','/admin/v1/mfa/disable'),
  ('POST','/admin/v1/mfa/enroll/confirm'),
  ('POST','/admin/v1/mfa/enroll/start'),
  ('POST','/admin/v1/mfa/verify'),
  ('POST','/admin/v1/notification-channels'),
  ('POST','/admin/v1/notification-channels/{id}/send-test-email'),
  ('POST','/admin/v1/online-session/force-logout'),
  ('POST','/admin/v1/online-session/my-sessions/revoke'),
  ('POST','/admin/v1/org-units'),
  ('POST','/admin/v1/permission-groups'),
  ('POST','/admin/v1/permissions'),
  ('POST','/admin/v1/permissions/sync:perms'),
  ('POST','/admin/v1/plan-modules'),
  ('POST','/admin/v1/plan-quotas'),
  ('POST','/admin/v1/plans'),
  ('POST','/admin/v1/positions'),
  ('POST','/admin/v1/roles'),
  ('POST','/admin/v1/tasks'),
  ('POST','/admin/v1/tasks:control'),
  ('POST','/admin/v1/tasks:restart'),
  ('POST','/admin/v1/tasks:start'),
  ('POST','/admin/v1/tasks:stop'),
  ('POST','/admin/v1/tenants'),
  ('POST','/admin/v1/tenants/{id}/cleanup'),
  ('POST','/admin/v1/tenants:with-admin'),
  ('POST','/admin/v1/users'),
  ('POST','/admin/v1/users/{userId}/password'),
  ('POST','/api/v1/auth/captcha/verify'),
  ('POST','/api/v1/auth/forgot-password'),
  ('POST','/api/v1/auth/invitations/accept'),
  ('POST','/api/v1/auth/logout'),
  ('POST','/api/v1/auth/password/login'),
  ('POST','/api/v1/auth/platform/password/login'),
  ('POST','/api/v1/auth/refresh'),
  ('POST','/api/v1/auth/reset-password-by-code'),
  ('PUT','/admin/v1/access-keys/{id}'),
  ('PUT','/admin/v1/access-keys/{id}/secret'),
  ('PUT','/admin/v1/apis/{id}'),
  ('PUT','/admin/v1/configs/{id}'),
  ('PUT','/admin/v1/dict/entries/{id}'),
  ('PUT','/admin/v1/dict/langs/{id}'),
  ('PUT','/admin/v1/dict/types/{id}'),
  ('PUT','/admin/v1/internal-message/categories/{id}'),
  ('PUT','/admin/v1/internal-message/messages/{id}'),
  ('PUT','/admin/v1/login-policies/{id}'),
  ('PUT','/admin/v1/me'),
  ('PUT','/admin/v1/menus/{id}'),
  ('PUT','/admin/v1/notification-channels/{id}'),
  ('PUT','/admin/v1/org-units/{id}'),
  ('PUT','/admin/v1/permission-groups/{id}'),
  ('PUT','/admin/v1/permissions/{id}'),
  ('PUT','/admin/v1/plan-modules/{id}'),
  ('PUT','/admin/v1/plan-quotas/{id}'),
  ('PUT','/admin/v1/plans/{id}'),
  ('PUT','/admin/v1/positions/{id}'),
  ('PUT','/admin/v1/roles/{id}'),
  ('PUT','/admin/v1/tasks/{id}'),
  ('PUT','/admin/v1/tenants/{id}'),
  ('PUT','/admin/v1/users/{id}')
  ) required(method,path) WHERE NOT EXISTS (SELECT 1 FROM sys_apis a WHERE a.method=required.method AND a.path=required.path)) THEN
    RAISE EXCEPTION 'API catalog is incomplete; register the OpenAPI shipped with this seed before initialization';
  END IF;
  INSERT INTO sys_permission_apis(permission_id,api_id,created_at)
  SELECT p.id,a.id,NOW() FROM sys_permissions p CROSS JOIN sys_apis a
  JOIN (VALUES
  ('DELETE','/admin/v1/access-keys/{id}'),
  ('DELETE','/admin/v1/apis/{id}'),
  ('DELETE','/admin/v1/configs/{id}'),
  ('DELETE','/admin/v1/dict/entries'),
  ('DELETE','/admin/v1/dict/langs'),
  ('DELETE','/admin/v1/dict/types'),
  ('DELETE','/admin/v1/internal-message/categories/{id}'),
  ('DELETE','/admin/v1/internal-message/messages/{id}'),
  ('DELETE','/admin/v1/login-policies/{id}'),
  ('DELETE','/admin/v1/menus/{id}'),
  ('DELETE','/admin/v1/mfa/{credentialId}'),
  ('DELETE','/admin/v1/notification-channels/{id}'),
  ('DELETE','/admin/v1/org-units/{id}'),
  ('DELETE','/admin/v1/permission-groups/{id}'),
  ('DELETE','/admin/v1/permissions/{id}'),
  ('DELETE','/admin/v1/plan-modules'),
  ('DELETE','/admin/v1/plan-quotas'),
  ('DELETE','/admin/v1/plans'),
  ('DELETE','/admin/v1/positions/{id}'),
  ('DELETE','/admin/v1/roles/{id}'),
  ('DELETE','/admin/v1/tasks/{id}'),
  ('DELETE','/admin/v1/tenants/{id}'),
  ('DELETE','/admin/v1/users/username/{username}'),
  ('DELETE','/admin/v1/users/{id}'),
  ('GET','/admin/v1/access-keys'),
  ('GET','/admin/v1/access-keys/{id}'),
  ('GET','/admin/v1/api-audit-logs'),
  ('GET','/admin/v1/api-audit-logs/{id}'),
  ('GET','/admin/v1/apis'),
  ('GET','/admin/v1/apis/walk-route'),
  ('GET','/admin/v1/apis/{id}'),
  ('GET','/admin/v1/configs'),
  ('GET','/admin/v1/configs/{id}'),
  ('GET','/admin/v1/dashboard/login-status-distribution'),
  ('GET','/admin/v1/dashboard/login-trend'),
  ('GET','/admin/v1/dashboard/operation-action-distribution'),
  ('GET','/admin/v1/dashboard/overview'),
  ('GET','/admin/v1/data-access-audit-logs'),
  ('GET','/admin/v1/data-access-audit-logs/{id}'),
  ('GET','/admin/v1/dict/entries'),
  ('GET','/admin/v1/dict/entries/by-type-code'),
  ('GET','/admin/v1/dict/langs'),
  ('GET','/admin/v1/dict/langs/{id}'),
  ('GET','/admin/v1/dict/types'),
  ('GET','/admin/v1/dict/types/code/{code}'),
  ('GET','/admin/v1/dict/types/{id}'),
  ('GET','/admin/v1/initial-context'),
  ('GET','/admin/v1/internal-message/categories'),
  ('GET','/admin/v1/internal-message/categories/{id}'),
  ('GET','/admin/v1/internal-message/inbox'),
  ('GET','/admin/v1/internal-message/messages'),
  ('GET','/admin/v1/internal-message/messages/{id}'),
  ('GET','/admin/v1/login-audit-logs'),
  ('GET','/admin/v1/login-audit-logs/{id}'),
  ('GET','/admin/v1/login-policies'),
  ('GET','/admin/v1/login-policies/{id}'),
  ('GET','/admin/v1/me'),
  ('GET','/admin/v1/menus'),
  ('GET','/admin/v1/menus/{id}'),
  ('GET','/admin/v1/mfa/methods'),
  ('GET','/admin/v1/mfa/status'),
  ('GET','/admin/v1/notification-channels'),
  ('GET','/admin/v1/notification-channels/{id}'),
  ('GET','/admin/v1/online-session/my-sessions'),
  ('GET','/admin/v1/online-session/sessions'),
  ('GET','/admin/v1/operation-audit-logs'),
  ('GET','/admin/v1/operation-audit-logs/{id}'),
  ('GET','/admin/v1/org-units'),
  ('GET','/admin/v1/org-units/{id}'),
  ('GET','/admin/v1/perm-codes'),
  ('GET','/admin/v1/permission-audit-logs'),
  ('GET','/admin/v1/permission-audit-logs/{id}'),
  ('GET','/admin/v1/permission-groups'),
  ('GET','/admin/v1/permission-groups/{id}'),
  ('GET','/admin/v1/permissions'),
  ('GET','/admin/v1/permissions/{id}'),
  ('GET','/admin/v1/plan-modules'),
  ('GET','/admin/v1/plan-modules/{id}'),
  ('GET','/admin/v1/plan-quotas'),
  ('GET','/admin/v1/plans'),
  ('GET','/admin/v1/plans/{id}'),
  ('GET','/admin/v1/policy-evaluation-logs'),
  ('GET','/admin/v1/policy-evaluation-logs/{id}'),
  ('GET','/admin/v1/positions'),
  ('GET','/admin/v1/positions/{id}'),
  ('GET','/admin/v1/redis-cache-monitor'),
  ('GET','/admin/v1/roles'),
  ('GET','/admin/v1/roles/{id}'),
  ('GET','/admin/v1/routes'),
  ('GET','/admin/v1/server-monitor'),
  ('GET','/admin/v1/tasks'),
  ('GET','/admin/v1/tasks/type-name/{typeName}'),
  ('GET','/admin/v1/tasks/{id}'),
  ('GET','/admin/v1/tasks:type-names'),
  ('GET','/admin/v1/tenants'),
  ('GET','/admin/v1/tenants/{id}'),
  ('GET','/admin/v1/tenants/{id}/usage'),
  ('GET','/admin/v1/tenants:exists'),
  ('GET','/admin/v1/users'),
  ('GET','/admin/v1/users/username/{username}'),
  ('GET','/admin/v1/users/{id}'),
  ('GET','/admin/v1/users:exists'),
  ('GET','/api/v1/auth/captcha'),
  ('GET','/api/v1/networks/vpcs/{vpc_id}'),
  ('POST','/admin/v1/access-keys'),
  ('POST','/admin/v1/access-keys/token'),
  ('POST','/admin/v1/apis'),
  ('POST','/admin/v1/apis/sync'),
  ('POST','/admin/v1/configs'),
  ('POST','/admin/v1/dict/entries'),
  ('POST','/admin/v1/dict/langs'),
  ('POST','/admin/v1/dict/langs/batch'),
  ('POST','/admin/v1/dict/types'),
  ('POST','/admin/v1/internal-message/categories'),
  ('POST','/admin/v1/internal-message/inbox/delete'),
  ('POST','/admin/v1/internal-message/read'),
  ('POST','/admin/v1/internal-message/revoke'),
  ('POST','/admin/v1/internal-message/send'),
  ('POST','/admin/v1/internal-message/status'),
  ('POST','/admin/v1/login-policies'),
  ('POST','/admin/v1/me/contact'),
  ('POST','/admin/v1/me/contact/verify'),
  ('POST','/admin/v1/me/password'),
  ('POST','/admin/v1/menus'),
  ('POST','/admin/v1/menus/sync'),
  ('POST','/admin/v1/mfa/disable'),
  ('POST','/admin/v1/mfa/enroll/confirm'),
  ('POST','/admin/v1/mfa/enroll/start'),
  ('POST','/admin/v1/mfa/verify'),
  ('POST','/admin/v1/notification-channels'),
  ('POST','/admin/v1/notification-channels/{id}/send-test-email'),
  ('POST','/admin/v1/online-session/force-logout'),
  ('POST','/admin/v1/online-session/my-sessions/revoke'),
  ('POST','/admin/v1/org-units'),
  ('POST','/admin/v1/permission-groups'),
  ('POST','/admin/v1/permissions'),
  ('POST','/admin/v1/permissions/sync:perms'),
  ('POST','/admin/v1/plan-modules'),
  ('POST','/admin/v1/plan-quotas'),
  ('POST','/admin/v1/plans'),
  ('POST','/admin/v1/positions'),
  ('POST','/admin/v1/roles'),
  ('POST','/admin/v1/tasks'),
  ('POST','/admin/v1/tasks:control'),
  ('POST','/admin/v1/tasks:restart'),
  ('POST','/admin/v1/tasks:start'),
  ('POST','/admin/v1/tasks:stop'),
  ('POST','/admin/v1/tenants'),
  ('POST','/admin/v1/tenants/{id}/cleanup'),
  ('POST','/admin/v1/tenants:with-admin'),
  ('POST','/admin/v1/users'),
  ('POST','/admin/v1/users/{userId}/password'),
  ('POST','/api/v1/auth/captcha/verify'),
  ('POST','/api/v1/auth/forgot-password'),
  ('POST','/api/v1/auth/invitations/accept'),
  ('POST','/api/v1/auth/logout'),
  ('POST','/api/v1/auth/password/login'),
  ('POST','/api/v1/auth/platform/password/login'),
  ('POST','/api/v1/auth/refresh'),
  ('POST','/api/v1/auth/reset-password-by-code'),
  ('PUT','/admin/v1/access-keys/{id}'),
  ('PUT','/admin/v1/access-keys/{id}/secret'),
  ('PUT','/admin/v1/apis/{id}'),
  ('PUT','/admin/v1/configs/{id}'),
  ('PUT','/admin/v1/dict/entries/{id}'),
  ('PUT','/admin/v1/dict/langs/{id}'),
  ('PUT','/admin/v1/dict/types/{id}'),
  ('PUT','/admin/v1/internal-message/categories/{id}'),
  ('PUT','/admin/v1/internal-message/messages/{id}'),
  ('PUT','/admin/v1/login-policies/{id}'),
  ('PUT','/admin/v1/me'),
  ('PUT','/admin/v1/menus/{id}'),
  ('PUT','/admin/v1/notification-channels/{id}'),
  ('PUT','/admin/v1/org-units/{id}'),
  ('PUT','/admin/v1/permission-groups/{id}'),
  ('PUT','/admin/v1/permissions/{id}'),
  ('PUT','/admin/v1/plan-modules/{id}'),
  ('PUT','/admin/v1/plan-quotas/{id}'),
  ('PUT','/admin/v1/plans/{id}'),
  ('PUT','/admin/v1/positions/{id}'),
  ('PUT','/admin/v1/roles/{id}'),
  ('PUT','/admin/v1/tasks/{id}'),
  ('PUT','/admin/v1/tenants/{id}'),
  ('PUT','/admin/v1/users/{id}')
  ) allowed(method,path) ON a.method=allowed.method AND a.path=allowed.path
  WHERE p.code='sys:platform_admin';
  INSERT INTO sys_permission_apis(permission_id,api_id,created_at)
  SELECT p.id,a.id,NOW() FROM sys_permissions p CROSS JOIN sys_apis a
  JOIN (VALUES
  ('DELETE','/admin/v1/users/username/{username}'),
  ('DELETE','/admin/v1/users/{id}'),
  ('GET','/admin/v1/dashboard/login-status-distribution'),
  ('GET','/admin/v1/dashboard/login-trend'),
  ('GET','/admin/v1/dashboard/operation-action-distribution'),
  ('GET','/admin/v1/dashboard/overview'),
  ('GET','/admin/v1/initial-context'),
  ('GET','/admin/v1/me'),
  ('GET','/admin/v1/perm-codes'),
  ('GET','/admin/v1/roles'),
  ('GET','/admin/v1/roles/{id}'),
  ('GET','/admin/v1/routes'),
  ('GET','/admin/v1/users'),
  ('GET','/admin/v1/users/username/{username}'),
  ('GET','/admin/v1/users/{id}'),
  ('GET','/admin/v1/users:exists'),
  ('POST','/admin/v1/me/contact'),
  ('POST','/admin/v1/me/contact/verify'),
  ('POST','/admin/v1/me/password'),
  ('POST','/admin/v1/users'),
  ('POST','/admin/v1/users/{userId}/password'),
  ('POST','/api/v1/auth/logout'),
  ('PUT','/admin/v1/me'),
  ('PUT','/admin/v1/users/{id}')
  ) allowed(method,path) ON a.method=allowed.method AND a.path=allowed.path
  WHERE p.code='sys:tenant_manager';

  INSERT INTO sys_languages(language_code,language_name,native_name,is_default,is_enabled,sort_order,created_at)
  VALUES ('zh-CN','中文（简体）','简体中文',true,true,0,NOW()),
         ('en-US','英语','English',false,true,1,NOW());
  INSERT INTO sys_configs(key,name,value,value_type,is_built_in,created_at) VALUES
    ('sys.password.minLen','Password minimum length','8','INT',true,NOW()),
    ('sys.password.maxAgeDays','Password maximum age in days','90','INT',true,NOW()),
    ('sys.password.historyCount','Password history retention count','3','INT',true,NOW())
  ON CONFLICT (key) DO NOTHING;

  INSERT INTO sys_users(tenant_id,username,nickname,status,created_at)
  VALUES (0,current_setting('ani.bootstrap_username'),'平台管理员','NORMAL',NOW()) RETURNING id INTO administrator_id;
  INSERT INTO sys_user_credentials(tenant_id,user_id,identity_type,identifier,credential_type,credential,is_primary,status,created_at)
  VALUES (0,administrator_id,'USERNAME',current_setting('ani.bootstrap_username'),'PASSWORD_HASH',current_setting('ani.bootstrap_password_hash'),true,'ENABLED',NOW());
  INSERT INTO sys_user_roles(tenant_id,user_id,role_id,is_primary,status,assigned_at,created_at)
  VALUES (0,administrator_id,platform_role_id,true,'ACTIVE',NOW(),NOW());
  INSERT INTO sys_configs(key,name,value,value_type,is_built_in,created_at)
  VALUES ('deployment.bootstrap.v1','Initial database bootstrap','1','STRING',true,NOW());
END
$seed$;

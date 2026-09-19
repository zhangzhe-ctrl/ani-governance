-- Task-only narrow setup authority. Upstream seed API IDs predate this inventory.
-- Preserve existing bindings; do not run destructive full API/permission sync.
BEGIN;
INSERT INTO sys_permission_apis(permission_id,api_id)
SELECT p.id,a.id FROM sys_permissions p CROSS JOIN sys_apis a
WHERE p.code='sys:platform_admin' AND (a.method,a.path) IN (
 ('POST','/admin/v1/tenants:with-admin'),
 ('PUT','/admin/v1/tenants/{id}'),
 ('POST','/admin/v1/roles'),
 ('POST','/admin/v1/users'),
 ('POST','/admin/v1/online-session/force-logout'),
 ('GET','/api/v1/models'))
ON CONFLICT(permission_id,api_id) DO NOTHING;
COMMIT;

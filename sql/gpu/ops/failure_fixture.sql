-- Deliberately invalid legacy NULL accepted by the old nullable tenant mixin.
-- Only used in a disposable restore of the old-version backup.
INSERT INTO sys_quota_accounts(tenant_id,quota_code,occupied_units,version) VALUES(NULL,'gpu.count',0,0);

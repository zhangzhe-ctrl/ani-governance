SELECT
 (SELECT md5(COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id)::text,'')) FROM sys_quota_operations t WHERE tenant_id=$1),
 (SELECT md5(COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id)::text,'')) FROM sys_quota_charges t WHERE tenant_id=$1),
 (SELECT md5(COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id)::text,'')) FROM sys_quota_accounts t WHERE tenant_id=$1);

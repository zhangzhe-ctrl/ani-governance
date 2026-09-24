DO $$ BEGIN
 IF to_regclass('public.sys_gpu_usage_sync') IS NOT NULL THEN RAISE EXCEPTION 'failed transaction left new table'; END IF;
 IF NOT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='sys_plan_quotas' AND column_name='plan_id' AND is_nullable='YES') THEN RAISE EXCEPTION 'failed transaction left prior ALTER applied'; END IF;
 IF (SELECT occupied_units FROM sys_quota_accounts WHERE tenant_id=900 AND quota_code='gpu.count')<>2 THEN RAISE EXCEPTION 'legacy account changed'; END IF;
 IF (SELECT released_units FROM sys_quota_charges WHERE tenant_id=900 AND charge_id='90000000-0000-4000-8000-000000000004')<>1 THEN RAISE EXCEPTION 'legacy release changed'; END IF;
END $$;

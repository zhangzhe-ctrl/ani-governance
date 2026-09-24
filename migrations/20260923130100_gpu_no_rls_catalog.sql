-- GOV-ACC-V12-01. Explicit migration only; never executed by service startup.
-- Every owned business relation is isolated by its explicit tenant predicates.
DO $$
DECLARE relation record; policy record;
BEGIN
  FOR relation IN SELECT c.oid,n.nspname,c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='r' AND (c.relname LIKE 'sys_%' OR c.relname LIKE 'internal_message%') LOOP
    EXECUTE format('ALTER TABLE %I.%I NO FORCE ROW LEVEL SECURITY',relation.nspname,relation.relname);
    EXECUTE format('ALTER TABLE %I.%I DISABLE ROW LEVEL SECURITY',relation.nspname,relation.relname);
    FOR policy IN SELECT polname FROM pg_policy WHERE polrelid=relation.oid LOOP
      EXECUTE format('DROP POLICY %I ON %I.%I',policy.polname,relation.nspname,relation.relname);
    END LOOP;
  END LOOP;
END $$;
INSERT INTO sys_quota_definitions(code,display_name,unit,accounting_kind,created_at) VALUES
 ('gpu.physical.count','Physical GPU count','gpu','CONCURRENT',now()),
 ('gpu.shared_memory_mib','Shared GPU memory','MiB','CONCURRENT',now());

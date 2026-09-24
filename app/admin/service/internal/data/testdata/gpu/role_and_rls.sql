SELECT NOT r.rolsuper AND NOT r.rolbypassrls AND NOT r.rolcreatedb AND NOT r.rolcreaterole
 AND NOT has_database_privilege(current_user,current_database(),'TEMP')
 AND NOT has_database_privilege(current_user,current_database(),'CREATE')
 AND NOT has_schema_privilege(current_user,'public','CREATE')
 AND NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='r' AND (c.relrowsecurity OR c.relforcerowsecurity OR c.relowner=r.oid OR EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid)))
FROM pg_roles r WHERE r.rolname=current_user;

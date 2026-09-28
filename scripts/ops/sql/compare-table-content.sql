SELECT format($query$
SELECT %L AS relation, count(*) AS rows,
 md5(coalesce(string_agg(j::text,chr(10) ORDER BY j::text),'')) AS digest
FROM (SELECT to_jsonb(t) AS j FROM %I.%I t) AS payloads;
$query$,c.relname,n.nspname,c.relname)
FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
WHERE n.nspname='public' AND c.relkind='r' ORDER BY c.relname
\gexec

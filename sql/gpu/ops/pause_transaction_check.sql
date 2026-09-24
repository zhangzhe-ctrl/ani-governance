-- Read-only safe-pause verification for the isolated acceptance PostgreSQL.
SELECT datname,usename,state,count(*) AS connections,
 count(*) FILTER (WHERE xact_start IS NOT NULL) AS open_transactions
FROM pg_stat_activity
WHERE pid<>pg_backend_pid() AND backend_type='client backend'
GROUP BY datname,usename,state ORDER BY datname,usename,state;

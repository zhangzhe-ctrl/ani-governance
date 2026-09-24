SELECT EXISTS (
 SELECT 1 FROM pg_stat_activity a JOIN pg_locks l ON l.pid=a.pid
 WHERE a.datname=current_database() AND a.usename=current_user
 AND a.pid<>pg_backend_pid() AND a.state='active'
 AND a.wait_event_type='Lock' AND NOT l.granted
 AND a.query LIKE '%AS database_now FROM sys_tenants%'
);

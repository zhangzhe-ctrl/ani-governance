SELECT EXISTS (
 SELECT 1 FROM pg_stat_activity a JOIN pg_locks l ON l.pid=a.pid
 WHERE a.datname=current_database() AND a.usename=current_user
 AND a.pid<>pg_backend_pid() AND a.state='active'
 AND a.wait_event_type='Lock' AND NOT l.granted
 AND EXISTS (
  SELECT 1 FROM pg_locks held
  WHERE held.pid=a.pid AND held.relation='sys_tenants'::regclass
  AND held.granted AND held.mode='RowShareLock'
 )
);

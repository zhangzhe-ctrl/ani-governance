UPDATE sys_tenants SET expired_at=now() WHERE id=$1 RETURNING expired_at=now();

UPDATE sys_quota_operations SET next_attempt_at=now()+interval '1 hour'
WHERE tenant_id=$1 AND operation_id=$2;

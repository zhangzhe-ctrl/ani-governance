-- Explicit negative-test corruption; production cannot mutate its frozen request.
UPDATE sys_quota_operations SET canonical_request='{"schema_version":2,"quota_items":[]}'
WHERE tenant_id=$1 AND operation_id=$2;

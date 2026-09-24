-- Explicit negative-test corruption; production cannot mutate original units.
UPDATE sys_quota_charges SET original_units=original_units+1 WHERE tenant_id=$1 AND charge_id=$2;

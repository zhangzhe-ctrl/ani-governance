INSERT INTO sys_quota_charges (tenant_id, charge_id, operation_id, quota_code, original_units, released_units, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, 1, 0, now(), now())

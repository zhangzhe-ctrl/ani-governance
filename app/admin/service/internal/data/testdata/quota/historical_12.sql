INSERT INTO sys_quota_release_receipts (tenant_id, receipt_id, owner_service, release_event_id, payload_hash, payload_json, created_at)
		 VALUES ($1, $2, $3, $4, 'h', 'p', now())

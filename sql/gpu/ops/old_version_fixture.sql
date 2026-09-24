-- Minimal populated QUOTA-GPU-LOCAL-01 fixture on the old migration head.
-- No bootstrap and no new GPU semantic claims.
INSERT INTO sys_plans(id,name,created_at) VALUES(900,'gov-acc-old-fixture',now());
INSERT INTO sys_tenants(id,name,code,resource_tenant_id,status,plan_id,created_at) VALUES(900,'gov-acc-old-fixture','gov-acc-old-fixture','90000000-0000-4000-8000-000000000900','ON',900,now());
INSERT INTO sys_plan_quotas(plan_id,quota_code,quota_type,quota_value,created_at) VALUES(900,'gpu.count',NULL,8,now()),(900,'storage.bytes','STORAGE',1024,now());
INSERT INTO sys_quota_accounts(tenant_id,quota_code,occupied_units,version,created_at) VALUES(900,'gpu.count',2,1,now());
INSERT INTO sys_quota_operations(tenant_id,operation_id,resource_tenant_id,resource_id,actor_type,actor_id,owner_service,action,idempotency_key,request_hash,canonical_request,dispatch_state,created_at) VALUES(900,'90000000-0000-4000-8000-000000000001','90000000-0000-4000-8000-000000000900','90000000-0000-4000-8000-000000000002','user','900','ani-gpu-simulator','LAB_GPU_CREATE','90000000-0000-4000-8000-000000000003','old-fixture-hash','{"schema_version":1}','ACKED',now());
INSERT INTO sys_quota_charges(tenant_id,charge_id,operation_id,quota_code,original_units,released_units,created_at) VALUES(900,'90000000-0000-4000-8000-000000000004','90000000-0000-4000-8000-000000000001','gpu.count',3,1,now());
INSERT INTO sys_quota_release_receipts(tenant_id,receipt_id,owner_service,release_event_id,payload_hash,payload_json,created_at) VALUES(900,'90000000-0000-4000-8000-000000000005','ani-gpu-simulator','90000000-0000-4000-8000-000000000006','old-release-hash','{"released_total":1}',now());

-- Explicit software-contract fixture; never called by service construction.
INSERT INTO sys_plans(id,name,created_at) VALUES(1,'gov-acc-joint-b',now());
INSERT INTO sys_tenants(id,name,code,resource_tenant_id,status,plan_id,created_at) VALUES(1,'gov-acc-joint-b','gov-acc-joint-b','11111111-1111-4111-8111-111111111111','ON',1,now());
INSERT INTO sys_quota_definitions(code,display_name,unit,accounting_kind,created_at) VALUES('test.concurrent','Software contract concurrent','unit','CONCURRENT',now());
INSERT INTO sys_plan_quotas(plan_id,quota_code,quota_value,created_at) VALUES(1,'gpu.physical.count',32,now()),(1,'gpu.shared_memory_mib',1048576,now()),(1,'test.concurrent',1000,now()),(1,'storage.bytes',1048576,now());
SELECT setval(pg_get_serial_sequence('sys_plans','id'),1,true);
SELECT setval(pg_get_serial_sequence('sys_tenants','id'),1,true);

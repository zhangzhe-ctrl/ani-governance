-- FIELD-CLEANUP-01：删除 4 张审计日志表的 geo_location 列。
-- 前置：IMAGE-SLIM-01 已停止采集（新记录为 NULL），OPA-REMOVAL-01 与本批
-- 删除了 Proto 消息、Ent 字段与对应代码。
-- 注意：DROP COLUMN 会一并丢弃历史行的归属地数据，执行前必须备份目标库；
-- 该列无法由迁移"加回"，回滚只能靠备份恢复。
-- 来源：手写（atlas migrate diff 的 dev 库规范化被既有 sys_quota_operations
-- 外键问题阻塞，见 docs/geo-location-field-removal-plan.md §3 注记）；
-- 校验：atlas migrate hash + 在独立开发库 migrate apply 通过。

ALTER TABLE "sys_api_audit_logs" DROP COLUMN "geo_location";
ALTER TABLE "sys_data_access_audit_logs" DROP COLUMN "geo_location";
ALTER TABLE "sys_login_audit_logs" DROP COLUMN "geo_location";
ALTER TABLE "sys_operation_audit_logs" DROP COLUMN "geo_location";

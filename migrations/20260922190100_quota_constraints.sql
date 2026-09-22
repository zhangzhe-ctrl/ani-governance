-- QUOTA-GPU-LOCAL-01 constraints：验证回填完成后，对旧套餐配额表
-- 加非空/唯一/FK/数值检查。执行顺序：expand → sql/quota/001_catalog_and_backfill.sql → 本文件。
-- 本文件只处理 sys_plan_quotas（旧表）；账本新表的约束在 expand 中随建表创建。

-- 数值检查：quota_value 非负（uint64 合同；PostgreSQL bigint 存储）。
ALTER TABLE "sys_plan_quotas" ADD CONSTRAINT "sys_plan_quotas_quota_value_nonnegative_ck" CHECK ("quota_value" >= 0);

-- 回填完成后的非空约束。
ALTER TABLE "sys_plan_quotas" ALTER COLUMN "quota_code" SET NOT NULL;

-- 同一套餐同一配额编码只能有一行政策项（列顺序与 schema.sql 导出一致）。
CREATE UNIQUE INDEX "uix_sys_plan_quotas_plan_id_quota_code" ON "sys_plan_quotas" ("quota_code", "plan_id");

-- 政策项的 quota_code 引用目录（RESTRICT：有政策引用的目录项不可删除）。
ALTER TABLE "sys_plan_quotas" ADD CONSTRAINT "sys_plan_quotas_quota_code_fkey" FOREIGN KEY ("quota_code") REFERENCES "sys_quota_definitions" ("code") ON DELETE RESTRICT;

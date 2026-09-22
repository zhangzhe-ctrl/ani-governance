# 迁移报告（DB-01～DB-07）

迁移顺序（计划 §12.1）：`20260922190000_quota_expand.sql` → `sql/quota/001_catalog_and_backfill.sql` → `20260922190100_quota_constraints.sql`。
Atlas CLI v1.3.0（自 v1.3.0 tag 源码构建，release.ariga.io 网络不可达）；`atlas migrate hash` 已更新 atlas.sum，既有 initial 条目未改写。

## 执行结果

| 项 | 结果 | 证据 |
| --- | --- | --- |
| DB-01 空库顺序迁移 | pass：initial→expand→data→constraints 按 `atlas migrate apply 1` 分步执行，每步核对 `migrate status`；目录 4 行（api.calls/gpu.count/storage.bytes/user.count） | p2 migrate logs（run-dir）、migrate-catalog.txt |
| DB-02 旧行升级 | pass：legacy_plan_a 三行 USER_LIMIT/STORAGE/API_CALL 升级后 id/plan_id/quota_type/quota_value 与升级前完全一致（diff 为空），quota_code 回填正确 | p2-db02-legacy-before/after.txt |
| DB-03 坏数据拒绝 | pass：NULL 字段/未知类型/重复 (plan,code) 三类 fixture 均整笔失败（psql exit=3），目录插入回滚（count=0），数据未变 | p2-db03-summary.md、p2-db03-*.txt/log |
| DB-04 直接 SQL 跨租户关联 | pass：tenant B 插入指向 A 操作的 charge 被复合 FK `(tenant_id,operation_id)` 拒绝；未知租户 receipt 被 tenant FK RESTRICT 拒绝 | TestQuotaPostgresCompositeFK |
| DB-05 运行账号 DDL | not_verified：隔离环境使用 postgres 超级用户（trust），未单独建运行账号做 DDL 拒绝测试；`migrate:false` 启动禁迁移逻辑为既有实现 | — |
| DB-06 备份恢复 | not_verified：未执行 pg_dump→另一库恢复流程 | — |
| DB-07 升级后新增 GPU 配额再重跑数据脚本 | pass：quota_type=NULL、quota_code='gpu.count' 合法行不报错；重跑后目录数量与现有数据不变 | p2-db07-*.log/txt |

## 预检范围（数据脚本内置）

待迁移旧行检查：plan_id/quota_type/quota_value 为 NULL、未知 quota_type、同套餐同类型重复、数量负数/越界（上限 9223372036854775807）、不存在的 plan 引用；任一命中整笔失败并输出脱敏主键清单。
已回填行检查：目录存在、旧三项 code/type 一致、按 (plan_id,quota_code) 唯一；gpu.count 的 quota_type=NULL 合法。

## 与 schema.sql 的已知偏差（已消除）

- 原记录的 CHECK 约束偏差已在残留补齐批次消除：`entsql.Annotation.Checks` 支持命名检查，CHECK 已进入 Ent schema 注解，重新导出的 schema.sql 与手写迁移中的同名约束完全一致（已逐名核对）。未来 `migrate diff` 不再产生 DROP 候选。
- 迁移中 `sys_quota_operations` 的自引用复合 FK 以 ALTER 形式在索引之后添加（PG 要求被引用唯一索引先存在）；schema.sql 中为内联形式，语义一致。

## 恢复说明

结构迁移仅新增对象，不修改既有列；回滚 = 停服后 DROP 新表/新索引/新列（另开明确批次，本批不提供 down 脚本）。数据脚本幂等可重跑；重复执行校验目录一致并 no-op。

# 版本数据脚本

`20260922_quota_catalog_backfill.sql` 是配额目录和旧行回填输入。升级顺序必须是 `migrations/20260922190000_quota_expand.sql` → 本脚本 → `migrations/20260922190100_quota_constraints.sql` → 后续版本迁移。先检查旧数据，再按原脚本回填；失败时停止，不跳过约束。

Atlas 结构迁移不会自动读取本目录。执行者需按 [部署流程](../../docs/deployment.md) 显式安排这个数据步骤；`app/admin/service/schema.sql` 不能替代迁移链。本批只迁址，SQL 字节和历史迁移均未改。

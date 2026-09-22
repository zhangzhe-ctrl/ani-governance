# DB-03 数据脚本坏数据拒绝验证（quota-gpu-local-01）

目标：验证 `sql/quota/001_catalog_and_backfill.sql` 对坏旧行整笔拒绝（回滚）、数据不变。未修改数据脚本与 migrations。

## 环境与准备

- 容器：`quota-gpu-local-01-pg`（PostgreSQL 16，127.0.0.1:42193，trust，user postgres）。
- Atlas：`/home/ubuntu/.local/quota-lab-tools-01/atlas migrate apply 2 --dir file://migrations --url "postgres://postgres@127.0.0.1:42193/<db>?sslmode=disable"`。
- 每个 variant 均先 `DROP DATABASE IF EXISTS` + `CREATE DATABASE`，再应用前两个迁移（20260921134442_initial + 20260922190000_quota_expand，后者新增 `quota_code` 列）。
- 脚本以 `psql -v ON_ERROR_STOP=1` 执行；schema 参考 migrations/20260921134442_initial.sql（sys_plans 仅需 name/created_at/updated_at）。

## Variant 1：gov_bad（NULL quota_code + 重复 USER_LIMIT + UNKNOWN_TYPE）

种子（plan_id=1 有效）：id=1 USER_LIMIT 10；id=2 USER_LIMIT 20（与前一行重复）；id=3 UNKNOWN_TYPE 99。quota_code 均留 NULL。

- 执行前快照：p2-db03-before.txt（3 行，quota_code 全 NULL）。
- 执行：退出码 **3**（非零，见 p2-db03-exit.txt），日志 p2-db03-reject.log：
  `ERROR: quota backfill precheck failed: unknown quota_type rows: id=3`（脚本预检 2，先于预检 3 触发）。
- 执行后快照：p2-db03-after.txt，与 before **diff 为空**（数据完全不变）。
- 附加验证：`sys_quota_definitions` 计数 = 0，事务已整体回滚，目录未写入。

## Variant 2：gov_bad2（仅重复行，plan 有效）

种子：同一 plan_id=1 下两行 USER_LIMIT（quota_value 10/20）。

- 执行前快照：p2-db03b-before.txt。
- 执行：退出码 **3**（p2-db03b-exit.txt），日志 p2-db03b-reject.log：
  `ERROR: quota backfill precheck failed: duplicate (plan_id, quota_type) rows: plan_id=1 type=USER_LIMIT`（脚本预检 3）。
- 执行后快照：p2-db03b-after.txt，与 before **diff 为空**。

## 结论

| Variant | 退出码 | 触发预检 | 数据不变 | 结果 |
|---|---|---|---|---|
| gov_bad（未知类型+重复） | 3 | 未知 quota_type（id=3） | 是 | PASS |
| gov_bad2（仅重复） | 3 | duplicate (plan_id, quota_type) | 是 | PASS |

未发现脚本缺陷：坏数据均被预检拒绝，错误信息含脱敏主键清单，事务回滚保证零写入。

## 证据文件

- p2-db03-before.txt / p2-db03-after.txt / p2-db03-reject.log / p2-db03-exit.txt
- p2-db03b-before.txt / p2-db03b-after.txt / p2-db03b-reject.log / p2-db03b-exit.txt

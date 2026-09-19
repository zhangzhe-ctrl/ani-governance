# 数据库备份与恢复（等保二级）

等保二级要求数据备份恢复：至少提供**本地备份**机制，并能验证可恢复。

## 1. 物理库定期备份（pg_dump）

`pg_backup.sh` 每次生成一份自定义格式（`-Fc`，自带压缩）全量备份，并按 `KEEP_COPIES`（默认 30）轮换。

```bash
# 手动执行（docker 容器部署自动走 docker exec，否则用本地 pg_dump）
./scripts/backup/pg_backup.sh

# cron 每日 02:00（生产）
0 2 * * * /opt/ani-governance/scripts/backup/pg_backup.sh >> /var/log/pg_backup.log 2>&1
```

环境变量见脚本头部注释（容器名、连接参数、目录、保留份数）。

## 恢复（pg_restore）

```bash
# 恢复到既有库（先建空库）
PGPASSWORD='*Abcd12345' createdb -h localhost -U postgres -T template0 gwa_restore
docker exec -i -e PGPASSWORD='*Abcd12345' citus-server-standalone \
    pg_restore -U postgres -d gwa_restore --no-owner --no-privileges < gwa-20260828-020000.dump

# 定期演练建议：每月抽一份备份在隔离实例上恢复并抽查行数
```

## 2. 核心业务数据 OSS 备份（应用内）

系统内置 asynq 定时任务 `backup`：导出核心身份/权限/组织表为 JSON（gzip）上传 OSS `backups` 桶，通过任务调度页管理。

## 3. 审计日志留存（≥6 个月）

`audit_log_archive` 系统级定时任务（每天 03:30）把超过保留期的六类审计日志行
导出为本地 JSONL 归档文件后从库中删除：

- 归档目录：`AUDIT_ARCHIVE_DIR`（默认 `./data/audit-archive`）——**纳入服务器备份范围**（脚本备份的是库，归档文件在磁盘上，需随主机/卷一起备份）；
- 库内保留天数：`AUDIT_RETENTION_DAYS`（默认 180，即 ≥6 个月留在库内，更早的行在归档文件中长期保存，按需另行转存）。

## 口令策略（等保"身份鉴别"）

阈值自 2026-09-12 起存于 `sys_configs` 平台参数表（启动时内置键缺一补一，可改不可删），经管理台「参数管理」页调整、即时生效；环境变量 `PASSWORD_MIN_LEN` / `PASSWORD_MAX_AGE_DAYS` / `PASSWORD_HISTORY_COUNT` 已废弃。参数行在库内，随本脚本备份一起导出/恢复——原环境变量方案的调优值不在备份范围内，此为其改善。注意：各口令写路径的 proto 校验层（buf-validate）另有 8 字符硬下限，属 wire 层纵深防御；参数仅在调高（>8）时收紧生效，调低于 8 不产生效果。

| 项 | 默认 | 平台参数键 |
|---|---|---|
| 最小长度 | 8 | `sys.password.minLen` |
| 复杂度 | 四类字符（大小写/数字/符号）至少三类 | 固定规则（不可配） |
| 有效期 | 90 天（超期拒绝登录，走重置流程） | `sys.password.maxAgeDays`（≤0 关闭） |
| 历史口令 | 最近 3 条不可复用（存 credential.extra_info） | `sys.password.historyCount`（≤0 关闭） |

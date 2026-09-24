# Governance 配额恢复 Ent

本次纠正此前将 Acc 的 pgx/sqlc 要求扩大到 Governance 的实现。Acc 仓库及其依赖版本未改变。

- 事务由 `ent.Client.Tx` 创建，查询、行锁、占额/退额、DELETE、投递和 usage sync 均通过同一个 `ent.Tx` 执行；成功提交，失败或 panic 回滚。
- 删除 `quota_pgx.go`、`quotasql`、`sqlc.yaml` 及 sqlc 生成门禁；数据库装配恢复配置指定的 Ent 驱动，不强制改用 pgx。
- 保留业务合同、显式 tenant 条件、锁顺序、幂等、累计释放、lease/revision CAS、套餐过滤/分页、现有数据约束及 Atlas 版本链。本次无 schema 变更，不需要新迁移或重跑种子。
- 增加真实 PostgreSQL Ent mutation hook/事务 hook 回归，验证第二笔 charge 失败时整笔占额回滚；生产 postgres 驱动和 tracing 开关均验证真实占额/撤销。

验证在 Fedora 隔离目录 `/home/chabking/gov-quota-ent-20260924-QZNFqX` 执行。使用任务自己的 Go 缓存和 `gov-quota-ent-QZNFqX` PostgreSQL 16.10 容器，端口仅绑定回环；业务测试使用 non-owner runtime 角色。初始化显式执行既有迁移，不由服务启动建表。

## 验证状态

基线为 `a7b3c4b25f6b86f7f7ff6a952d546d6a0b7aa663`，本次 Ent 改造已提交并发布为 `479db091a97dc51376a864d06409f76f84ac0139`；以下记录是该版本当时的隔离验证，不是后续整改版本的验收。实际执行源码已按 [source.sha256](source.sha256) 与远端副本逐文件核对，生成检查后再次通过 [最终核对](source-check-final.log)；[deleted-files.txt](deleted-files.txt) 中的旧执行路径在远端同样不存在。

| 检查 | 状态与证据 |
|---|---|
| PostgreSQL 完整配额数据回归 | pass；[data.log](data.log)，43 个顶层测试、58 个子测试通过，覆盖显式租户、并发限额、累计释放、负向原子性、Ent hooks、投递/sync 租约、10 个提交前后 SIGKILL 点及 12 次双进程删除/领取竞争 |
| 套餐服务 PostgreSQL 回归 | pass；[service-pg.log](service-pg.log) |
| data/service/server 与 pkg 测试 | pass；[packages.log](packages.log) |
| 正式、quota_lab、admin 构建 | pass；`go build -o /dev/null ./app/admin/service/cmd/server`、同命令加 `-tags quota_lab`、`go build -o /dev/null ./app/admin/service/cmd/admin` 均退出 0 |
| Ent 执行边界与格式 | pass；`python3 scripts/check-quota-ent.py`、`bash scripts/verify-gpu-format.sh`、本地 `git diff --check` |
| 数据层 race | pass；[race-data.log](race-data.log)，包含 Ent hooks、并发限额、取消/领取竞争、GPU 和进程故障恢复 |
| 服务层 race | pass；[race-service.log](race-service.log)，GPU/配额投递/Accelerator 服务用例 |
| Ent 再生成一致性 | pass；[schema.log](schema.log)，`make verify-quota-ent` 重生成 Ent 与 schema.sql 后逐字节比较，无差异 |
| 租户条件 mutation | pass；[tenant-mutation.log](tenant-mutation.log)，仅在 Go overlay 中移除 `GetOperationForUser` 的 tenant 条件，已知其他租户读取断言失败；未改动交付源码 |

首次编译及测试暴露的 Ent API 类型、时间戳显式写入/扫描、条件 upsert 无返回行语义已修正。原负向释放测试改为比较所有持久化 JSON 字段，排除 Ent 私有事务驱动对象；锁等待检测改为查询实际 `pg_locks` 的 tenant 表 RowShareLock 与未授予锁，不再匹配 sqlc SQL 文本。初始失败日志保留在上述远端目录，不作为通过证据。

`TestQuotaGpuOldPopulatedUpgradeReplay`、`TestQuotaGpuRestoredProjectionContinuation` 依赖独立历史备份，未在本次重跑，记为 not_verified；本次无结构迁移。`TestQuotaProcessChild` 在父进程入口跳过，在进程恢复测试创建的真实子进程中执行。

执行命令（在上述隔离副本、受限 `QUOTA_LAB_PG_DSN` 和任务缓存下）：

```sh
go test -tags quota_pg ./app/admin/service/internal/data -run 'TestQuota|TestPlanQuota' -count=1 -v -timeout=15m
go test -tags quota_pg ./app/admin/service/internal/service -run 'TestPlanQuota' -count=1 -timeout=10m
go test ./app/admin/service/internal/data ./app/admin/service/internal/service ./app/admin/service/internal/server ./pkg/...
go build -o /dev/null ./app/admin/service/cmd/server
go build -tags quota_lab -o /dev/null ./app/admin/service/cmd/server
go build -o /dev/null ./app/admin/service/cmd/admin
go test -race -tags quota_pg ./app/admin/service/internal/data -run 'TestQuotaEnt|TestQuotaPostgresConcurrentLimit|TestQuotaPostgresCancelClaimRace|TestQuotaPostgresLeaseGenerationGuard|TestQuotaPostgresPolicyChanges|TestQuotaGpu|TestQuotaProcess' -count=1 -timeout=20m
go test -race ./app/admin/service/internal/service -run 'TestGpu|TestQuotaDurable|TestAccelerator' -count=1 -timeout=10m
make verify-quota-ent
bash scripts/verify-gpu-format.sh
```

远端测试容器、测试卷及本次任务专用 Go 缓存已清理，源码与日志保留，见 [cleanup.log](cleanup.log)。

未部署。已推送 `479db09`，该提交 GitHub CI 在格式阶段失败（run `35973339978`），不提供后续门禁通过证据。历史 pgx/sqlc 证据仍只证明历史版本，不计入本次 Ent 修复验收。

## 审核后的证据补充

本目录原始日志曾被仓库 `*.log` 忽略规则排除；现从原 Fedora 隔离目录补入供复核，没有重写失败或跳过状态。原格式通过发生在源码副本，与完整 Git checkout 的增量格式范围不同，不能视为 CI 格式通过。后续整改、完整 checkout 验证和历史恢复结果见 [整改记录](../gov-quota-ent-review-20260924/README.md)。

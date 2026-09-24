# GOV-ACC-V12-01 交付证据入口

**软件适配及无GPU联调已完成：82项软件必验全部通过；完整矩阵85 pass / 10 not_verified。** Governance CI按用户明确指示不等待；9项硬件/正式owner/部署边界保持未验证。[最终收尾与恢复备份](release/closeout.md)记录实际发布和清理。

本批实现将 Accelerator 全部生产数据库查询，以及 Governance GPU 受理所复用的完整通用配额账本、套餐配额、投递、删除、退款和同步查询迁入固定 sqlc v1.30.0 生成的 pgx 层。无关 Governance Ent 模块保留；历史 quota_lab 的独立模拟器 `sim_*` 存储不属于正式配额账本，模拟器调用的实际配额实现与正式入口相同。

全部运行、格式化、生成、构建、测试、审计和证书操作在授权 Fedora 隔离目录执行。业务运行角色非 owner、非 superuser、无 BYPASSRLS/DDL/TEMP，显式租户、复合外键及 JSON 身份约束通过真实 PostgreSQL 负向和查询 mutation 验证。服务启动不迁移或播种。

## 最终模块和运行证据

Gov 实现提交 `bd9ad1a33bbe8ce28f4faeb19bfc9ee494ae8a84` 已推送授权任务分支，精确 SHA 的 CI 最后观测仍在运行，按用户明确指示不再等待；[发布回执](release/publication.json) 与 [提交源码比对](release/committed-source-parity.json) 记录当前状态。1535 个最终联合受测文件与已提交源码完全匹配。

- Accelerator 已发布 `1d32dd9a9173b8869fa0ef2ae64e20b88f2ca0a3`，任务分支 `codex/governance-accelerator-v1.2`。Gov 正式模块为 `v0.0.0-20260924030150-1d32dd9a9173`；公共代理下载、sum 和来源见 [最终模块回执](bff/final-1d32dd9/module.json)。[精确 SHA CI](release/acc-ci-success.json) 成功。
- [数据层最终报告](data-layer.md)、[完整 make verify-gpu 日志](data/resume/verify-gpu-05.log) 和 [退出/时间回执](data/resume/verify-gpu-05.meta)：最终模块下 exit 0；包括正式/lab/admin 构建、sqlc/schema 无漂移、受影响鉴权/Network、真实 PG、故障进程、race、lab 和审计。旧失败日志保留。
- [A 层最终普通进程验证](bff/final-1d32dd9/README.md)：普通 Gov→普通 Acc，Publish 实际到达消费证明门槛并拒绝；没有 profile 或 VERIFIED 状态副作用，正式目录 NOT_ENABLED。
- [B 层最终六场景普通运行](joint-resume/20260924T031542Z/test.log)：真实 pgx/sqlc、PG、mTLS、两个 Gov 进程和独立持久测试 owner，含 FULL/UNKNOWN 两个真实 JWT/HTTP BFF 窗口；exit 0，140.884 秒。其他定向修复记录见 `joint-resume/`。
- [同一六场景 race 运行](joint-resume/20260924T031813Z/test.log)：exit 0，161.297 秒；独立实际复跑完整进程/网络/持久化链路，未报告数据竞争。
- C 层生产 provider reader TLS HTTP/Unix gRPC 回放、22 RPC mTLS、Acc dump restore/mutation/race 证据在固定 Acc 提交的 `docs/evidence/gov-acc-v12-01/acc/`；这些构造输入不是硬件采样。
- [实际 PostgreSQL 中断回执](joint-resume/acc-pg-outage.json)：同一普通 Acc PID 的 ready 200→503→200，health 保持 200，持久供给记录摘要不变。

逐项结论以 [95 项验收矩阵](acceptance-results.md) 为准，发布、最终源码一致性及清理回执集中在 `release/`。CI例外见 [用户收尾决定](release/ci-disposition.md)，Fedora通过不替代Gov GitHub CI通过。

## 使用与恢复

- [通用 GPU owner 接入及释放指南](../../contracts/gpu-owner-integration-guide.md)：实际字段、身份、完整 charge、持久 ACK、DELETE 墓碑、ObserveRelease、累计退款、可靠通知和后续硬件验收。
- [本批运行/迁移/恢复手册](../../operations/governance-accelerator-v1.2.md)：显式迁移、唯一 schema 来源、版本锁、停止受理/worker、隔离库恢复及复跑入口。
- [接口登记](../../interface-integration-register.md) 记录管理/租户 BFF、权限和错误边界。

原始历史报告是当时检查点，后续结果以带时间和退出码的新增回执覆盖当前结论，不改写过去的失败或未验证事实。恢复备份、DSN、证书和私钥不提交到仓库。

本批软件验证不证明正式 Inference owner 已接入、真实 GPU 数据平面、任意 owner 运行支持或生产部署；对应 OWNER/LIVE/DEPLOY 项保留 `not_verified`。

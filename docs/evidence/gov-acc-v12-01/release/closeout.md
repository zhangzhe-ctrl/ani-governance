# GOV-ACC-V12-01 收尾记录

2026-09-24，82 项软件必验全部通过。95 项矩阵最终为 **85 pass、10 not_verified、0 fail**：9 项正式 owner/真实 GPU/多 owner/生产部署按原范围不验证；SHIP-03 按[用户明确决定](ci-disposition.md)接受不等待 Governance CI 成功，保留 not_verified。

## 版本与交付

| 项目 | 固定实现与事实 |
|---|---|
| Accelerator | `1d32dd9a9173b8869fa0ef2ae64e20b88f2ca0a3`；已发布，精确 SHA CI success |
| Governance 实现 | `bd9ad1a33bbe8ce28f4faeb19bfc9ee494ae8a84`；已发布；本收尾记录所在后续提交仅增加证据、版本及状态说明 |
| 正式 Acc 模块 | `v0.0.0-20260924030150-1d32dd9a9173`；GOWORK=off、公共代理、模块 sum 校验通过，无兄弟 replace/internal/Proto 复制 |
| 发布范围 | 两仓准确 origin，普通非强推到 `codex/governance-accelerator-v1.2`；未合并 main、未打 tag、未部署生产 |
| 一致性 | 本地完整暂存树与 Fedora 独立 import tree 相同；Gov 实现 Git bundle 的 commit/tree 相同，最终联合 manifest 的 1535 文件全部匹配；后续证据提交的最终 SHA 在外置发布回执及完成报告记录，避免自引用 |

两仓全量生产查询整改范围、唯一 schema、受限角色/no-RLS、显式租户/mutation、同 pgx.Tx 原子性及旧数据升级结果见[数据层](../data-layer.md)与[完整验收表](../acceptance-results.md)。本批没有把无关 Gov Ent 模块或 lab 独立模拟器存储改造成另一套 GPU 账本。

完整 Fedora `make verify-gpu` 与 Acc `make verify` 通过；最终六项联合 normal/race 分别 140.884/161.297 秒。普通 A→A 的 Publish 到达消费证明门槛后拒绝；B 使用真实业务代码、PG、mTLS 和独立持久测试 owner，覆盖非空 binding 及真实 JWT BFF；C 是生产 reader 的受控来源回放。硬件输入均明确为构造测试证据。审计为 0 可达漏洞和 0 未获准密钥发现；依赖清单及非可达告警边界保留原始日志。

## 实际清理与恢复

[cleanup-result.json](cleanup-result.json) 为清理后独立检查，[cleanup.log](cleanup.log) 记录执行；exit 0。

- A/B helper 和普通服务进程已停止；独立检查 task process=0，任务监听端口无残留。
- 3 个固定任务容器及其 3 个唯一匿名卷已删除；库和角色随隔离实例移除，没有操作共享数据库或其他容器。
- 已审阅临时 DSN、session、token、证书和私钥已删除；另一次全任务根文件名检查未发现 `.key`、`.pem`、`*dsn` 或 `control-token` 残留。39 GiB 左右的十个任务私有 cache 目录已删除。
- 所有源码、脱敏证据、版本锁、固定工具及旧恢复备份保留；旧 dump 收紧至 0600，原内容不变。
- 最终恢复资料在 Fedora `/home/chabking/gov-acc-v12-01-20260923/recovery/final-20260924T034035Z`：19 个 custom database dump、2 个完整 cluster dump、Redis RDB、TOC 和校验清单。文件均为 0600，目录 0700，SHA256 校验通过。
- 最终备份清单 SHA256 为 `fdd4fbfec0c6ccec25d326b10678caa85287945f63f7be347125c103a81da583`。完整备份含角色密码 hash、业务身份和测试会话，只保留在受保护位置，不提交内容。清理时只验证 dump/TOC/摘要，没有将这次新备份冒称再次还原；此前 populated 另库恢复及 worker 收敛另有实际证据。

需要恢复时按[运维手册](../../../operations/governance-accelerator-v1.2.md)在新的隔离实例校验并恢复备份，重新签发 CA/mTLS 和会话，不重跑 bootstrap 清除旧事实。含新 schema/canonical 的库不可直接交给不兼容旧二进制。

[通用 owner 指南](../../../contracts/gpu-owner-integration-guide.md)已交付。正式 Inference owner、受管 workload renderer、真实 GPU 分配/推理/释放、第二 owner 与生产部署仍需各自后续验收；本次完成的是 Gov+Acc 软件适配和无 GPU 合同联调。

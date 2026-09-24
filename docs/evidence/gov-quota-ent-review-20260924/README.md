# Governance Ent 审核整改（2026-09-24）

输入为 `479db091a97dc51376a864d06409f76f84ac0139` 及用户提供的 review.md / ci-failure-excerpt.txt。用户授权修复审核项并修复 CI。继续使用 Ent；不变更 schema、业务权限、RLS、Accelerator 版本或生产部署。

## 修改及回归

- SEARCH 在数值/时间比较解析之前处理；空白搜索保持恒真，非空搜索以 `to_jsonb(column) #>> '{}'` 恢复旧查询的 JSON 标量文本表示，字段仍经白名单，搜索值仍参数绑定。真实 PG 的数值空白/非空、时间空白/日期片段四个新增反例在修复前失败，修复后通过；原有比较、NULL、组合、排序和分页继续覆盖。
- 配额、套餐及 usage sync 本次曾改为应用时间的 created_at/updated_at，统一改为由当前 Ent 事务读取 PostgreSQL CURRENT_TIMESTAMP，再传给 Ent mutation/upsert。同一事务内多次读取仍为同一事务起始时间。原本由调用方指定的重试时间合同保留；未将业务时间字段全部强行改为数据库时间。
- 第二条 charge 失败时，直接断言 operation、account、charge 全部回滚。新增 panic、取消和 commit hook 拒绝提交的真实 PG 路径，断言失败事务写入不持久化。
- 生产 NewEntClient 在 trace 开/关下断言 accumulator 实际收到事务读写 SQL、脱敏摘要；回滚后的语句事件仍保留，且数据库行不存在。审计事件描述已执行的语句尝试，不将其误称为事务提交凭据。
- 六个继承的格式问题仅由 gofmt 修正；Makefile race 正则加入 TestQuotaEnt，不缩小原有范围。
- GitHub Actions 增加无论门禁成功或失败都上传的证据 artifact，保存 SHA、源码摘要、Go/PG 版本、完整门禁日志及退出码；退出失败仍使 job 失败。
- 原 Ent 整改目录引用的日志已从原 Fedora 目录补入。缺失原因是原 `*.log` 忽略规则；新增仅针对这两个证据目录的例外，更新已提交/已推送和原 CI 失败状态。原日志不证明新版本。

## 执行边界

全部生成、格式化、构建、测试和审计在 Fedora `/home/chabking/gov-ent-review-fix-20260924` 执行。使用完整 Git checkout、任务独立 Go 缓存，GOWORK=off、GOMAXPROCS=2、GOFLAGS=-p=2。PostgreSQL 16.10 独立容器只绑定回环25435，业务测试使用非owner、无DDL/TEMP/BYPASSRLS的运行角色。

修复前反例见 search-before.log、clock-before.log、format-before.log；修复后定向结果见 focused.log。完整门禁与两项历史恢复的最终状态以本目录执行回执及最终 CI 为准。

历史恢复使用此前保留的 old-populated.dump 与 new-populated.dump，输入摘要见 historical-inputs.sha256。分别恢复到独立数据库，显式应用所需版本迁移和权限；终态投影的丢失ACK故障只作用于新恢复副本，不改原备份。

## 本轮 Fedora 结果

- `make verify-gpu` 整体 exit 0（verify.log / verify.exit）：Ent 再生成无差异、静态边界、完整 Git checkout 格式、三个构建、普通回归、PG data（6.924s）/套餐服务（0.124s）、data race（55.752s）/service race（1.160s）、lab 和审计均通过。
- `TestQuotaGpuOldPopulatedUpgradeReplay` 在旧 populated 备份升级后的独立库中通过（upgrade.log）。
- `TestQuotaGpuRestoredProjectionContinuation` 在另一 populated 备份恢复副本中通过（restore.log），含持久投影重新领取、ACK、余额不变量及再次领取为空。两项在旧报告中的 not_verified 已由本轮真实执行补齐。
- govulncheck 0 可达漏洞；另有1个导入包/8个所需模块的未调用告警，原文保留；gitleaks 0发现，未扩大密钥例外。
- source-parity.json / source.sha256 绑定1737个非文档文件，和 Fedora 受测目录逐字一致。后追加说明和执行日志不改变受测代码。

完整执行命令见 full.sh、focused.sh、history.sh。GitHub CI 是独立执行，最终状态请按包含本修复的实际提交查看 Actions；成功或失败都有绑定该 SHA 的 quota-ent artifact，本记录不以 Fedora 通过代替 CI 通过。任务验证资源和受保护恢复备份的最终清理回执保留在上述 Fedora 任务目录。

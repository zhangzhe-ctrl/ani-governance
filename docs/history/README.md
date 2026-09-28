# 历史索引

本仓以提交 `63849fc4cde38b879184a8fea4a6f539e60063e1` 冻结清理前来源。被移出 HEAD 的 `migration/`、`docs/evidence/`、`third_party/` 与日期计划可用 `git show <提交>:<路径>` 或该提交的 Git 树读取。Fedora 任务私有归档 `historical-source-63849fc.tar.gz` 只覆盖前三个目录；它已经试解包并核对摘要，不是当前运行输入，也不进入 PR。

| 历史主题 | 原路径或入口 | 当前入口 |
|---|---|---|
| T00～T15 迁移回执、预检与补丁 | `migration/` | 活跃 PGV 范围及工具锁迁至 `tools/config/`；数据库结构迁移仍在 `migrations/` |
| 配额、AK/SK、GPU/Accelerator 验收日志 | `docs/evidence/` | 当前合同见 `docs/contracts/`、接口登记和 Actions artifact；旧日志只代表对应 SHA 与环境 |
| tx7do 模块和 Buf 来源备份 | `third_party/tx7do/` | 维护副本内的 LICENSE/NOTICE 与根 `THIRD_PARTY_NOTICES.md`；Buf 精确许可归属仍未核验 |
| 旧 Model/Network/GPU 实验与日期计划 | `scripts/model-lab/`、`scripts/network-lab/`、`scripts/quota-lab/`、日期命名计划 | `scripts/experiments/` 保留当前可参数化的合同入口；旧实验不能直接在现环境执行 |
| 旧混合 CHANGELOG | `docs/history/legacy-changelog.md` | 原文见 `git show 9ba13ad933b66e8f675f7eb9519123abcdb12e8f:CHANGELOG.md` |

旧 R6 固定 dump 未恢复，状态仍是 `NOT_VERIFIED`。历史日志中的通过状态不能升级为当前部署或跨仓验收。当前开发从 [开发指南](../development.md) 与 [仓库规则](../contributing/repository-hygiene.md) 开始。

本轮退出 HEAD 的旧部署结果、邀请审计报告、主机安装/PM2 与故障演示 SQL 可从固定基线 `0b06a893dc26f457434a50b3f878799dfb7fa23c` 的 Git 树按原路径查阅；其中历史判断只对应原时点，不是当前功能结论。

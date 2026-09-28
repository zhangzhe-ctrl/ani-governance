# 文档导航

| 当前入口 | 用途 |
|---|---|
| [开发指南](development.md) | 工具、生成、测试与证据边界 |
| [仓库卫生规则](contributing/repository-hygiene.md) | 测试、脚本、CI、归档和例外处理 |
| [执行规则](../AGENTS.md) | 授权、改动与安全边界 |
| [部署与初始化](deployment.md) | Atlas、初始化、运行配置 |
| [业务服务接入](service-integration.md) | 下游身份、装配与 API 登记 |
| [接口集成登记](interface-integration-register.md) | 现行接口与问题，不因归档自动结项 |
| [API 权限运维](api-role-permission-ops.md) | 目录同步、权限、套餐和策略 |
| [GPU owner 合同](contracts/gpu-owner-integration-guide.md) | 账本、释放、测试 owner 与真实接入界限 |
| [Governance/Accelerator 运维](operations/governance-accelerator-v1.2.md) | 运行操作和恢复边界 |
| [脚本导航](../scripts/README.md) | CI、运维和可选隔离实验 |
| [版本数据步骤](../sql/data/README.md) | quota expand、数据回填、constraints 的显式顺序 |
| [运维 SQL](../scripts/ops/sql/README.md) | 只读诊断与显式写入脚本的权限和目标边界 |

[历史索引](history/README.md) 给出退出 HEAD 的迁移回执、原始证据、第三方来源和旧实验的固定提交。当前工具配置在 `tools/config/`；数据库结构迁移在 `migrations/`。历史结果只适用于当时记录的源码和环境，不能代替当前验证。

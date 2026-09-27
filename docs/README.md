# 文档导航

## 当前使用入口

| 文档 | 用途 |
|---|---|
| [开发指南](development.md) | 工具准备、构建生成、开发合同与验证层次 |
| [执行规则](../AGENTS.md) | AI 和协作者的授权、修改与安全边界 |
| [部署与初始化](deployment.md) | Atlas、显式初始化、运行配置和业务验收 |
| [业务服务接入](service-integration.md) | 下游 mTLS、身份元数据、装配、API 登记 |
| [接口登记](interface-integration-register.md) | 现有接口与经批准的调整，持续维护同一份登记 |
| [API 权限运维](api-role-permission-ops.md) | 目录同步、权限和策略操作 |
| [脚本导航](../scripts/README.md) | 开发、运维和隔离实验入口的使用边界 |
| [合同目录](contracts/) | 按相关业务合同阅读，不从历史教程猜测 |

## 专题与历史

`operations/` 为运维专题。带 `plan`、日期或 `evidence` 的文件是对应批次的计划/记录，不自动成为今天的环境或完成结论；定位时查看实际提交和状态。

[历史目录](history/README.md) 保存旧混合变更日志。`local-integration.md`、日期命名的部署记录、`evidence/` 和迁移回执保留原事实，不按当前名称重写。

当前开发命令以 [development.md](development.md) 和实际 Makefile/工具代码为准；专题文档中的历史主机、版本或任务参数先核对，不直接复制到业务环境。发现文档与源码不一致时，记录具体差异，不能通过擅改业务代码“让教程正确”。

[第三方来源及许可](../THIRD_PARTY_NOTICES.md) 保留。`../migration/` 还含活跃工具配置，`../migrations/` 才是数据库结构迁移目录；不因整理导航移动这两个目录。

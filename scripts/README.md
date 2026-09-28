# ANI Governance 脚本入口

日常开发及验证先读 [开发指南](../docs/development.md) 与 [执行规则](../AGENTS.md)。脚本在当前授权主机执行，存在本身不授权部署或数据库操作。

| 范围 | 入口 |
|---|---|
| CI 检查、集成、生成 | `scripts/ci/`；对应 `make test-unit`、`make test-integration`、`make check-generated` |
| 脚本负对照 | `scripts/tests/`；Python unittest 与 Shell 安全测试 |
| 正式生成 | `make api`、`make gow`、`make tools-integration`；版本见 `tools/config/tool-lock.json` |
| 显式数据库运维 | `scripts/atlas.sh`、`scripts/backup/`、`scripts/ops/sql/` |
| 可复用开发工具 | `scripts/dev/new-service-scaffold.sh`、`scripts/dev/lab/` |
| 可选隔离实验 | `scripts/experiments/aksk-vpc/`、`scripts/experiments/gpu-contract/` |
| 部署材料 | `scripts/deploy/`；另按部署授权执行 |

旧 Model/Network 实验、GPU simulator 和一次性迁移清理脚本已退出 HEAD。历史来源固定在 Git 提交 `63849fc4cde38b879184a8fea4a6f539e60063e1`；旧主机与命名空间参数不作为当前默认值。

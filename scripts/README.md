# ANI Governance 脚本导航

日常开发统一见 [本仓开发指南](../docs/development.md)，执行边界见 [AGENTS.md](../AGENTS.md)。所有命令先核对工作目录和操作对象；脚本存在不等于适合在当前环境执行。

| 类别 | 入口与边界 |
|---|---|
| 完整 API | 根 `make api` / `tools/bin/gow api`；调用已锁定暂存链，不直接跑单模板代替全链 |
| 本地工具 | `build-redact-plugin.sh`、根 `make gow`，从当前源码构建 |
| 独立文档 | 根 `make openapi`，重新生成后再处理；不对最终 YAML 单独重复后处理 |
| Ent/SQL | 根 `make ent` 或指南中的显式两步；与数据库应用迁移不同 |
| 命令接线回归 | `tests/check-repo-entrypoints.py` / `make check-repo-entrypoints`；只在临时模拟树执行 |
| 脱敏集成 | `make tools-integration`，需固定 protoc，不因缺工具 skip |
| 旧开发安装器 | `env/install_unix_dev.sh`、`env/install_windows_dev.ps1`、`make install-dev` 已退役，副作用前退出 |
| 其他主机/运维安装器 | 不属于推荐开发流程；`env/install_unix_prod.sh` 等仍按独立授权审阅，不因本次整理声称已验证 |
| Atlas | [deploy/atlas/](deploy/atlas/)、`atlas.sh`，显式数据库结构操作；不在服务启动中执行 |
| 备份与恢复 | [backup/README.md](backup/README.md)，只操作已确认的目标，不随开发任务执行 |
| 可选部署 | `deploy/pm2_service.sh`、`deploy/sse/`，包含编译或服务/网络操作，先审阅前提 |
| 服务接入 | [指南](../docs/service-integration.md)、`new-service-scaffold.sh`，骨架不代表装配和验收完成 |
| 隔离实验 | [lab/README.md](lab/README.md)、[network-lab/README.md](network-lab/README.md)，绑定环境和输入，不是通用生产命令 |
| 暂停的 Model 材料 | `model-lab/`、`generate-model-slice.sh`、`bootstrap-model-access.sql` 保留作重接基线，未重接前不可运行 |
| 旧产物清理 | `post-generate-clean.sh` 已退役；不通过回退/删文件掩盖生成差异 |
| 来源归档 | `backup-tx7do.py` 按 [来源说明](../third_party/tx7do/README.md) 使用，不是当前工具安装方式 |

单模板/切片生成只用于其明确的输入与输出范围。不能从旧教程直接执行安装上游 redact 或使用任意 latest 插件。共享脚本库中的通用函数保留，不代表其环境修改功能获得本次执行授权。

运行时凭据、业务数据库、证书和历史备份不进入开发文档或 Git；各项命令的实际验证范围以当前 PR 和相应原始证据为准。

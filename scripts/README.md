# 后端脚本指南

所有命令从仓库根目录执行，并遵守当前任务指定的执行环境。镜像构建使用根 `Dockerfile`，数据库、Redis 和部署配置由目标环境提供。

| 入口 | 用途与边界 |
|---|---|
| `env/install_unix_dev.sh` | Unix 开发工具安装，会修改主机环境；使用前检查工具版本及安装范围。 |
| `env/install_unix_prod.sh` | Unix 运行工具安装，不等于应用已部署。 |
| `env/install_windows_dev.ps1` | Windows 开发工具安装；执行前审阅安装范围。 |
| `deploy/pm2_service.sh` | 可选 PM2 部署，包含编译、复制配置和服务管理。 |
| `deploy/sse/` | 可选 SSE 反向代理，按目标网络配置后端地址。 |
| [backup/README.md](backup/README.md) | PostgreSQL 备份及恢复说明。 |
| `generate-model-slice.sh`（**已暂停**）、`generate-network-slice.sh` | 固定范围的 API 生成脚本。model 接入已于 2026-09-21 暂摘，`generate-model-slice.sh` 保留作重接基线，重接前不可运行。 |
| [post-generate-clean.sh](post-generate-clean.sh)（**已退役**） | 原用于在 `make api` 之后 `git checkout --` 还原生成漂移并删除空壳 `*.pb.validate.go`。`make api` 已收敛为委托 `tools/bin/gow api`（先校验插件与输入、在隔离暂存副本中生成、成功后按受管清单写回）再接 `make openapi`，实测从干净检出逐字节复现既有产物，因此不再需要事后改动正式产物；脚本现为拒绝执行并指向核对命令。 |
| `bootstrap-network-access.sql`、`bootstrap-model-access.sql`（**已暂停**） | 专项权限登记，按脚本前提使用，不是通用种子。model 侧脚本因接入暂摘保留作重接基线，登记的 `/api/v1/models` 已下线。 |
| [../docs/service-integration.md](../docs/service-integration.md) | 业务服务接入指南：mTLS 出站、身份 header 契约、装配锚点、Api 登记与验收清单。 |
| [new-service-scaffold.sh](new-service-scaffold.sh) | 接入骨架生成（只写四个源文件，不执行生成/构建；生成、编译与验收按仓库执行环境约定运行）。 |
| [model-lab/README.md](model-lab/README.md)（**已暂停**）、[network-lab/README.md](network-lab/README.md) | 历史隔离实验入口，包含特定环境和验收前提。model-lab 因接入暂摘而不可运行，保留作重接基线。 |
| [lab/README.md](lab/README.md) | 当前隔离实验镜像构建：服务镜像 / 运维 CLI 镜像 / Atlas 镜像（可选 network），导入 kind。 |
| [deploy/atlas/](deploy/atlas/) | 独立 Atlas 迁移镜像的 Dockerfile 与构建脚本；应用镜像不含 Atlas。 |
| `backup-tx7do.py` | 源码备份及恢复校验，见 [备份说明](../third_party/tx7do/README.md)。 |

## 常用开发命令

```bash
gow run admin          # 先配置目标环境依赖及服务配置
gow api                # Proto 改动后生成
gow ent admin          # Ent schema 改动后生成
make openapi
make build_only        # 编译现有源码，不触发生成
```

`make docker` 是镜像构建入口，不负责启动数据库或部署应用。执行部署、备份恢复和实验脚本前核对目标环境；不能把脚本存在或构建成功当作运行验收。

当前工作流见 [WORKFLOWS_AND_BEST_PRACTICES.md](WORKFLOWS_AND_BEST_PRACTICES.md)，开发规则见 [AGENTS.md](../AGENTS.md)。

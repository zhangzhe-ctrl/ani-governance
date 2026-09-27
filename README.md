# ANI Governance

ANI 独立维护的 Go 后端，基于 [go-wind-admin](https://github.com/tx7do/go-wind-admin) fork 演进。后续开发和发布由本仓库维护，不再跟随 go-wind-admin 主线。

仓库仅保留后端，Go 模块位于根目录。模块路径保持 `go-wind-admin`，运行服务名为 `ani-governance`。现有实现包括 admin 服务、认证授权、租户与套餐、审计、任务/SSE，以及 Network 接入（Model 接入已于 2026-09-21 暂摘，待重接）。现有代码不等于全部功能或 ANI 域迁移已经验收。

## 开发

Go 基准 **1.26.7**，gow 固定 **v1.0.3**。主路径使用 Kratos、Ent、PostgreSQL 和 Redis；其他依赖按启用模块配置。

固定版本的领域 API 模块（如 `ani-network-service`）直接依赖上游 GitHub 固定版本；`GOPROXY` 需把 `proxy.golang.org` 放在前面或走 `direct`（`goproxy.cn` 对这些模块可能返回 `not found`），不要自建 file-GOPROXY 交付。`third_party/tx7do/` 仅覆盖其清单所列依赖，不是全项目离线依赖包。

先读 [AGENTS.md](AGENTS.md)。以下命令均从仓库根目录执行，并遵守任务指定的本地/远程执行边界：

首次部署先按 [部署与初始化流程](docs/deployment.md) 完成 Atlas 迁移和显式初始化，再启动服务。与业务服务（Network 等）的历史联调记录见 [docs/local-integration.md](docs/local-integration.md)。

```bash
make gow            # 从 tools/localdeps/gow 构建本仓在用的 gow（api/ent/run/version）
export PATH=$PWD/tools/bin:$PATH   # 之后可直接用 gow；也可写成 tools/bin/gow
gow run admin
gow api
gow ent admin
make tools-integration   # protoc 驱动的 redact 集成用例（需固定 protoc）
make openapi
make build_only
```

`make build_only` 仅编译已有源码，`make build` 还会生成 API/OpenAPI。修改 Proto、schema 后重新生成；`wiring_ent.go` 手写维护，不使用 Wire。

各功能对接涉及的接口统一维护在 [功能对接与接口风格改动登记](docs/interface-integration-register.md)，每次对接新功能先追加接口排查结果，待用户指定批次后统一调整风格，由用户确认何时结项。

## 配置与部署

配置样例在 `app/admin/service/configs/`；HTTP 默认 `7788`，可选 SSE `7789`。运行前配置数据库、Redis、密钥和领域服务地址，开发样例不作为生产参数。

镜像构建入口是根 `Dockerfile`。运行所需的数据库、Redis 等依赖由目标环境提供；部署配置由对应环境维护。可选 PM2/SSE 代理在 `scripts/deploy/`，数据库备份脚本在 `scripts/backup/`，使用前核对目标环境和脚本范围。现有脚本见 [脚本指南](scripts/README.md)。

服务启动不迁移、不播种、不同步 API。结构由 `migrations/` + Atlas 管理；数据通过 `make build_admin` 构建的 `bin/admin init` 显式初始化，种子 SQL 在 `sql/bootstrap/001_initial.sql`。`admin sync-apis --dry-run` 预览目录差异，显式同步保留 API ID 和权限关联。完整命令、首管理员、基础套餐及登录验收见 [部署与初始化流程](docs/deployment.md)。构建、测试、部署、恢复和生产切换分别提供证据，未执行的验收为 `not_verified`。

## 依赖与来源

依赖由 `go.mod` / `go.sum` 管理。`third_party/tx7do/` 用于依赖源码备份，覆盖范围和校验信息以实际清单为准；备份不自动改变 Go 的依赖消费方式。

原项目版权与 MIT 许可全文保留在 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。依赖源码中的许可声明也应随备份保留。

# 后端工作流

## 开发

1. 阅读根 [AGENTS.md](../AGENTS.md)，确定改动范围及执行环境。
2. 使用 Go 1.26.7、gow v1.0.3；按 [README.md](../README.md) 配置 Model/Network 固定 API 模块的交付代理。
3. 在目标环境准备数据库、Redis 及所需领域服务，配置 `app/admin/service/configs/` 和 mTLS 材料。
4. 修改源码；只有 Proto 或 schema 改变时执行对应生成，再编译受影响入口并运行定向测试。

所有开发命令从仓库根目录执行。`make build_only` 编译现有源码，`make build` 会先生成 API/OpenAPI；二者不能混同。

## 部署与恢复

使用根 `Dockerfile` 构建镜像，由目标环境提供运行配置和外部依赖。可选 PM2 与 SSE 代理入口见 [脚本指南](README.md)，不默认执行环境安装或部署脚本。

已有数据库的迁移、Api 登记、授权链和恢复分别验收。PostgreSQL 备份与恢复遵循 [备份说明](backup/README.md)，源码依赖恢复遵循 [tx7do 说明](../third_party/tx7do/README.md)。

## 实验与证据

Model/Network lab 脚本绑定历史环境、输入和基线；先读对应 README，不把它们当作通用部署命令。新提交的构建、测试和运行证据分别记录，未执行项目标记 `not_verified`。

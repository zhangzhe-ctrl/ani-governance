# 部署初始化验证（2026-09-21）

验证在 Ubuntu 的独立目录、专用 PostgreSQL 18 / Redis 7 容器中执行；没有修改已部署环境。

下表记录部署初始化阶段的历史验收，当时登录仍要求验证码和 AES 编码。后续已去掉两者并补齐租户登出授权；后续版本的定向测试、构建结果及未验证范围见 [功能对接与接口风格改动登记](interface-integration-register.md)。表中的真实 HTTP 验收和 60 文件哈希记录不代表后续改动已重复完成同样验收。

| 检查 | 结果 |
|---|---|
| 服务端与 admin 工具构建 | pass，Go 1.26.7 |
| Atlas 初始迁移应用到空库 | pass，版本 `20260921134442`，1198 条 SQL |
| Atlas wrapper 的 status / apply --dry-run / diff | pass，迁移目录与 Ent 目标结构无差异 |
| 初始化失败回滚 | pass，缺失 API 时用户、角色和 API 登记均不留下部分结果 |
| 实际 ID 绑定 | pass，用户、角色、权限序列从非 1 值开始仍能完整绑定 |
| 初始化重跑 | pass，已有数据行、密码和关联不变 |
| API 增量同步 | pass，保留 ID、禁用状态、权限关联；新 API 未自动授权 |
| 禁止启动迁移、构造函数无数据库访问 | pass，定向测试 |
| 两次真实服务启动 | pass，数据库连接强制只读；全部 `sys_*` 表逐行快照不变 |
| 平台登录 | pass，真实验证码、密码登录、HttpOnly 刷新 Cookie |
| 登录后的管理入口 | pass，个人资料、初始化上下文（非空菜单及权限）、套餐查询 |
| 租户最小闭环 | pass，绑定基础套餐建租户、直接激活管理员、租户登录、用户列表和创建用户 |
| 租户访问平台套餐 API | pass，返回 403 |
| 相关 Service / 邀请 / 目录映射回归 | pass，定向运行，没有运行全量测试 |
| 本地与远端验收源码 | pass，60 个 Go / SQL / YAML / HCL 文件 SHA-256 一致 |

真实链路发现并修复了 `GetInitialContext` 已注册但没有实现的问题。验收也确认：创建用户必须提交已有角色的 `role_ids`；省略角色会返回 400，不能把该结果当成部署授权失败。

复现核心 PostgreSQL 回归需提供**全新、可丢弃、库名以 `_bootstrap_test` 结尾**的数据库：

```bash
GOV_BOOTSTRAP_TEST_DSN="$TEST_DATABASE_DSN" go test ./app/admin/service/internal/data \
  -run 'Test(BootstrapPostgres|EntClientRejectsStartupMigration)$' -count=1
go test ./sql/bootstrap ./app/admin/service/internal/service ./pkg/constants \
  -run 'Test(ConstructorsDoNotAccessDatabase|CatalogValidation|ServiceTagToBusinessModuleExactMapping)$' -count=1
```

测试中的 `Schema.Create` 仅用于临时测试库；实际部署路径另以 Atlas 建库后运行服务验证。

本次 Atlas 工具实际版本：`v1.3.4-0e30359-canary`，二进制 SHA-256：`935fe7be0a51c53fd099e2a4c54df3709fe05d6e3664031bd721f4ef05f6fd3d`。这是本次工具记录，不代表生成工具链全部已锁定；迁移 SQL 和 `atlas.sum` 已保存。

远端证据目录：`/home/ubuntu/Workspace/.codex-runs/bootstrap-20260921/`，包括 `final-tests.log`、`atlas-final-apply.log`、`atlas-drift.log`、`runtime-final.log`、`http-final.log` 和 `verified-source-sha256.json`。`runtime-final.log` 保留首次 HTTP 验收缺少 `role_ids` 的失败，修正请求后的最终结果见 `http-final.log`；测试密码和令牌不进入本报告。

未验证：已有生产数据库的 Atlas 接管和恢复、Docker 镜像构建、前端浏览器、真实邮件投递。当前交付是源码和隔离环境验证，不是生产部署。

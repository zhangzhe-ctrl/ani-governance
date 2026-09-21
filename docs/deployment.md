# 部署、初始化与 API 管理

固定顺序：**Atlas 迁移结构 → 显式初始化数据 → 检查 → 启动服务 → 登录/API 验收**。
服务启动不建表、不播种、不同步 API、不恢复默认密码。配置 `data.database.migrate` 必须为 `false`；旧配置为 `true` 时明确报错。

本次隔离环境的实际结果见 [验证记录](deployment-verification.md)。

## 1. 准备配置和制品

构建环境使用 Go 1.26.7，从仓库根目录执行：

```bash
make build_only
make build_admin
```

服务使用 `app/admin/service/configs/` 的配置结构。部署时提供独立配置目录，至少核对：

| 配置 | 要求 |
|---|---|
| `data.database` | PostgreSQL 地址、库名、账号；`migrate: false` |
| `data.redis`、`server.asynq.uri` | 两处都指向可用 Redis，密码正确 |
| `auth`、加密配置 | 使用部署环境自己的密钥，多副本保持一致 |
| `authz.type` | 当前主装配要求 `casbin` |
| `server.rest.addr` | 默认 7788；按实际监听地址与反向代理配置 |
| CORS / Cookie | 优先同域代理；跨端口开发需正确 Origin、凭证设置；代理 HTTPS 传递正确协议 |
| Network | 未接入时不设置 `ANI_NETWORK_ADDR`；接入时另配 mTLS 和下游数据 |

迁移账号负责结构变更；服务运行账号只需业务表/序列的必要读写权限，不授予建表、改表权限。Atlas、初始化命令和服务必须连接同一个目标数据库。

## 2. 数据库结构由 Atlas 单独执行

`migrations/` 是结构迁移目录，包含本次从 Ent schema 生成的初始基线及 `atlas.sum`。种子数据不放在结构迁移里。Atlas 的标准版本迁移流程见 [Ent 官方说明](https://entgo.io/docs/versioned-migrations/)。

由部署环境注入 `ANI_DATABASE_DSN`，不要把真实连接串提交到仓库。安装 Atlas 后，从根目录运行：

```bash
./scripts/atlas.sh migrate status
./scripts/atlas.sh migrate apply --dry-run
./scripts/atlas.sh migrate apply
```

`scripts/atlas.sh` 固定运行目录，避免 Ent schema 位于 Go `internal` 目录导致加载失败。

开发者修改 schema 后，先完成 Ent 生成，再用**独立、可清空的开发数据库**生成下一份迁移：

```bash
# ANI_ATLAS_DEV_DSN 指向独立开发库，不能指向待升级的数据库。
./scripts/atlas.sh migrate diff change_name
```

审查生成 SQL 和恢复方案，连同 `atlas.sum` 提交。部署只执行已审查的迁移，不在启动时生成迁移。

**已有数据库不能直接套初始建表 SQL。** 先备份，在副本上对比现有结构和基线；完全匹配后才能按 [Atlas baseline 流程](https://atlasgo.io/versioned/apply#existing-databases) 建立版本记录。存在差异时先编写明确的迁移。不能仅用 `--allow-dirty` 跳过接管判断。

## 3. 首次数据初始化：有种子 SQL，密码由部署传入

种子 SQL 为 `sql/bootstrap/001_initial.sql`，通过 `admin init` 执行：

```bash
# 文件由部署环境或 Secret 提供，只包含密码；限制文件读取权限。
./bin/admin init --username admin --password-file /run/secrets/governance-admin-password
./bin/admin check
```

`admin` 只连接 PostgreSQL，不需要 Redis、邮件或其他业务服务。密码须满足当前初始策略：至少 8 位、至少三类字符，并受 bcrypt 长度限制。没有内置默认管理员密码。

首次初始化在一个事务内完成：

- 从随二进制发布的 OpenAPI 登记 API。
- 创建平台管理员、启用的密码凭证、平台角色及其权限关联。
- 创建租户管理员模板、权限和菜单。
- 创建“基础管理”套餐，开放 `DASHBOARD`、`OPM`，覆盖后台入口、个人资料与租户用户管理。
- 创建基础语言、口令策略参数，最后记录初始化完成标记。

关联按角色/权限代码、菜单名称、API `(method,path)` 查找，使用数据库实际生成的 ID。种子没有价格、下游资源配额或自动创建的业务租户。

初始化失败整体回滚；重复执行已完成的初始化不改任何业务行，不覆盖密码、套餐或管理员后续配置。无完成标记但已有用户/角色等数据时会拒绝重新播种，避免把历史数据库当空库覆盖。

`admin check` 和服务启动时的检查都是只读检查：确认存在启用的平台账号、凭证、后台访问权限及必要管理 API 授权。它不能代替实际密码登录、租户隔离或下游服务验收。

## 4. 启动与最小业务闭环

```bash
# 开发环境（默认配置已正确填写）
gow run admin

# 制品或镜像内的服务进程
/app/bin/server -c /run/governance/configs
```

根 Dockerfile 同时包含 `/app/bin/server` 和 `/app/bin/admin`；默认命令只启动服务。初始化单独以一次性作业或运维命令执行，不放在每个副本的启动命令中。

先用首管理员登录平台：`POST /api/v1/auth/platform/password/login`。登录不再需要图形验证码或验证码请求头；客户端在 JSON 的 `password` 字段直接提交用户输入的密码，不做 AES/Base64/bcrypt 编码，通过 HTTPS 访问部署入口。接口报文和待调整事项统一见 [功能对接与接口风格改动登记](interface-integration-register.md)。不要把“数据库里是 bcrypt”误解为“客户端发送 bcrypt”。

登录后至少验证 `GET /admin/v1/me`、`GET /admin/v1/initial-context`、`GET /admin/v1/plans`，确认能访问管理接口。

然后读取“基础管理”套餐的实际 `id`，通过 `POST /admin/v1/tenants:with-admin` 创建租户及管理员：

```json
{
  "tenant": {"name": "示例租户", "code": "example", "status": "ON", "plan_id": 123},
  "user": {"username": "manager", "status": "NORMAL"},
  "password": "由创建者安全提供的初始密码",
  "activation_mode": "IMMEDIATE"
}
```

`123` 必须换成查询得到的套餐 ID；`subscription_plan` 文本不能替代 `plan_id`。`IMMEDIATE` 为默认模式，直接启用账号，不需要邮件。需要邀请邮件时按 [账号开通说明](onboarding.md) 使用 `EMAIL_INVITATION`。

租户登录使用 `POST /api/v1/auth/password/login`，`tenant_name` 填租户的 **code**（示例为 `example`）。登录后验证个人资料、初始上下文、用户列表和租户内创建用户。创建用户的请求还必须包含实际的 `data.role_ids`（先从 `/admin/v1/roles` 查询）及明确提供的密码；缺少角色会返回 400。平台租户 ID `0` 不代表可以直接访问任意下游租户资源。

## 5. API 的来源、登记和授权分开管理

| 内容 | 来源与变更方式 |
|---|---|
| 路由、方法、操作名称 | Proto/BFF 生成的 OpenAPI，与服务二进制一起发布 |
| API 对应业务模块 | `pkg/constants/module_mapping.go`，缺映射时同步报错 |
| 数据库 API 目录 | 显式增量同步，以 `(path,method)` 匹配，保留 ID 和启停状态 |
| 权限 → API、角色 → 权限 | 首次种子中的明确清单，后续通过管理入口或单独审查的数据 SQL 修改 |
| 租户允许使用的模块 | 租户 `plan_id` 对应的 `sys_plan_modules` |

升级新增接口时：

```bash
./bin/admin sync-apis --dry-run
./bin/admin sync-apis
./bin/admin check
```

预览只读；应用不清表、不重新编号、不删除旧 API、不改权限关联。代码已删除的 API 会显示 `REVIEW`，保留到人工审查其授权关系后再明确清理。相同 `(path,method)` 已有多行会报冲突，不猜测应该保留哪一行。

新增 API 默认没有任何权限绑定，平台管理员也不因目录同步自动获得新权限。按实际接口用途选择权限代码，并检查目标租户套餐包含相应模块。修改已有权限的 API 列表时保留原关联，使用明确 `update_mask`；不要把一次新增误写成替换整组权限。

服务内原有 `SyncApis` 入口已使用同一增量实现。`SyncPermissions` 是另一个会重建派生业务权限的功能，**不是发布流程的一步**。

通过 CLI/SQL 改数据后重启服务实例，使内存中的 Casbin 策略刷新；启动只读取策略。管理 API 入口按现有逻辑刷新当前实例，不能据此声称其他实例的策略已同步。

### 补齐旧库的租户登出授权

2026-09-21 已在首次种子中补上 `sys:tenant_manager` → `POST /api/v1/auth/logout`。用旧种子初始化的数据库，需要显式执行一次数据补丁；重跑 `admin init` 不会修复已初始化的库。

使用目标库连接配置执行（例如按环境设置 `PGSERVICE` 或 `PGHOST` / `PGDATABASE` / `PGUSER`，密码使用凭据文件）：

```bash
psql -v ON_ERROR_STOP=1 -f sql/patches/20260921_tenant_logout.sql
```

补丁只补这一条权限关联，按权限 code 和 API 方法/路径查找，保留现有 ID；重复执行不重复插入。缺少或重复的前置权限/API 会报错并回滚，不自动新增 API、不启用被停用的账号/权限/接口，也不放宽套餐。

执行后重启所有服务实例以重载 Casbin 策略，再用启用且套餐包含 DASHBOARD 的租户管理员验证登录、登出及旧令牌失效。这里的重启只读数据库，不会自动执行补丁。

## 6. 日常升级与排错

日常升级：备份 → Atlas 迁移 → API 差异预览/登记 → 明确的权限或套餐变更 → `admin check` → 启动/重启 → 实际登录和目标 API 验收。不要重跑首次种子来补新版本数据。

| 现象 | 优先核对 |
|---|---|
| 启动报禁止迁移 | 把旧 `migrate: true` 改成 `false`，先单独完成 Atlas 迁移 |
| 表不存在 | Atlas 是否连接同一库、迁移是否全部成功 |
| 启动前检查不通过 | 新库是否执行 `admin init`；旧库账号/凭证/角色/API 关联是否完整 |
| 登录失败 | 平台/租户入口、租户 code、是否仍误发 AES/Base64 密文、账号/凭证状态 |
| 登录成功但 API 403 | 该方法和模板路径是否登记；角色权限是否绑定；租户状态、plan_id、模块白名单 |
| 重启后权限表现不同 | 是否曾只改过某实例内存策略；CLI/SQL 更新后是否刷新所有实例 |

“基础管理”套餐不包含 Network 等下游模块；需要时明确增加套餐模块和接口权限，并单独验收下游接线。进程启动、只读检查、真实登录、目标 API 成功分别记录，不能互相替代。

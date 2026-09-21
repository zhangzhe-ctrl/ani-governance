# 功能对接与接口风格改动登记

总体状态：**进行中，未结项**。最后更新：2026-09-21。

本文件覆盖所有后续对接功能。每次用户对接一个新功能时，AI 先排查真实调用链，将涉及的接口、请求和响应、鉴权条件、现有问题追加到对应功能组。先累计登记，等用户指定某个批次后，再统一修改该批接口的路径、字段和响应风格；不能因排查或登记就自动改接口风格。

后续始终维护本文件，不按功能或对话另起登记文件。复用接口引用已有编号，避免重复登记；新功能使用自己的稳定编号。用户明确要求的即时功能修复可单独实施并记录，不必等待风格批次。尚未提供的旧项目格式标为“待指定”；只有用户确认全部改动完成后，整体登记才结项。

## 功能登记索引

| 功能组 | 接口编号 | 排查进度 | 风格改动批次 |
| --- | --- | --- | --- |
| 登录、登出及登录后初始化 | AUTH-01～AUTH-08；相关可选接口 AUTH-09～AUTH-14 | 已排查；已按单独要求去掉登录图形验证码 | 待用户分批，尚未统一改风格 |

## 风格改动批次

当前没有已指定的批次。后续由用户指定涉及的接口编号及目标风格，在此追加批次、实施状态与验证结果；单个批次完成不代表整体登记结束。

## 功能组：登录、登出及登录后初始化

### 本次改动

用户已明确要求将平台和租户登录改为直接提交密码。本次作为即时改动实施，不等待统一风格批次；具体调用方式与发布边界见下文。

平台和租户密码登录均已移除图形验证码检查。不再需要先获取/验证图片验证码，也不需要 `X-Captcha-Id`、`X-Captcha-Value`；旧客户端即使发送这两个请求头，登录也不读取。没有新增开关，不需要数据库迁移或 API 目录同步；部署新服务二进制后生效。

独立的两个图形验证码接口暂保留兼容，已退出登录流程。账户启用的 MFA、找回密码邮件验证码是不同功能，此次没有改动。

### 当前接口清单

下表为当前已注册的 HTTP 路由，不是计划中的接口。请求体直接传 JSON，不套 `data`；GET 无请求体。受保护接口使用 `Authorization: Bearer <access_token>`，还受现有角色权限与租户套餐检查约束。

| 编号 | 方法与路径 | 请求 / 认证 | 当前响应或作用 | 目标风格 / 状态 |
| --- | --- | --- | --- | --- |
| AUTH-01 | `POST /api/v1/auth/password/login` | `tenant_name`、`username`、`password`；无需 Bearer、无需图形验证码 | 租户登录，返回下述令牌报文并设置 Cookie | 验证码和登录 AES 编码已移除；路径/字段待指定 |
| AUTH-02 | `POST /api/v1/auth/platform/password/login` | `username`、`password`；无需 Bearer、无需图形验证码 | 平台登录，要求平台角色 | 验证码和登录 AES 编码已移除；路径/字段待指定 |
| AUTH-03 | `POST /api/v1/auth/logout` | Bearer；请求体 `{}` 即可；`jti` 可选 | `{"status":"revoked"}`；吊销该用户的全部后台会话并清 Cookie，非仅当前设备 | 路径/登出范围待指定；租户权限问题见 ISSUE-02 |
| AUTH-04 | `POST /api/v1/auth/refresh` | 携带 `refresh_token` Cookie；请求体 `{}`，可选 `client_id`、`device_id`；无需 Bearer | 返回新访问令牌，轮换 Cookie，旧令牌对失效 | Cookie 或其他风格待指定 |
| AUTH-05 | `GET /admin/v1/me` | Bearer | 当前用户 `identity.service.v1.User` 对象 | 路径/用户字段待指定 |
| AUTH-06 | `GET /admin/v1/initial-context` | Bearer | `menus`、`permissions`、`hiddenFields` | 登录后聚合初始化；路径/报文待指定 |
| AUTH-07 | `GET /admin/v1/routes` | Bearer | `items` 菜单路由树 | 单独获取菜单；与聚合接口择需调用 |
| AUTH-08 | `GET /admin/v1/perm-codes` | Bearer | `codes`、`hiddenFields` | 单独获取权限；与聚合接口择需调用 |

以下仅在对应功能使用时才需要接入，不是普通账密登录的前置步骤：

| 编号 | 方法与路径 | 当前用途 | 登记状态 |
| --- | --- | --- | --- |
| AUTH-09 | `POST /admin/v1/mfa/verify` | 已启用 MFA 的账号提交登录返回的操作 ID 与二次验证答案；返回旧版 `LoginResponse`，与 `TokenPairResponse` 不是同一报文 | 仅 MFA 登录中间态需要，报文风格待指定 |
| AUTH-10 | `POST /api/v1/auth/forgot-password` | 发找回密码邮件验证码 | 可选功能，未改 |
| AUTH-11 | `POST /api/v1/auth/reset-password-by-code` | 用邮件验证码重置密码 | 可选功能，未改 |
| AUTH-12 | `POST /api/v1/auth/invitations/accept` | 接受邀请、设置密码并激活；直接激活模式无需调用 | 可选功能，未改 |
| AUTH-13 | `GET /api/v1/auth/captcha` | 独立获取图形验证码 | 登录不再使用；接口是否删除待指定 |
| AUTH-14 | `POST /api/v1/auth/captcha/verify` | 独立验证图形验证码 | 登录不再使用；接口是否删除待指定 |

内部方法 `Login`、`WhoAmI`、`ValidateToken` 不等于存在对应公开 HTTP 地址；不要按方法名拼接口。

### 当前最小调用流程

1. 租户账号调用 AUTH-01；首管理员/平台账号调用 AUTH-02。`tenant_name` 实际是 `sys_tenants.code`，不是租户显示名称；租户登录漏传它会拒绝，不会自动改走平台登录。用户名允许 `local:` 前缀。
2. 保存响应中的访问令牌；由浏览器或 HTTP 客户端 Cookie jar 保存登录响应 Cookie。
3. 使用 Bearer 调用 AUTH-05、AUTH-06，取得个人资料、菜单与权限。用了聚合初始化后，不必同时调用 AUTH-07、AUTH-08。
4. 使用 AUTH-04 刷新会话并替换访问令牌。旧 refresh token 单次使用，客户端应避免并发刷新。
5. 使用有效 Bearer 调用 AUTH-03，成功后清理本地令牌和用户上下文。仅清前端缓存不代表服务端已登出。访问令牌已经过期时，当前登出接口会先被鉴权拒绝。

租户登录请求示意（密码占位符不能原样发送）：

```json
{
  "tenant_name": "your-tenant-code",
  "username": "manager",
  "password": "<用户输入的原始密码>"
}
```

平台登录去掉 `tenant_name`，使用平台登录路径。两者都不传 `grant_type`、`captcha_id` 或验证码头。

正常登录/刷新返回 `access_token`、`expires_in`（秒）和 `issued_at`。不要依赖响应体中的 `refresh_token`：当前适配层不填该字段。`expires_in` 在 Proto 中是 int64，客户端应兼容 JSON 字符串数值。若账号启用了 MFA，密码通过后返回 `mfa_required=true`、`mfa_operation_id`，此时没有有效访问令牌，应完成 AUTH-09。

当前密码处理链：用户输入密码 → 客户端直接放入 JSON 的 `password` → 通过 HTTPS 传输 → 服务端直接与数据库 bcrypt 哈希校验。登录不再进行 AES/Base64 解码，也不接收客户端 bcrypt 哈希。用户密码与数据库存储无需修改。

发布时前后端必须同步：客户端删除登录前的 AES/Base64 编码；旧客户端提交的 AES 密文会被当作密码本身校验，通常返回 `INVALID_CREDENTIALS`。没有按内容猜测编码方式的兼容分支。HTTPS 由部署入口/反向代理提供，本次没有自动配置域名或 TLS 证书。传输要求见 [OWASP 认证指南](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html#transmit-passwords-only-over-tls-or-other-strong-transport)。

本次只改平台和租户登录协议。修改密码、管理员重置密码等其他接口按各自现有合同处理，不能据此把客户端所有密码字段的编码一起删除。两个登录入口的审计不记录密码请求体，通用请求日志仍过滤 `args`。

Cookie 现状：`refresh_token` 为 HttpOnly，Path=`/api/v1/auth/refresh`、SameSite=Lax；`refresh_exp` 是前端可读的过期时间提示，Path=`/`。Secure 随 HTTPS / 可信代理转发协议判断。浏览器跨源请求需允许携带凭据，实际 CORS 与 Cookie 是否送达仍需按部署域名联调。推荐在当前联调中经同源代理访问；仅加 `credentials: include` 不会取消 SameSite 限制。

### 已发现的对接问题与待改项

| 编号 | 源码确认的现状 | 后续处理 | 状态 |
| --- | --- | --- | --- |
| ISSUE-01 | 原登录强制 AES 编码，与接口说明不一致 | 按用户要求改成 HTTPS 直接提交原始密码；更新源 Proto、生成代码/OpenAPI、登录用例及审计回归；数据库 bcrypt 不变 | 实现完成；登录/密码管理与审计定向测试、服务编译 PASS；前端与后端需同步发布，用户环境尚未部署 |
| ISSUE-02 | 原首次种子遗漏 `sys:tenant_manager` → `POST /api/v1/auth/logout` | 已补首次种子；旧库显式执行 [登出授权补丁](../sql/patches/20260921_tenant_logout.sql)，操作见 [部署流程](deployment.md)；仍保留租户状态、套餐与角色权限检查 | 实现完成；PostgreSQL 18 定向验证 PASS；用户环境尚未应用，HTTP 联调 not_verified |
| ISSUE-03 | 认证路径为 `/api/v1/auth/*`，个人资料/初始化为 `/admin/v1/*`；字段也有 snake_case 与 camelCase 混用 | 累计记录差异，待用户指定批次与旧项目目标格式后统一调整 | 待指定 |
| ISSUE-04 | 当前登出吊销用户全部后台会话，`jti` 仅审计用 | 用户指定保留全部登出还是改为当前会话 | 待指定 |
| ISSUE-05 | kind 实际联调发现租户数据库已有套餐，但读取 DTO 丢失 `plan_id`，初始化菜单被误判为无套餐而清空 | 将现有 nullable 外键显式绑定为 Ent 字段；重新生成代码，租户详情与列表可返回套餐 ID；Atlas 确认与现有结构无差异，无需迁移 | 租户仓储、登录与租户服务回归 PASS；kind NodePort 平台/租户登录、非空菜单、权限、刷新、登出验收 PASS；详见 [运行记录](deployment-kind-20260921.md) |

这些条目是现状与待讨论项，不表示已批准新增需求。既有数据库的数据变更必须显式执行；服务启动不迁移、不播种、不同步 API。

### 验证与变更记录

| 日期 | 改动 | 验证 | 对接验收 |
| --- | --- | --- | --- |
| 2026-09-21 | 按用户要求重新初始化 Ubuntu kind Governance；保留旧库，新库 Atlas + 显式初始化；修复租户套餐 ID 映射导致的空菜单；不调整前端 | 后端 NodePort 登录、菜单权限、刷新、平台/租户登出及失效检查 PASS；重启后 17 张初始化/业务表不变，详见 [运行记录](deployment-kind-20260921.md) | 本轮后端 NodePort 验收通过；前端、公网 HTTPS 与邮件未验收，整体接口风格登记仍未结项 |
| 2026-09-21 | 按用户要求取消两个密码登录入口的 AES/Base64 解码；同步 Proto、生成代码/OpenAPI 与部署说明；原 bcrypt 哈希及其他密码接口协议保留 | Ubuntu / Go 1.26.7：25 个 `TestAuthSvcSqlite_` 用例、2 个密码管理回归用例、`TestPasswordLoginAuditDoesNotPersistPassword` 与服务编译 PASS；含正确原始密码通过、旧 AES 报文拒绝、审计不记录密码；生成使用现有 buf 模板，Go 插件版本 v1.36.12 | 用户环境 HTTPS 配置、部署及前端联调 not_verified；需前后端同步发布；整体登记未结项 |
| 2026-09-21 | 按用户要求补齐租户管理员登出授权：新库种子新增关联，旧库提供独立 SQL 补丁；密码协议未改 | Ubuntu / Go 1.26.7 / PostgreSQL 18，`TestBootstrapPostgres` PASS：新库授权存在、模拟旧库缺项可修复、重复执行数据及 ID 不变、缺 API 前置条件时失败且不改数据 | 未执行用户环境补丁；租户 HTTP 登出联调 not_verified；整体登记未结项 |
| 2026-09-21 | 按用户澄清扩展为全功能登记，新增功能索引与待分批规则；登录组保留原编号 | 文档链接与差异检查通过，无运行代码变更 | 风格批次尚未指定，整体登记未结项 |
| 2026-09-21 | 移除共用密码登录逻辑中的验证码门槛、常量及校验函数；更新用例，使平台/租户成功登录都不提供验证码；建立本登记 | Ubuntu，Go 1.26.7，`go test ./app/admin/service/internal/service -run '^TestAuthSvcSqlite_' -count=1` PASS（25 个测试函数）；服务入口 `go build ./app/admin/service/cmd/server` PASS；覆盖无验证码、旧验证码头、错误凭证、限流、租户/平台角色、刷新轮换、登出吊销及独立验证码接口 | 用户当前环境部署与前端联调：not_verified；整体登记保持进行中 |

后续修改本功能组时更新对应 AUTH/ISSUE 条目与本表，区分“实现完成”“定向验证通过”“实际联调通过”“用户确认关闭”。测试使用 Ent SQLite 与 miniredis；本次不能据此声称 PostgreSQL HTTP 全链路或前端联调已通过。

### 源码入口

- [HTTP 认证路由](../api/protos/admin/service/v1/i_authentication.proto)、[登录与刷新报文](../api/protos/authentication/service/v1/ani_auth.proto)
- [个人资料路由](../api/protos/admin/service/v1/i_user_profile.proto)、[初始化路由和响应](../api/protos/admin/service/v1/i_admin_portal.proto)、[MFA 路由](../api/protos/admin/service/v1/i_mfa.proto)
- [认证业务与 Cookie](../app/admin/service/internal/service/authentication_service.go)、[密码解码与凭证校验](../app/admin/service/internal/data/user_credential_repo.go)
- [HTTP 鉴权装配](../app/admin/service/internal/server/rest_server.go)、[租户套餐检查](../app/admin/service/internal/data/tenant_access_checker.go)、[初始化权限 SQL](../sql/bootstrap/001_initial.sql)
- [定向测试](../app/admin/service/internal/service/authentication_service_sqlite_test.go)、[部署与初始化流程](deployment.md)

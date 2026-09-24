# 功能对接与接口风格改动登记

总体状态：**进行中，未结项**。最后更新：2026-09-22。

本文件覆盖所有后续对接功能。每次用户对接一个新功能时，AI 先排查真实调用链，将涉及的接口、请求和响应、鉴权条件、现有问题追加到对应功能组。先累计登记，等用户指定某个批次后，再统一修改该批接口的路径、字段和响应风格；不能因排查或登记就自动改接口风格。

后续始终维护本文件，不按功能或对话另起登记文件。复用接口引用已有编号，避免重复登记；新功能使用自己的稳定编号。用户明确要求的即时功能修复可单独实施并记录，不必等待风格批次。尚未提供的旧项目格式标为“待指定”；只有用户确认全部改动完成后，整体登记才结项。

前端交接入口：[用户、租户及两类管理员页面接入说明](#frontend-management-handoff)。该章节可整段转发，包含第一批接口、请求示例、权限、响应和暂缓项；沿用本文已有接口编号。

## 功能登记索引

| 功能组 | 接口编号 | 排查进度 | 风格改动批次 |
| --- | --- | --- | --- |
| 登录、登出及登录后初始化 | AUTH-01～AUTH-08；相关可选接口 AUTH-09～AUTH-14 | 已排查；已按单独要求去掉登录图形验证码 | 待用户分批，尚未统一改风格 |
| 用户管理 | ACCOUNT-01～ACCOUNT-09；复用 ROLE-01/02、AUTH-01～AUTH-08 | 前端交接见专节；管理侧更新须携带目标租户和有效角色；部分接口暂缓 | 待指定 |
| 租户管理 | TENANT-01～TENANT-09 | 已核对 HTTP、Service、Repository 和首次种子；本轮未做运行验收 | 待指定 |
| 租户管理员管理 | 复用 TENANT-06、ACCOUNT-01～ACCOUNT-09、ROLE-01～ROLE-05、AUTH-01/12；AUTH-15～AUTH-18（自助 /me） | 复用用户和租户角色；用户明确不做主管理员移交 | 待指定 |
| 套餐管理 | PLAN-01～PLAN-14；复用 TENANT-04/08 | 模块限制已有接线；数量配额只配置和统计 | 待指定 |
| 平台运营账号管理 | 复用 ACCOUNT-01～ACCOUNT-09、ROLE-01～ROLE-05、AUTH-02/12；AUTH-15～AUTH-18；SESSION-01～SESSION-04；PERM-01～PERM-06、PERMGROUP-01～PERMGROUP-05 | 支持多个平台账号；运营/只读角色模板待 API 接入后再加；自助会话与权限点/权限组接口本轮补登，缺口见 MGMT-08～MGMT-15 | 待指定 |
| API Key / AK-SK | AK-01～AK-07 | 签名、角色绑定、加密与可信审计已完成，真实 VPC 闭环 PASS | AKSK-VPC-20260922 已完成本批 |
| VPC 详情查询的机器调用 | NET-01；复用 AK-* | 必要 vpc-read 接收已整合，双 actor 与真实 mTLS 查询 PASS | AKSK-VPC-20260922 已完成本批 |
| Network 租户面读写对接（GOV-RESOURCE-20260922） | NET-02～NET-11；复用 AK-*、AUTH-* | BFF 路由/客户端/Service 已实现，17/17 全链路验收 PASS | GOV-RESOURCE-20260922 完成 |
| 通用配额与 GPU 本地模拟 | QUOTA-01～03、QUOTA-LAB-01～04；复用 PLAN-11～14、TENANT-04/08 | 本地模拟闭环已实现并通过指定验收；正式构建无 GPU 路由；真实 GPU 未接入 | QUOTA-GPU-LOCAL-01，本地验收完成（真实 GPU not_verified） |

## 风格改动批次

本批 **AKSK-VPC-20260922** 已由用户明确指定，覆盖 AK-01～07、AK-ISSUE-01～05、NET-01/NET-ISSUE-01：实现、远端定向测试、空库真实链路及必要负向验收均 PASS。证据见 [运行记录](evidence/aksk-vpc-20260922/README.md)。单个批次完成不代表整体登记结束，其他功能风格仍待指定。

本批 **GOV-RESOURCE-20260922**（进行中）：把 governance ↔ 下游资源服务（ani-resource-service，原 ani-network-service 改名）对接从单一 VPC 只读扩为**租户面读+写**。新增 BFF 路由（NET-02～NET-11）：`GET/POST /api/v1/networks/vpcs`、`DELETE /api/v1/networks/vpcs/{vpc_id}`、`GET /api/v1/networks/operations/{operation_id}`、`GET/POST /api/v1/networks/eips`、`DELETE /api/v1/networks/eips/{eip_id}`、`GET /api/v1/networks/vpcs/{vpc_id}/snat`、`POST /api/v1/networks/vpcs/{vpc_id}/snat/bindings`。权限码族 `network:vpc:list|create|delete`、`network:operation:get`、`network:eip:get|list|create|delete`、`network:snat:get|bind`（`bootstrap-network-access.sql` 已扩展）。下游统一走新的 `ANI_NETWORK_MODE=governance` 组合入口（mTLS、SAN、三头、actor 合同与 vpc-read 一致；resource 侧白名单按域分组，平台面与流式全部拒绝）。本批全链路验收未执行，状态以 [计划](../../ani-resource-service/docs/plans/governance-integration.md) 勾选为准。

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
| AUTH-15 | `PUT /admin/v1/me` | 本人资料自助更新；直接落 `userRepo.Update`，**绕过 `UserService` 的角色类型/租户校验**，携带 `roleIds`+`role_ids`　掩码可能自改角色（见 MGMT-11） | 已接线但此前未登记，本轮补登；路径/字段待指定 |
| AUTH-16 | `POST /admin/v1/me/password` | 本人自助改密；`oldPassword`/`newPassword` 走 AES 密文（与登录直接提交明文密码不同）；成功后吊销本人全部客户端令牌 | 已接线但此前未登记，本轮补登；协议/路径待指定 |
| AUTH-17 | `POST /admin/v1/me/contact` | 向本人待绑定邮箱发送验证码；当前只支持 EMAIL，不支持手机号。与 AUTH-18 配合建立邮箱凭证，可用于找回密码；邀请接受流程会自行建立邮箱凭证，不以本接口为前置 | 已接线；2026-09-22 前端交接复核更正用途；待指定 |
| AUTH-18 | `POST /admin/v1/me/contact/verify` | 校验本人联系方式验证码，与 AUTH-17 成对 | 已接线但此前未登记，本轮补登；待指定 |

`PUT /admin/v1/me`（AUTH-15）与账号组的 `ACCOUNT-04`（管理侧改用户）不是同一入口：后者经 `UserService` 校验角色类型/租户并要求至少一个角色，前者不经该校验。

内部方法 `Login`、`WhoAmI`、`ValidateToken` 不等于存在对应公开 HTTP 地址；不要按方法名拼接口。

### 当前最小调用流程

1. 租户账号（包括租户首管理员）调用 AUTH-01；平台账号调用 AUTH-02。`tenant_name` 实际是 `sys_tenants.code`，不是租户显示名称；租户登录漏传它会拒绝，不会自动改走平台登录。用户名允许 `local:` 前缀。
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


## 功能组：租户、管理员、套餐与平台运营账号

排查日期：2026-09-21，首次基于 `39008e4` 排查，随后与最新 `main`（`11fa857`，移除文件管理及对象存储）整合。首次排查只登记功能与接口。后续用户授权小范围修复账号停用/删除的会话吊销；不修改密码重置、接口风格或运行数据。上一轮 kind 验收覆盖租户及首管理员创建、平台/租户登录、菜单、套餐读取与登出；本表其余操作的本轮运行验收为 `not_verified`。

2026-09-21 增量复核（按用户要求重列租户管理、租户管理员管理、套餐管理、平台管理员管理四域接口）：逐一对照源码核对 `TENANT-01～09`、`ACCOUNT-01～09`、`ROLE-01～05`、`PLAN-01～14`，与既有登记一致，无错登（`misregistered` 为空）；补登在线会话自助两条路由 `SESSION-03/04` 及授权缺口 `MGMT-08`；并补登此前仅以"待对接"一句带过的**权限点/权限组**接口 `PERM-01～06`、`PERMGROUP-01～05` 与**自助 /me 面** `AUTH-15～18`（登记于登录功能组）；新增 `MGMT-09～MGMT-15`；并新增「鉴权边界（授权矩阵与租户隔离）」小节与 `MGMT-16～MGMT-18`（授权矩阵、令牌租户信任、SystemViewer 旁路、全局权限点）。本轮只做登记，未改任何接口、鉴权或运行数据。

2026-09-22 前端交接复核：在 `8b80461` 源码基线上补充四类页面交接说明；更正 AUTH-17 的邮箱/邀请依赖、平台 API Key 实际准入、MGMT-13 删除保护及 MGMT-16 的租户 0 描述；补充 ACCOUNT-06 平台用户名查重限制。此前“无错登”不代表本轮未发现描述问题。本轮仅改文档，没有修复后端或执行 HTTP 验收。

### 能力与身份边界

| 功能 | 已有实现 | 当前限制 |
| --- | --- | --- |
| 租户管理 | 列表/详情/查重、创建/编辑/删除；同时创建首管理员；设置套餐、有效期、启停/冻结状态；人数与用量查询；显式清理 Governance 本地数据 | 普通创建不自动建管理员；审核仅为状态字段；删除租户记录与清理数据是不同操作，没有下游资源统一销毁流程 |
| 租户管理员 | 开租户时复制管理员角色模板并绑定首管理员；也可通过用户接口增加本租户管理员、编辑资料/角色/状态、删除账号、重置密码；立即激活或邮件邀请 | `adminUserId` 是租户记录的主管理员指针，用户持有管理员角色是另一件事；用户已明确产品不提供主管理员移交，不再列为待实现项；现有源码未发现最后一个管理员保护 |
| 套餐 | 套餐增删改查；免费/标准/企业版本字段；模块白名单管理；用户数/存储/API 调用量配额配置；到期策略及数据保留天数字段 | 模块白名单已用于 API 和导航控制；没有统一的超配额拒绝逻辑；保留天数未找到自动清理消费方；不是订单/支付系统 |
| 平台运营账号 | 用户增删改查、分配系统角色、自定义角色及权限、状态修改、密码重置、在线会话查询与指定会话下线；默认管理员和本人有删除保护 | 账号 `tenantId=0`，角色类型 `SYSTEM`，平台登录要求至少一个 `platform:` 前缀角色并具有后台访问权限；种子只预置 `platform:admin`，无运营/只读角色模板 |

用户列表为共用接口：平台运营账号列表需明确筛选 `tenant_id=0`；租户管理员列表需筛选目标租户并结合管理员角色，不能把该租户所有用户都当管理员。租户登录上下文的创建操作会强制采用当前租户；平台上下文可指定目标租户。默认租户管理员授权包括用户管理和角色读取，不包括角色增删改及套餐/租户平台管理。

新增自定义平台角色可复用 `ROLE-*` 的 `permissions` 字段关联权限点；权限点里的 `menuIds`、`apiIds` 决定菜单与 API 授权。具体运营权限矩阵尚未指定，不能直接用 `sys:platform_admin` 充当“只读”。

### 鉴权边界（授权矩阵与租户隔离）

**授权矩阵**（首次种子 `sql/bootstrap/001_initial.sql`；行号取自两份**授权块**：平台块 `INSERT … WHERE p.code='sys:platform_admin'`（`313-497`，WHERE 在 `497`）、租户块 `WHERE p.code='sys:tenant_manager'`（`501-530`，WHERE 在 `532`）。该文件 `123-308` 是另一段"API 完整性校验目录"，**不是**授权块）。租户管理员实际有 **30 条**授权（`501-530`），其中 6 条为 2026-09-22 新增的 API Key 管理（`/api/v1/auth/api-keys*`，见 AK 功能组）。

| 接口族 | 平台管理员 | 租户管理员 | 平台块行号 / 租户块行号 |
| --- | --- | --- | --- |
| 租户 `/admin/v1/tenants*`（含 `:with-admin`、`:exists`、`/usage`、`/cleanup`） | 全 | **无** | 334,407-410,459-461,494 / 无 |
| 套餐 `/admin/v1/plans*`、`/plan-modules*`、`/plan-quotas*` | 全 | **无** | 328-330,389-393,449-451,488-490 / 无 |
| 权限点·权限组 `/admin/v1/permissions*`、`/permission-groups*` | 全 | **无**（仅 `GET /admin/v1/perm-codes` 只读权限码目录，515） | 326-327,385-388,446-448,486-487 / 无 |
| 角色 `/admin/v1/roles*` | 全 CRUD | 仅 `GET /roles`、`GET /roles/{id}`（只读） | 332,399-400,453,492 / 516-517 |
| API 目录 / 菜单 `/apis*`、`/menus*` | 全 | **无** | 314,322,341-343,370-371,418-419,435-436,474,483 / 无 |
| 在线会话 `/admin/v1/online-session/*`（含自助 `my-sessions`） | 全 | **无** | 376-377,443-444 / 无（见 MGMT-08） |
| 用户 `/admin/v1/users*`（建/读/改/删/重置密码） | 全 | **全**（限本租户） | 335-336,411-414,462-463,495 / 507-508,519-522,526-527,530 |
| 自助 `/admin/v1/me*` | 全 | **全** | 369,432-434,482 / 514,523-525,529 |
| API Key `/api/v1/auth/api-keys*`（2026-09-22 新增，见 AK 功能组） | 种子有绑定，但 Service 拒绝 `tenantId=0`，平台身份实际不可调用 | **全**（限本租户） | 313,337-338,417,472-473 / 501-506 |

权限码与绑定（`35-42`、`51-56`）：`platform:admin`（`tenant_id=0`，SYSTEM，受保护）绑定 `sys:access_backend` + `sys:platform_admin` 等 6 项；`template:tenant:manager`（`tenant_id=0`，TEMPLATE）只绑 `sys:access_backend` + `sys:tenant_manager`。`sys:access_backend` 是两角色共有的后台入口权限。

**租户隔离（运行时）**：请求主体 → ent viewer（`pkg/middleware/auth/auth.go:112-123`），user/tenant/orgUnit 均取自**校验后的主体**（JWT 或已验签的 AK/SK 主体，`auth.go:49-66`），不信任请求头；casbin domain 亦取主体租户（`utils.go:37-43`）。数据层由 `mixin.TenantID` + go-crud `TenantPrivacy`（查/建/改/删注入 `tenant_id=viewer`，跨租户改租户被拒）＋ 本仓 `TenantMutationGuardPolicy`（`ent/schema/tenant_mutation_guard.go:27-63`）双重约束。服务层再强制：`user_service.go:352-355`（强制 `TenantId=操作人租户`）、`370-395`（只允许**本租户 TENANT 类型**角色，否则 `some roles not found`）、`567-581`/`691-695`（跨租户改密拒绝，除非持 `sys:reset_others_credential`）、`role_service.go:160-162,194-196,208-210`（强制 `type=TENANT`、拒绝改 SYSTEM 角色）。这些普通管理入口的设计边界是：租户管理员操作本租户账号，不获得平台账号（`tenant_id=0`）或其他租户账号的管理权限。

上述源码边界不是完整授权边界的运行验收。MGMT-11 的自助角色写入仍绕过 Service 校验；关系表自身有 tenant_id 不能代替目标角色的租户一致性校验。2026-09-22 复核未运行跨租户 HTTP 负向验证。

| 编号 | 已核对的事实及影响 | 状态 |
| --- | --- | --- |
| MGMT-16 | viewer 的租户取自可信主体；JWT `tenant_id=0` 会绕过 Ent 租户过滤，但仍受角色和 Casbin domain/API 授权。AK/SK 与 JWT 不能混写：`signature.go`、`access_key_repo.go` 明确拒绝租户 0，AccessKey schema 也有正租户约束；管理接口 `keyManager` 拒绝平台身份。 | JWT 签发必须保证租户语义正确；不能把取消行过滤描述为“所有平台 API 全通”，也不能忽略 AK/SK 已有防线。2026-09-22 源码更正；本轮未做运行验收 |
| MGMT-17 | `appViewer.NewSystemViewerContext` 会**完全绕过**租户过滤（该 viewer `IsPlatformContext()`/`IsSystemContext()` 均为真、`TenantID()==0`，go-crud `rule/tenant.go:36-39,68-71` 对平台/系统上下文直接返回空谓词）。该旁路在**请求与认证路径上是刻意且广泛使用**的：登录（`authentication_service.go:294,874` 的 `resetContextForLogin`，注释说明为绕过 TenantPrivacy 以查到租户凭证）、找回密码（`authentication_forgot_password.go:27,75`）、邀请激活（`user_invitation.go:87`）、租户闸门中间件（`tenant_access_checker.go:45`）、租户用量（`tenant_usage_repo.go:74,154,256`）、站内信（`internal_message_service.go:508`）、策略评估日志（`policy_eval_logging_engine.go:94,113`）、启动重置策略（`rest_server.go:259`）、策略装载（`authorizer_provider.go:57`）、定时任务（`asynq_server.go:63`、`task_service.go:559`）与审计落地（`pkg/middleware/logging/*`、`audit_log_archive_repo.go:48`） | `user_service.go`/`role_service.go` 未使用（已确认）；该旁路**安全性取决于各调用点是否自行正确限定范围**，任何新增/改动若误用即成越权口，必须评审 |
| MGMT-18 | `sys_permissions`/`sys_permission_apis` **无 `tenant_id`**，权限点是全局资源；租户上下文下挂权限点不经租户校验，而组织单元有同租户校验（`role_org_unit_repo.go:38-58`）。 | 当前租户管理员**无角色写授权**，故为**潜在**项；一旦开放角色写，可把任意权限点挂到本租户 TENANT 角色（**租户内**提权，非跨租户） |

### 用户确认的范围（2026-09-21）

- 主管理员更换：产品不提供，不新增入口或移交流程；本轮也不借此删除现有字段或改写通用更新合同。
- 平台账号：允许多个不同用户名的 `tenantId=0` 账号，共享同一平台角色或分配不同系统角色；默认管理员的删除保护不限制创建其他账号。
- 运营/只读角色模板：等待后期 API 都接入后，再确定权限矩阵并补充种子；本轮不新增角色或授权。
- 密码重置：AES 协议与路径登记不一致只记录，暂不修复。
- 套餐扩展：可创建多个套餐并组合现有模块、配额值；当前模块来自固定 `Module` 枚举，数量配额类型只有 `USER_LIMIT`、`STORAGE`、`API_CALL`，不是界面新增名称就能得到新统计能力。
- 跨服务配额：另行讨论。建议先列实际需要的配额项，再逐项确定单位、租户维度、累计/当前占用口径、来源服务、统计更新方式以及是否需要超限拦截。Governance 维护套餐限额，各资源服务提供其实际用量；需要强制限额时再明确创建/释放资源中的额度校验与并发处理。本轮不扩展配额模型，也未自动创建新任务。

### 接口及报文

以下均需 Bearer 和相应 API 授权；租户上下文还受租户状态与套餐模块限制。普通新增传 `{data:{...}}`，更新传 `{data:{...},updateMask:...}`；列表使用 `pagination.PagingRequest` 并返回 `{items,total}`，详情直接返回对象，普通写操作返回 `google.protobuf.Empty`。新增租户连同管理员使用专用报文。所有目标风格均为“待指定”。

| 编号 | 方法与路径 | 请求或用途 |
| --- | --- | --- |
| TENANT-01 | `GET /admin/v1/tenants` | 分页、过滤租户；返回人数及主管理员名称 |
| TENANT-02 | `GET /admin/v1/tenants/{id}` | 租户详情 |
| TENANT-03 | `POST /admin/v1/tenants` | `data`；只创建租户记录 |
| TENANT-04 | `PUT /admin/v1/tenants/{id}` | `data`、`updateMask`；含 `planId`、`expiredAt`、`status` 等 |
| TENANT-05 | `DELETE /admin/v1/tenants/{id}` | 删除租户记录 |
| TENANT-06 | `POST /admin/v1/tenants:with-admin` | `{tenant:{...},user:{...},password,activationMode}`；租户、管理员、角色和凭证在同一事务创建 |
| TENANT-07 | `GET /admin/v1/tenants:exists` | `code` / `name` 查重，返回 `exist` |
| TENANT-08 | `GET /admin/v1/tenants/{id}/usage` | 用户数、API 审计记录数、套餐及配额；文件管理移除后 `storageUsedBytes` 固定为 0，不代表外部存储真实用量；不是计费周期用量 |
| TENANT-09 | `POST /admin/v1/tenants/{id}/cleanup` | 显式删除实现所列 Governance 租户数据，保留租户并置 OFF；不等于清理下游资源或对象存储文件 |
| ACCOUNT-01 | `GET /admin/v1/users` | 用户分页；按租户、角色等过滤 |
| ACCOUNT-02 | `GET /admin/v1/users/{id}` | 用户详情 |
| ACCOUNT-03 | `POST /admin/v1/users` | `{data:{tenantId,username,roleIds,...},password,activationMode}`；角色必须与目标租户/系统类型匹配 |
| ACCOUNT-04 | `PUT /admin/v1/users/{id}` | `data`、`updateMask`，可另传 `password`；当前 Service 即使只改资料/状态也要求传有效角色 |
| ACCOUNT-05 | `DELETE /admin/v1/users/{id}` | 删除用户；拒绝删除默认平台管理员或操作者本人 |
| ACCOUNT-06 | `GET /admin/v1/users:exists` | `id` / `username` 二选一，返回 `exist`；用户名查重仅支持当前租户，平台身份按用户名调用会拒绝，不能额外传 `tenantId` 指定目标；平台页面改用 ACCOUNT-01 按目标租户＋用户名精确过滤 |
| ACCOUNT-07 | `POST /admin/v1/users/{user_id}/password` | `newPassword`；当前仍要求 AES 编码，与登录不同；路径目录问题见 MGMT-05 |
| ACCOUNT-08 | `GET /admin/v1/users/username/{username}` | 用户名读取别名；同名账号可能跨租户，平台管理优先使用 ID |
| ACCOUNT-09 | `DELETE /admin/v1/users/username/{username}` | 已注册的旧别名；Service 删除保护按 ID 查目标，不能据路由存在认定此别名可用，见 MGMT-06 |
| ROLE-01 | `GET /admin/v1/roles` | 按租户与 `type` 读取角色 |
| ROLE-02 | `GET /admin/v1/roles/{id}` | 角色详情与权限 ID |
| ROLE-03 | `POST /admin/v1/roles` | `data`，包含角色代码、类型和 `permissions` |
| ROLE-04 | `PUT /admin/v1/roles/{id}` | 修改角色及权限关联；受保护角色的类型、代码、状态等禁止修改 |
| ROLE-05 | `DELETE /admin/v1/roles/{id}` | 删除角色；受保护角色禁止删除 |
| PLAN-01 | `GET /admin/v1/plans` | 套餐列表 |
| PLAN-02 | `GET /admin/v1/plans/{id}` | 套餐详情 |
| PLAN-03 | `POST /admin/v1/plans` | `data`，含名称、版本、到期策略、保留天数 |
| PLAN-04 | `PUT /admin/v1/plans/{id}` | `data`、`updateMask` |
| PLAN-05 | `DELETE /admin/v1/plans?id=...` | ID 在查询参数，未使用详情路径 |
| PLAN-06 | `GET /admin/v1/plan-modules` | 按 `plan_id` 查模块白名单 |
| PLAN-07 | `GET /admin/v1/plan-modules/{id}` | 单条模块配置 |
| PLAN-08 | `POST /admin/v1/plan-modules` | `data`，关联套餐与模块 |
| PLAN-09 | `PUT /admin/v1/plan-modules/{id}` | `data`、`updateMask` |
| PLAN-10 | `DELETE /admin/v1/plan-modules?id=...` | 删除单条模块配置 |
| PLAN-11 | `GET /admin/v1/plan-quotas` | 配额列表；BFF 没有注册单条 GET |
| PLAN-12 | `POST /admin/v1/plan-quotas` | `data`，含 `planId`、`quotaType`、`quotaValue` |
| PLAN-13 | `PUT /admin/v1/plan-quotas/{id}` | `data`、`updateMask` |
| PLAN-14 | `DELETE /admin/v1/plan-quotas?id=...` | 删除配额项 |
| SESSION-01 | `GET /admin/v1/online-session/sessions` | `page`、`pageSize`、`keyword`；返回在线会话 |
| SESSION-02 | `POST /admin/v1/online-session/force-logout` | `userId`、`jti`、`clientType`；吊销指定会话，返回空对象 |
| SESSION-03 | `GET /admin/v1/online-session/my-sessions` | 无请求参数，身份取自认证上下文；返回本人会话列表（含 `current` 标记），供个人中心自助查看 |
| SESSION-04 | `POST /admin/v1/online-session/my-sessions/revoke` | `clientType`、`jti`；吊销本人指定会话，返回空对象 |
| PERM-01 | `GET /admin/v1/permissions` | 权限点分页；租户调用者仅见自身角色可达的权限点，平台调用者见全部 |
| PERM-02 | `GET /admin/v1/permissions/{id}` | 权限点详情；租户调用者对越权权限点返回 403 |
| PERM-03 | `POST /admin/v1/permissions` | `data`（code/name/groupId/status）；写后 `ResetPolicies` 失败会向上返回错误 |
| PERM-04 | `PUT /admin/v1/permissions/{id}` | `data`、`updateMask`；**必须回传完整 `apiIds`/`menuIds`，省略即确定性清空绑定**（见 MGMT-09） |
| PERM-05 | `DELETE /admin/v1/permissions/{id}` | 删除权限点；`sys:*` 受保护清单未在本路径强制（见 MGMT-10） |
| PERM-06 | `POST /admin/v1/permissions/sync:perms` | 无请求体；先截断再重建非 `sys:*` 的权限组/权限点，破坏性同步，执行前需备份 |
| PERMGROUP-01 | `GET /admin/v1/permission-groups` | 权限组分页 |
| PERMGROUP-02 | `GET /admin/v1/permission-groups/{id}` | 权限组详情 |
| PERMGROUP-03 | `POST /admin/v1/permission-groups` | `data`；不触发 `ResetPolicies`（策略生成不读分组 module，非刷新缺口） |
| PERMGROUP-04 | `PUT /admin/v1/permission-groups/{id}` | `data`、`updateMask`；不触发 `ResetPolicies` |
| PERMGROUP-05 | `DELETE /admin/v1/permission-groups/{id}` | 先删组内权限点再删组，两步无共享事务，属破坏性级联 |

> 权限点与权限组此前只以"待对应功能对接时详细登记"一句带过，本轮按 `PERM-*`/`PERMGROUP-*` 正式登记；菜单与 API 目录的独立管理仍待对应功能对接时登记。

> 在线会话在源域只声明 2 个 RPC（`online_session.service.v1.OnlineSessionService.List/ForceLogout`），BFF `OnlineSessionService` 额外暴露了 `ListMyOnlineSession`/`RevokeMyOnlineSession` 两条自助路由（`api/protos/admin/service/v1/i_online_session.proto:27,34`），两者复用同一批源域消息。

登录/登出复用 AUTH-01～04，邮件接受邀请复用 AUTH-12。`activationMode=IMMEDIATE`（默认）不需要 SMTP；`EMAIL_INVITATION` 需邮件通道及邀请入口配置，不接受预设密码，账号待激活，邀请有效期 24 小时。当前无邀请重发/撤销管理 HTTP 接口。权限点与权限组已按 `PERM-01～06`、`PERMGROUP-01～05` 正式登记；自助 `/admin/v1/me` 面（`AUTH-15～18`）登记于登录功能组；菜单与 API 目录的独立管理仍待对应功能对接时详细登记，本轮未操作其同步接口。

### 已发现的限制与对接问题

| 编号 | 已核对的事实及影响 | 状态 |
| --- | --- | --- |
| MGMT-01 | 源领域 Proto 有 `AssignTenantAdmin`，但 admin BFF 无该路由，TenantService 无对应方法；通用更新可写 `adminUserId`，却不会同步角色或校验完整移交条件 | 2026-09-21 用户明确产品不需要；作为不实现项关闭，不新增移交功能 |
| MGMT-02 | 按用户授权补齐：状态实际写成非 NORMAL、或删除用户成功后，调用现有全客户端会话吊销；清理访问令牌、刷新令牌及在线会话；FieldMask 排除状态时不误踢；吊销失败明确报错，共用缓存清理不再覆盖前序错误。角色变更不在本轮修复范围 | 实现及定向回归 PASS；尚未部署，kind 验收 not_verified |
| MGMT-03 | 用户数和 API 调用量有本地统计；STORAGE 配额类型保留，但文件管理移除后存储用量固定返回 0，尚未接入外部服务统计。未找到创建资源时的超限拦截；数据保留天数未找到自动清理消费方 | 用户要求扩展配额；建议另开任务确定各配额类型、统计口径、来源服务及超限处理，本轮只登记 |
| MGMT-04 | 到期 READONLY 在请求时判断；BLOCK_LOGIN/FREEZE 由每小时扫描修改状态，依赖 Asynq 调度与执行；延长有效期不会自动把已冻结/过期状态改回 ON | 实现已存在；本轮到期运行验收 not_verified |
| MGMT-05 | 用户密码路由注册为 `{user_id}`，嵌入 OpenAPI/首次种子登记为 `{userId}`；TenantAccessChecker 对路径模板做精确匹配，租户直调该接口可能在业务执行前被拒；密码更新/重置仍需 AES，登录已改原始密码 | 用户明确暂不修复，只登记；HTTP 复现 not_verified |
| MGMT-06 | 用户名删除路由虽已注册，UserService.Delete 总是先按 `req.GetId()` 查目标，用户名路由未提供该 ID；平台管理应使用 ACCOUNT-05 | 源码缺口；别名 HTTP 验收 not_verified |
| MGMT-07 | UserService.Update 无论 updateMask 是否涉及角色都校验非空 roleIds；创建普通用户的立即激活分支先提交用户，再创建密码凭证，后者失败不自动回滚已建用户 | 接入限制已确认；待指定修复，未修改 |
| MGMT-08 | 在线会话 4 条路由中，自助两条 `my-sessions`、`my-sessions/revoke` 在首次种子里只授给 `sys:platform_admin`；`sys:tenant_manager` 没有任何 online-session 授权，菜单 `OnlineSessionManagement` 的 `authority` 也只有 `sys:platform_admin`。因此租户管理员（及一般自服务场景）无法查看/下线自己的会话 | 源码与种子已确认；租户管理员是否放行自助会话待用户指定，未修改 |
| MGMT-09 | `PermissionService.Update` 的仓储实现无条件重设 `apiIds`/`menuIds`（`permission_repo.go:456-462`），其 `CleanNotExist*` 用 `NotIn(空)` 恒真删除；请求体省略这两字段会**确定性清空**该权限点已绑定的 API/菜单关联 | 已接线但此前未登记；本轮补登 `PERM-04` 并明确"PUT 必须回传完整数组"，未改代码 |
| MGMT-10 | `constants.ProtectedPermissionCodes`（`sys:*` 共 7 个）在 `PermissionService.Delete` 路径未被强制校验，删除保护只落在角色层（`ROLE-05`） | 受保护权限点可被直接删除，导致鉴权与菜单静默失效；是否补删除保护待指定 |
| MGMT-11 | `PUT /admin/v1/me`（`UserProfileService.UpdateUser`，`user_profile_service.go:81-96`）直接调 `userRepo.Update`，跳过 `UserService.Update` 的角色类型/租户校验与"至少一个角色"约束；而 `user_repo.go` 在掩码含 `role_ids` 时会应用 `roleIds` | 源码观察（**未运行验证**）：本人可借自助接口改动自身角色集合，形成自助提权面；是否收紧待用户指定 |
| MGMT-12 | `tenant.admin_user_id` 是自由指针（`TenantRepo.AssignTenantAdmin`），无外键/级联或一致性校验；`UserService.Delete` 删除主管理员用户后**不清空**该指针 | 租户记录留下悬空主管理员引用，`List`/`Get` 的 `adminUserName` 静默为空；与 MGMT-01（移交缺失）是不同问题 |
| MGMT-13 | 平台账号复用 `/admin/v1/users`；删除保护覆盖 `id==1`、（`username==admin` 且 `tenantId==0`）以及操作者本人。平台登录还要求 `platform:*` 角色及后台访问权限，不能仅凭 `SYSTEM` 类型判定可登录。 | 删除非默认账号不必然导致失去管理员；缺口是停用、移除角色等更新没有“最后一个有效管理员”保护，默认账号被停用或失去管理角色后亦可能失去管理入口。2026-09-22 更正；是否补保护待指定 |
| MGMT-14 | 租户仅注册 `GET /admin/v1/tenants/{id}`（绑定 id），源消息的 `query_by code/name` 无 HTTP 入口；`cleanup` 不删租户行（置 OFF），`code`/`name` 仍被 `TenantExists` 占用 | 按 code/name 读取须列表过滤；清理后无法以同 code/name 重新 `with-admin` 建租户 |
| MGMT-15 | `PlanModuleService`/`PlanQuotaService` 的 Update 回调不写 `planId`（仅 updated_by/updated_at 与 module/quota 字段）；三者均无 HTTP `Count` 路由，源域 `TenantService.Count`/`BatchCreate` 亦无 HTTP 入口；`cleanup` 声明 `body:"*"` 但请求体无业务语义 | 白名单行/配额行的套餐归属不能经 PUT 改挂，只能删后重建；批量建租户须循环调用；登记为契约冗余，未改代码 |

### MGMT-02 修复及验证边界

复用现有 `RevokeUserTokenAllClientTypes`，不新增接口、表、迁移或依赖。状态更新后读取实际持久化状态，避免请求带 `status` 但 FieldMask 排除它时误踢用户；删除仅在仓储成功后吊销目标账号。访问令牌、刷新令牌及会话元数据均清理，其他用户保持原会话。

数据库修改与 Redis 吊销不是同一事务。吊销失败返回明确错误，不谎报成功：状态更新失败提示可重试同一状态更新；已删除账号应通过在线会话管理清理残留会话。本次不新增分布式事务或并发登录/刷新栅栏，相关并发竞态未验收。密码重置协议、用户名删除别名、角色变更后的即时权限更新均不在本次修改范围。

验证使用 Ubuntu 独立源码目录、Go 1.26.7、SQLite 和 miniredis；使用独立源码快照，不覆盖远端工作区，不修改 kind 运行实例。验证记录：

- 在未修复源码上运行新增回归：成功复现停用/锁定/删除后旧令牌仍有效，以及缓存部分清理错误被覆盖。
- 修复后执行 `go test ./app/admin/service/internal/data ./app/admin/service/internal/service -run '^(TestUserTokenCache|TestUserService|TestAuthSvcSqlite_)' -count=1`：PASS。
- 新增用例覆盖平台/租户账号停用与删除、锁定、多会话和两类客户端缓存清理、其他账号不受影响、资料修改不踢人、FieldMask 排除状态、受保护账号删除拒绝、Redis 故障明确报错；共用缓存测试验证前序错误不被后续成功覆盖。后台令牌使用真实签名和校验；app 端仅覆盖现有缓存清理，不声称已有 app 签发实现。
- 服务入口 `go build ./app/admin/service/cmd/server` 编译：PASS。未重新部署，真实 PostgreSQL/Redis HTTP 验收为 `not_verified`。
- 远端证据目录：`/home/ubuntu/Workspace/.codex-runs/governance-account-sessions-20260921/`（`baseline.log`、`regression.log`、`build.log`）。

源码入口：[租户业务](../app/admin/service/internal/service/tenant_service.go)、[用户业务](../app/admin/service/internal/service/user_service.go)、[角色业务](../app/admin/service/internal/service/role_service.go)、[套餐业务](../app/admin/service/internal/service/plan_service.go)、[租户模块检查](../app/admin/service/internal/data/tenant_access_checker.go)、[用量与到期任务](../app/admin/service/internal/data/tenant_usage_repo.go)、[首次种子](../sql/bootstrap/001_initial.sql)、[HTTP 装配](../app/admin/service/internal/server/rest_server.go)。

整合记录：已拉取 `11fa857`，源码改动无文本冲突；原未跟踪的 `docs/onboarding-invite-audit.md` 与远端新增版本内容一致，保留远端跟踪版本。同步修正文件管理移除后存储用量固定返回 0 的登记说明；整合后的相同定向回归与服务入口编译均 PASS（Go 1.26.7）；证据目录为 Ubuntu `/home/ubuntu/Workspace/.codex-runs/governance-account-main-20260921/`，包含 `regression.log` 和 `build.log`。未部署到 kind。

整合记录（2026-09-22）：本轮登记与远端 `0c8e567`（AK-SK 签名接入 / Network VPC 校验）及合并提交 `7c8f574` 整合。该提交把访问密钥路由由 `/admin/v1/access-keys*` 改为 `/api/v1/auth/api-keys*`，为租户管理员新增 6 条 AK 授权（租户块由 24 条增至 30 条，现为 `501-530`），重写 `pkg/middleware/auth/auth.go`（viewer 构造移至 `112-123`，新增已验签 AK/SK 主体路径 `49-66`），并移除了 `access_key_repo.go` 中的 SystemViewer 调用。本文件「鉴权边界（授权矩阵与租户隔离）」小节的全部行号引用已据此**重算**（平台块 `313-497`、租户块 `501-530`），并新增「API Key」一行及与 AK 功能组的交叉引用。同步修正登录路径的 SystemViewer 引用行号（`294,874`）。登记为文档改动，未改任何接口或运行代码。

<a id="frontend-management-handoff"></a>

## 前端交接：用户、租户、租户管理员、平台管理员页面

交接日期：2026-09-22。源码基线：`8b80461`。范围为上述四类页面及必要的登录、角色选择、套餐选择、会话配套。可将本节完整转发给前端。

**“可接”表示当前源码存在对应路由与实现，可开始开发和联调；不表示目标环境已部署或端到端验收通过。** 本次只核对源码与报文并编写文档；目标环境 HTTP、前端、隔离验收为 `not_verified`。本节“暂缓”是建议的第一批页面范围，不代表后端已关闭这些路由。

### 1. 通用请求与响应

- `BASE_URL` 使用联调环境提供的 Governance 地址；以下路径原样使用，不统一替换 `/api/v1` 和 `/admin/v1`。
- 受保护请求带 `Authorization: Bearer <access_token>`；JSON 请求带 `Content-Type: application/json`，接受 JSON 响应。浏览器登录、刷新需保存并发送 Cookie，建议经同源代理访问。
- 普通创建是 `{ "data": { ... } }`，普通更新是 `{ "data": { ... }, "updateMask": "字段1,字段2" }`。登录和 `tenants:with-admin` 等专用接口按下文报文发送。
- HTTP JSON 的 `updateMask` 是逗号分隔的 **lowerCamelCase 字符串**，例如 `"nickname,roleIds"`、`"planId,expiredAt,status"`；不是 `{ "paths": [...] }`，也不要发送 `"role_ids"`。后端解析后才转换为 snake_case。更新始终明确传 mask，不使用 `allowMissing`。
- 路径中的 `{id}` 替换为真实数字，GET 不带请求体。普通实体 ID 为 JSON number；`total`、用量等 int64/uint64 字段按 JSON 十进制字符串处理，界面计数可在确认安全整数范围后转 number。
- 枚举使用字符串名；时间使用 RFC 3339，例如 `"2027-09-22T00:00:00Z"`，不能发送毫秒时间戳。可选字段可能缺失；不要假设 `null` 或省略字段能清空数据库值。
- 成功响应直接是业务对象，没有统一 `{code,data}` 外壳。列表为 `{ "items": [...], "total": "2" }`；详情直接返回 User/Tenant/Role/Plan；普通创建、更新、删除返回 `{}`，**不返回新建 ID**。创建成功后刷新对应筛选列表获取结果。
- 错误使用非 2xx HTTP 状态，JSON 为 `{ "code": 403, "reason": "FORBIDDEN", "message": "...", "metadata": { "request_id": "..." } }` 这类结构；`reason` 的实际值按响应处理。401 走刷新/重新登录，403 提示权限或租户套餐限制，不能把所有 403 都当登录过期。保留 request_id 便于联调。
- 不提交 `createdBy/updatedBy`、统计字段等只读数据。用户响应中的邮箱/手机可能脱敏或被 `hiddenFields` 隐藏；编辑时只提交实际修改的字段，不能把整个详情对象原样写回。

### 2. 登录及页面初始化

| 编号 | 方法与路径 | 请求 | 响应 / 页面行为 |
| --- | --- | --- | --- |
| AUTH-02 | `POST /api/v1/auth/platform/password/login` | `{username,password}`，原始密码 | 平台管理员登录 |
| AUTH-01 | `POST /api/v1/auth/password/login` | `{tenant_name,username,password}`，原始密码 | 租户用户及租户首管理员登录；`tenant_name` 填租户 `code`，不是显示名称 |
| AUTH-05 | `GET /admin/v1/me` | Bearer，无参数 | 当前 User；用于确认当前 `id/tenantId/roles` |
| AUTH-06 | `GET /admin/v1/initial-context` | Bearer，无参数 | `{menus,permissions,hiddenFields}`，用于导航、按钮和字段展示 |
| AUTH-04 | `POST /api/v1/auth/refresh` | `{}`，携带 `refresh_token` Cookie，无需 Bearer | 新访问令牌；客户端串行刷新，旧令牌对失效 |
| AUTH-03 | `POST /api/v1/auth/logout` | Bearer，`{}` | `{status:"revoked"}`；吊销该用户全部后台会话并清 Cookie |
| AUTH-07/08 | `GET /admin/v1/routes`、`GET /admin/v1/perm-codes` | Bearer | 单独导航 `{items}`、权限 `{codes,hiddenFields}`；用了 AUTH-06 可不重复调用 |

正常登录/刷新响应为 `access_token`、`expires_in`（秒，字符串）和 `issued_at` 等字段。刷新令牌从 HttpOnly Cookie 读取，不依赖响应体的 `refresh_token`。密码登录不再使用 AES、Base64 或图形验证码。

若登录返回 `mfa_required=true`，当前尚未完成登录，不能进入管理页；按既有 AUTH-09（`POST /admin/v1/mfa/verify`）处理二次验证，其响应与普通登录不同，见登录功能组。不要因首批未制作 MFA 页面而把该中间态当成功。

### 3. 列表、筛选与分页

用户、租户、角色、套餐列表统一使用 `page`、`pageSize`、`query`，从第 1 页开始；`query` 是 **JSON 字符串**，通过 URL 参数编码发送。筛选中的字段采用数据库 snake_case；这与 JSON body 的 camelCase 是两层合同。

```javascript
// 示例 ID 均须替换为接口查到的值。此例查询租户 12 的全部用户。
const params = new URLSearchParams({
  page: '1',
  pageSize: '20',
  query: JSON.stringify({ tenant_id: '12' }),
  orderBy: JSON.stringify(['-id']),
});
const url = `${BASE_URL}/admin/v1/users?${params.toString()}`;
// 使用项目统一请求客户端，并附加 Bearer。
```

| 需求 | `query` 的对象内容（发送时 JSON.stringify） |
| --- | --- |
| 平台账号列表 | `{ "tenant_id": "0" }` |
| 指定租户的全部用户 | `{ "tenant_id": "12" }` |
| 指定租户管理员列表 | `{ "tenant_id": "12", "role_id": "34" }`，34 为该租户管理员角色 ID |
| 用户名包含搜索 | `{ "tenant_id": "12", "username__contains": "alice" }` |
| 平台替指定租户检查用户名 | `{ "tenant_id": "12", "username": "alice" }`，查 `items/total` |
| 查租户管理员角色 | 对 `/admin/v1/roles`：`{ "tenant_id": "12", "type": "TENANT", "code": "tenant:manager", "status": "ON" }` |
| 查默认平台管理员角色 | 对 `/admin/v1/roles`：`{ "tenant_id": "0", "type": "SYSTEM", "code": "platform:admin", "status": "ON" }` |
| 租户名称包含搜索 | 对 `/admin/v1/tenants`：`{ "name__contains": "示例" }` |
| 创建租户后定位记录 | 对 `/admin/v1/tenants`：`{ "code": "example-team" }` |

裸字段是精确条件，文本模糊搜索显式使用 `__contains`，ID 不做模糊匹配。单角色过滤用顶层 `role_id`；多角色用顶层 `"role_ids__in": ["34","35"]`。不要写成 `role_ids:[34,35]`，也不要把角色条件藏在 `$and/$or` 子树中。管理员列表由后端过滤后分页，不在前端拿一页普通用户再筛管理员。

角色/套餐下拉可在范围受控时使用 `noPaging=true`，仍携带必要的 `query`；当前依赖默认上限为 10,000 行，不能当作无限全量导出。普通分页建议 `pageSize=20`，默认上限 100。当前租户登录只能读取本租户允许的数据，不能靠修改筛选参数切换身份。

### 4. 用户管理：三个账号页面共用

平台管理员可管理平台及目标租户账号；默认租户管理员仅管理本租户。普通用户是否有管理权限取决于其角色，不能因能登录而显示所有管理按钮。

| 编号 | 方法与路径 | 请求 / 响应 | 第一批接入说明 |
| --- | --- | --- | --- |
| ACCOUNT-01 | `GET /admin/v1/users` | 共用分页 → `{items,total}` | 列表、查重预检查、创建后定位 |
| ACCOUNT-02 | `GET /admin/v1/users/{id}` | 无 body → User | 查看及编辑前读取最新角色/状态 |
| ACCOUNT-03 | `POST /admin/v1/users` | `{data,password,activationMode}` → `{}` | 新增普通用户、追加租户管理员、创建平台管理员共用 |
| ACCOUNT-04 | `PUT /admin/v1/users/{id}` | `{data,updateMask}` → `{}` | 资料、角色、启用/禁用共用；不在资料更新中夹带 password |
| ACCOUNT-05 | `DELETE /admin/v1/users/{id}` | 无 body → `{}` | 按 ID 删除；服务端拒绝删除默认平台管理员及本人 |
| ACCOUNT-06 | `GET /admin/v1/users:exists` | `?username=alice` 或 `?id=123` → `{exist}` | username 仅当前租户可用；平台用户名查重改用列表精确查询，不能附加 tenantId 绕过 |
| ROLE-01 | `GET /admin/v1/roles` | 共用分页 → `{items,total}` | 获取目标租户可分配角色；平台角色筛 `tenant_id=0,type=SYSTEM` |
| ROLE-02 | `GET /admin/v1/roles/{id}` | 无 body → Role | 查看角色 `code/type/tenantId/permissions`；permissions 是权限点 ID 数组 |

用户主要显示字段：`id,tenantId,tenantName,username,nickname,realname,roleIds,roles,roleNames,status,email,mobile,createdAt,updatedAt`。角色显示名称不能作为写入标识，写入使用 `roleIds`。

创建示例：`POST /admin/v1/users`。`12/34` 为查询所得租户/角色 ID；密码占位符必须替换为用户输入并通过服务端密码策略。

```json
{
  "data": {
    "tenantId": 12,
    "username": "alice",
    "nickname": "Alice",
    "roleIds": [34],
    "status": "NORMAL"
  },
  "password": "<用户输入的原始密码>",
  "activationMode": "IMMEDIATE"
}
```

立即激活时页面要求明确输入密码，不依赖后端默认密码。创建用户和密码凭证目前不是一个事务；若返回失败，应按租户和用户名重新查列表确认是否已建用户，不能无条件自动重试创建（MGMT-07）。

资料更新示例：`PUT /admin/v1/users/123`。

```json
{
  "data": {
    "tenantId": 12,
    "roleIds": [34],
    "nickname": "新的昵称",
    "remark": "新的备注"
  },
  "updateMask": "nickname,remark"
}
```

**每次用户更新都带真实目标 `tenantId` 和完整有效 `roleIds`**，即使 mask 只改昵称或状态；这是当前 Service 的校验要求。普通字段更新的 mask 不加 `roleIds`，防止覆盖并发角色变化。平台更新租户账号漏传 `tenantId`，会按 SYSTEM 角色校验并可能报 `some roles not found`。

- 启停：同一 PUT，`data` 保留 `tenantId/roleIds` 并传 `status:"DISABLED"` 或 `"NORMAL"`；`updateMask:"status"`。
- 分配角色：同一 PUT，传目标完整 `roleIds`，`updateMask:"roleIds"`；不允许传空角色数组。角色保存没有吊销旧 JWT，不能提示“已有会话权限立即失效”；刷新/重登才重载用户角色。
- 第一批资料字段可用 `nickname/realname/email/mobile/telephone/region/address/description/remark/gender`；用户名、租户归属保持只读，不提供迁租、改用户名或头像编辑。组织/岗位关联不在本次四页面接入范围。
- 管理侧修改资料中的 email 不等于完成邮箱验证，也不自动建立找回密码所需的邮箱凭证；邀请码接受与 AUTH-17/18 邮箱验证是独立流程。
- 用户状态全集为 `NORMAL/DISABLED/PENDING/LOCKED/EXPIRED/CLOSED`，状态展示需兼容全部值；手动启停使用前两种，邀请待激活不等于管理员手动启用流程。
- 禁用/删除可能出现数据库成功、会话吊销失败而返回 500；页面重新查询记录/状态，展示服务端提示，不把报错简单解释为“没有发生修改”。

### 5. 租户管理员与平台管理员的页面规则

| 页面 | 列表条件 | 创建 / 编辑角色 | 登录入口 |
| --- | --- | --- | --- |
| 租户用户管理 | 指定 `tenant_id`；默认含管理员，可按角色进一步筛选 | 该租户的 TENANT 角色 | AUTH-01 |
| 租户管理员管理 | 指定 `tenant_id` ＋ 该租户 `tenant:manager` 的角色 ID | 复用 ACCOUNT-03/04，绑定查询到的本租户管理员角色 | AUTH-01，含首管理员 |
| 平台管理员管理 | `tenant_id=0`；若只列管理员，再加 `platform:admin` 对应角色 ID | 第一批使用现有 `platform:admin`；角色类型 SYSTEM | AUTH-02 |

三个页面复用同一用户 CRUD，不新增“管理员账号”接口。`tenant_id=0` 是平台范围，不是“当前租户”的占位值；不要在租户用户请求中随意使用 0。

`adminUserId` 是租户记录中的主管理员指针；它不代表所有具有管理员角色的用户。产品不做主管理员移交。默认租户管理员只有角色读取权限，没有角色增删改权限；“给用户分配已有角色”和“编辑角色的权限”是两项功能。

首次种子只提供平台管理员和租户管理员模板，**没有普通用户、平台运营、平台只读的完整角色模板**。普通用户页面需要后端/平台先准备对应 TENANT 角色，不能默认把管理员角色当作普通角色。第一批不制作自定义权限矩阵。

删除、禁用、撤销管理员角色前需要确认目标；当前没有“最后一个有效管理员”后端保护，删除租户主管理员也不会自动清空 `adminUserId`。建议首批暂不开放主管理员删除、管理员降权等易导致失去管理入口的操作（MGMT-12/13）；页面限制不代替后端保护。

### 6. 租户管理

本组为平台侧页面；默认租户管理员无 TENANT-* 及 PLAN-* 授权。

| 编号 | 方法与路径 | 请求 / 响应 | 第一批接入说明 |
| --- | --- | --- | --- |
| TENANT-01 | `GET /admin/v1/tenants` | 共用分页 → `{items,total}` | 列表、租户选择、创建后定位 |
| TENANT-02 | `GET /admin/v1/tenants/{id}` | 无 body → Tenant | 详情 |
| TENANT-06 | `POST /admin/v1/tenants:with-admin` | `{tenant,user,password,activationMode}` → `{}` | 新开租户推荐入口，原子创建租户、首管理员、角色、凭证 |
| TENANT-03 | `POST /admin/v1/tenants` | `{data:{...租户字段}}` → `{}` | 仅创建租户记录，不生成管理员/租户管理员角色；不作为默认开通按钮 |
| TENANT-04 | `PUT /admin/v1/tenants/{id}` | `{data,updateMask}` → `{}` | 编辑资料、状态、套餐、有效期 |
| TENANT-07 | `GET /admin/v1/tenants:exists` | `?code=example-team` 或 `?name=示例团队` → `{exist}` | 至少传一项；两项同时传时任一重复即 true；不支持排除当前记录 ID |
| TENANT-08 | `GET /admin/v1/tenants/{id}/usage` | 无 body → TenantUsage | 只读用量；`storageUsedBytes=0` 为当前占位，不能标记为外部存储真实用量 |
| PLAN-01 | `GET /admin/v1/plans` | 共用分页 → `{items,total}` | 套餐选择器，使用返回的真实 id |
| PLAN-02 | `GET /admin/v1/plans/{id}` | 无 body → Plan | 展示所选套餐名称、到期策略等 |

主要租户展示字段：`id,name,code,status,type,auditStatus,planId,expiredAt,adminUserId,adminUserName,memberCount,createdAt`。审核状态目前是普通字段，不代表独立审批流程。`TenantUsage` 返回 `tenantId,userCount,storageUsedBytes,apiCallCount,planId,planName,quotas`，API 次数不是计费周期统计，quota 也不代表已实现超额拦截。

开通示例：`POST /admin/v1/tenants:with-admin`。套餐 ID 5 须换成实际允许后台管理模块的套餐。

```json
{
  "tenant": {
    "name": "示例团队",
    "code": "example-team",
    "status": "ON",
    "planId": 5
  },
  "user": {
    "username": "tenant_admin",
    "nickname": "租户管理员",
    "status": "NORMAL"
  },
  "password": "<用户输入的原始密码>",
  "activationMode": "IMMEDIATE"
}
```

`user.tenantId/roleIds` 和 `tenant.adminUserId` 不由页面填写，由服务端生成绑定。建号成功后刷新列表，按租户 `code` 精确取得新租户。租户必须启用、绑定包含所需模块的套餐，且实际 API 目录和角色绑定齐全，租户账号才能使用管理页；这些是环境前提，前端不用调用 API 同步或修改种子。

`tenants:with-admin` 返回失败时，也先按 code 查询租户及首管理员。数据库事务提交后还会重载策略，重载失败可导致“已创建但响应报错”；不要无条件自动重试创建，由后端确认持久化结果并重载策略。

更新示例：`PUT /admin/v1/tenants/12`。

```json
{
  "data": {
    "planId": 5,
    "expiredAt": "2027-09-22T00:00:00Z",
    "status": "ON"
  },
  "updateMask": "planId,expiredAt,status"
}
```

套餐实际关联用 `planId`，`subscriptionPlan` 文本不能代替它。只改状态时 `data:{status:"OFF"}`、`updateMask:"status"`。租户状态为 `ON/OFF/FREEZE/EXPIRED`；延长有效期不会自动将冻结/过期状态恢复为 ON，页面应根据操作者意图明确提交状态。修改 code 会影响后续租户登录输入，应作明显提示。不开放 `adminUserId` 更新。

### 7. 可选配套：邀请与平台会话

**邮件邀请**复用 ACCOUNT-03 或 TENANT-06：设置顶层 `activationMode:"EMAIL_INVITATION"`，移除 password，普通建号增加 `data.email`，建租户增加 `user.email`。服务端会将账号设为 PENDING；须先具备可用 EMAIL 渠道和邀请 origin。邀请 24 小时有效，目前无重发/撤销管理接口，第一批可以只接立即激活。

自建接受邀请页时调用 AUTH-12：`POST /api/v1/auth/invitations/accept`，body 为 `{ "token": "<邮件链接片段中的令牌>", "password": "<用户输入的原始密码>" }`，无需 Bearer，成功返回 `{}`，随后进入原平台/租户登录页；不自动登录。邀请接受不依赖先调用绑定邮箱接口。

**平台会话管理**使用下表；默认租户管理员四条均未授权，第一批不在租户页面展示可操作入口。

| 编号 | 方法与路径 | 请求 | 响应 |
| --- | --- | --- | --- |
| SESSION-01 | `GET /admin/v1/online-session/sessions` | `page=1&pageSize=20&keyword=alice`；keyword 按用户名/IP 匹配 | `{items,total}` |
| SESSION-02 | `POST /admin/v1/online-session/force-logout` | `{userId,jti,clientType}`，均取目标会话记录 | `{}`；下线指定会话 |
| SESSION-03 | `GET /admin/v1/online-session/my-sessions` | 无参数 | `{items,total}`，本人会话，含 `current` 标记 |
| SESSION-04 | `POST /admin/v1/online-session/my-sessions/revoke` | `{jti,clientType}` | `{}`；下线本人指定会话 |

会话项含 `jti,userId,username,tenantId,clientType,ipAddress,userAgent,deviceId,loginAt`；`clientType` 为 `"admin"` 或 `"app"`，原样回传。`current` 仅在本人列表有意义。管理会话列表不支持独立 userId/tenantId 精确筛选参数，不能把前端过滤一页的结果和全局 total 当成某用户的完整会话分页。

### 8. 第一批暂缓项与后端跟进

| 接口 / 操作 | 暂缓原因或接入前置 |
| --- | --- |
| AUTH-15 `PUT /admin/v1/me` | 直接进入通用用户更新，可写角色；必须先由后端限定自助字段（MGMT-11）。页面隐藏字段无法消除已暴露接口的风险，正式开放给真实用户前需处理 |
| ACCOUNT-07 `POST /admin/v1/users/{user_id}/password` | 仍要求 AES+Base64，且 HTTP 路径模板与 API 目录不一致（MGMT-05）；建议本批先不开放重置密码按钮 |
| ACCOUNT-04 顶层 password、AUTH-16 `POST /admin/v1/me/password` | 同样仍是旧 AES 协议；不能复用创建/登录明文提交，也不以普通 PUT 夹带密码绕过前述问题 |
| ACCOUNT-08/09 用户名详情/删除别名 | 使用 ID 入口；用户名删除有实现缺口，平台同名账号存在跨租户歧义（MGMT-06） |
| ROLE-03～05、PERM-*、PERMGROUP-* 自定义角色与权限写入 | 四类页面先选择已有角色。运营/只读权限矩阵待定；权限点 PUT 丢数组会清绑定，删除保护不完整（MGMT-09/10/18） |
| TENANT-05 `DELETE /admin/v1/tenants/{id}` | 仅删除租户记录，不等于关联数据及下游资源清理；首批普通管理流程不开放 |
| TENANT-09 `POST /admin/v1/tenants/{id}/cleanup` | 删除实现列出的 Governance 本地数据，保留租户并置 OFF；不清下游资源，code/name 仍占用，需单独设计操作流程 |
| 主管理员移交 | 产品明确不提供，不调用通用更新改 adminUserId 代替移交 |
| 平台身份调用 API Key 管理 | 当前 Service 拒绝 tenantId=0；本批平台管理员页面不据种子授权矩阵添加该入口 |

前端可先完成列表/详情、现有角色选择、立即激活创建、管理侧基础编辑与启停；按上述限制处理错误和状态。后端应先收紧 `/me`，再按选定范围修复密码重置、角色变更后的旧会话和管理员保护问题。以上为交接建议，未在本次文档工作中实施修复或改变此前产品范围。

### 9. 联调检查与源码依据

联调至少分别使用平台管理员、租户 A 管理员、租户 B 管理员核对：正确列表范围、管理员角色筛选、创建后登录、更新 mask、启停与会话失效、无权限按钮/403、A 无法读写 B 的账号。邮件/MFA/会话功能在选择接入时另验。当前这些目标环境结果均为 `not_verified`，不得仅用 HTTP 200 或菜单出现判定完成。

接口以已注册 BFF 与 Service 为准，不能只按源领域 RPC 名或 OpenAPI 路径模板推导可用性。

- 路由：[用户](../api/protos/admin/service/v1/i_user.proto)、[租户](../api/protos/admin/service/v1/i_tenant.proto)、[角色](../api/protos/admin/service/v1/i_role.proto)、[套餐](../api/protos/admin/service/v1/i_plan.proto)、[认证](../api/protos/admin/service/v1/i_authentication.proto)、[会话](../api/protos/admin/service/v1/i_online_session.proto)。
- 报文：[User](../api/protos/identity/service/v1/user.proto)、[Tenant / TenantUsage](../api/protos/identity/service/v1/tenant.proto)、[Role](../api/protos/permission/service/v1/role.proto)、[Plan](../api/protos/identity/service/v1/plan.proto)、[登录](../api/protos/authentication/service/v1/ani_auth.proto)、[会话](../api/protos/online_session/service/v1/online_session.proto)。
- 实现：[用户校验与会话吊销](../app/admin/service/internal/service/user_service.go)、[用户筛选及查重](../app/admin/service/internal/data/user_repo.go)、[租户与首管理员开通](../app/admin/service/internal/service/tenant_service.go)、[邀请使用说明](onboarding.md)、[部署和权限目录前提](deployment.md)。

## 功能组：API Key / AK-SK

### 当前交付：AKSK-VPC-20260922（已实施及验收）

以 [执行文档](aksk-vpc-execution-plan.md) 为最终合同。下面旧版本排查保留为历史，不再描述当前代码。

| 编号 | 当前方法和路径 | 当前合同与验证 |
| --- | --- | --- |
| AK-01 | `GET /api/v1/auth/api-keys` | 用户 JWT；`items/total`，无 SK；真实 HTTP PASS |
| AK-02 | `GET /api/v1/auth/api-keys/{key_id}` | 用户 JWT；snake_case Key 信息、数字 id/role_id；跨租户 404，PASS |
| AK-03 | `POST /api/v1/auth/api-keys` | `{data:{name,role_id,expires_at?}}` → **201** `{data,secret_key}`；真实 HTTP PASS |
| AK-04 | `PUT /api/v1/auth/api-keys/{key_id}` | 仅 name/role_id/is_active/expires_at；外层 update_mask，值为 FieldMask lowerCamel；清空到期、启停、改绑 PASS |
| AK-05 | `DELETE /api/v1/auth/api-keys/{key_id}` | 200 `{status:"revoked"}`，后续签名 401，PASS |
| AK-06 | `PUT /api/v1/auth/api-keys/{key_id}/secret` | `{}` → `{data,secret_key}`；AK 不变，旧 SK 立即拒绝、新 SK 成功，PASS |
| AK-07 | 旧 `/admin/v1/access-keys/token` 已移除 | RPC/签发分支/白名单和 secret_hash 删除；旧路径 404，PASS |

Key 绑定同租户启用 TENANT 角色，复用套餐和 Casbin。主密钥文件必填，SK 使用专用 AES-256-GCM 实例加密；仅创建/重置返回一次，列表、详情、日志和审计无 SK/完整签名。每请求查询 Key 和角色状态，主体为独立 `api_key`，不伪造 user_id。首次种子为租户管理员授予六条管理权限；目标专用套餐经 API 开放 DASHBOARD/OPM/SYSTEM/NETWORK。

| 问题编号 | 本批结论 |
| --- | --- |
| AK-ISSUE-01 | 已解决：一个 Key 一个角色；无权限/跨租户/停用角色及改绑真实验收 PASS |
| AK-ISSUE-02 | 已解决：移除机器 JWT，逐请求签名与当前状态校验；停用/删除/到期/重置即时生效 PASS；无存量迁移任务 |
| AK-ISSUE-03 | 已解决：移除交换接口，审计使用已验证主体；SK/签名扫描及伪造身份回归 PASS |
| AK-ISSUE-04 | 已解决本批首次部署：空库种子权限 + 显式专用套餐与角色配置；租户管理员创建 Key PASS |
| AK-ISSUE-05 | 已解决 NET-01：共用 Principal 与下游 actor；Key 对用户专用/Key 管理接口仍 403，PASS |

证据及复现命令见 [本批记录](evidence/aksk-vpc-20260922/README.md)。本次仅持久化 VPC 查询，不证明网络数据面，也不开放其他业务 API。

### 实施前历史排查（以下记录截至源码 5a2a2e8，已由上述交付替代）

排查日期：2026-09-22，源码基线 `5a2a2e8`。本轮只排查与登记，没有修改实现、种子或运行环境。结论：**凭证管理、AK/SK 换 JWT 已有实现，但默认初始化后尚不能完成受控业务 API 调用，不能算完整可用。**

### 已有能力与边界

- 支持创建多个凭证、分页列表、详情、修改名称/启停/有效期、删除、重置 SK；创建和重置响应各返回一次明文 SK，列表/详情不返回 SK。凭证表只存 SK 的 SHA-256 摘要；审计路径另有泄露问题，见 AK-ISSUE-03。
- 凭证归属当前操作者的租户，服务端覆盖请求中的 `tenantId`。平台账号创建的是租户 `0` 的凭证，不能通过传入其他 `tenantId` 代建租户凭证。现有租户过滤和写入守卫适用于凭证表；本轮未做完整跨租户 HTTP 验收。
- AK/SK 先换短期 JWT，再携带 `Authorization: Bearer <accessToken>` 调 API。现有链路没有直接传 `X-API-Key`、把 SK 当 Bearer、或逐请求 HMAC 签名的用法。
- 换令牌时校验摘要、凭证状态和有效期，包含 IP+AK 失败限流；凭证 `expiresAt` 留空表示长期有效。`lastUsedAt` 只表示最近换令牌时间，不是最近业务调用时间。
- 机器令牌无 refresh token、不走用户登录 Cookie。到期重新提交 AK/SK；JWT 有效期共用 `authn.jwt.access_token_expires`，配置样例为 `5400s`，未配置时回退 `900s`，以响应 `expiresIn` 为准。JWT 有效期没有截断到凭证到期时间。

### 接口与当前报文

除 AK-07 外均需已登录管理员的 Bearer 和相应 API 权限；租户上下文还受套餐模块限制。API Key 管理当前归类 `SYSTEM`，默认套餐只有 `DASHBOARD`、`OPM`，默认租户管理员也未授予这些管理 API；平台管理员已有管理授权。

| 编号 | 方法与路径 | 请求 / 响应与用途 |
| --- | --- | --- |
| AK-01 | `GET /admin/v1/access-keys` | `pagination.PagingRequest`；返回 `{items,total}`，不含 SK |
| AK-02 | `GET /admin/v1/access-keys/{id}` | 返回凭证详情，不含 SK |
| AK-03 | `POST /admin/v1/access-keys` | `{data:{name,status,expiresAt}}`；返回 `{data:{id,accessKey,...},secret}`，AK/SK 由服务端生成 |
| AK-04 | `PUT /admin/v1/access-keys/{id}` | `{data:{...},updateMask:"status"}` 等；可改名称、状态、有效期，不能改 AK、摘要或租户；返回空对象 |
| AK-05 | `DELETE /admin/v1/access-keys/{id}` | 删除凭证；返回空对象 |
| AK-06 | `PUT /admin/v1/access-keys/{id}/secret` | 请求体 `{}`；返回 `{data,secret}`，旧 SK 不能再换新令牌 |
| AK-07 | `POST /admin/v1/access-keys/token` | `{accessKey,secret}`；无需登录 Bearer；返回 `{accessToken,expiresIn,tokenType}` |

这些接口沿用 camelCase，不能直接套用 AUTH-01/02 的 `access_token` 响应解析。没有独立 Count HTTP 路由。

### 当前调用方式及卡点

以下展示现有协议，第三步仍受下述授权缺口阻断；不是已完成的业务接入验收。真实 AK/SK 只应经 HTTPS 提交并保存在调用方服务端，不能放进浏览器前端。

1. 使用有管理权限的账号登录，再创建凭证：

   ```http
   POST /admin/v1/access-keys
   Authorization: Bearer <管理员访问令牌>
   Content-Type: application/json

   {"data":{"name":"业务脚本","status":"ON"}}
   ```

   保存响应 `data.id`、`data.accessKey` 和 `secret`。需要固定到期时间时，增加 RFC3339 格式的 `expiresAt`。不要依赖重新查询取回 SK，丢失后只能重置。

2. 调用方用 AK/SK 换令牌，不需要先登录：

   ```http
   POST /admin/v1/access-keys/token
   Content-Type: application/json

   {"accessKey":"ak-<创建时返回的值>","secret":"sk-<创建或重置时返回的值>"}
   ```

   响应字段为 `accessToken`、`expiresIn`（秒）、`tokenType`（`bearer`）。缓存令牌，在到期前后按需重新交换，不必每次业务请求都提交 SK。

3. 业务请求带 `Authorization: Bearer <accessToken>`。当前令牌固定 `roles=["machine"]`、`userId=0`；种子没有这个角色的业务授权，也不会继承创建者权限，因此默认 Casbin 下受保护 API 会被拒绝。仅拿到 JWT 不代表已能调用业务接口。

停用示例：`PUT /admin/v1/access-keys/{id}`，请求体 `{"data":{"status":"OFF"},"updateMask":"status"}`。它目前只阻止后续换令牌，**不会立即吊销已经发出的 JWT**；删除和重置同样存在这个限制。

### 已发现的缺口

| 编号 | 已核对的事实及影响 | 状态 / 最小处理方向 |
| --- | --- | --- |
| AK-ISSUE-01 | Key 没有角色/权限绑定字段；所有机器 JWT 固定 `machine` 角色，种子没有相应策略；不继承创建者权限。手动给同一租户的 `machine` 角色授权会使该租户所有 Key 共用权限，仍无法分别控制 | 未修复。最小方向是复用现有租户角色和 API 权限，为 Key 明确绑定授权，不另建 IAM |
| AK-ISSUE-02 | 停用、删除、重置、凭证到期只阻止新交换；后续 JWT 校验只验签/有效期/Redis/黑名单，不复查对应 Key。令牌使用 `userId=0` 入 Redis，无按 Key 的吊销索引，也不登记普通在线会话 | 未修复。不能套用本轮已完成的用户账号会话吊销；需要按 Key 使旧令牌失效，或在认证时复查 Key 当前状态 |
| AK-ISSUE-03 | IssueToken 经过通用 API 审计；既没有归入跳过请求体的登录操作，也没有邀请接口的脱敏分支，提交的 SK 会进入审计请求体 | 未修复。换令牌入口须避免记录 `secret`；凭证表存摘要并不能解决这个泄露路径 |
| AK-ISSUE-04 | 默认租户管理员没有 Key 管理 API 授权，默认套餐也没有其 `SYSTEM` 模块；平台创建的 Key 固定归属租户 0 | 未修复。租户自助使用前需明确并显式补齐所需授权/套餐，不通过启动重刷种子或清空 API 表处理 |
| AK-ISSUE-05 | 机器令牌 `userId=0`，部分业务入口要求真实用户；例如 `NetworkService.GetVPC` 明确拒绝零用户 ID，补角色授权后仍然不能调用 | 未修复。按实际要开放的业务 API 适配机器身份并验收，不能声称所有接口天然支持 API Key |

如后续要求补齐最小闭环，范围限于：选定实际要调用的 API、复用角色授权、处理 Key 失效与审计脱敏，并显式配置所需初始化授权。不自动引入跨服务配额、独立认证服务或全平台机器身份框架。

### 请求签名模式的改造评估（待决定，未实施）

用户询问改成“客户端用 SK 为每次请求签名，直接调用业务 API”的成本。本项只是方案评估，尚未实施。此前覆盖通用 HTTP JSON 签名、迁移和较完整验证的初估为 3～5 个开发人天；后续用户要求先跑通主流程，收窄后的第一批范围和估算见文末，不以此前清单作为全部首批要求。

- 认证入口：现有 `auth.Server` 固定提取 Bearer；需要识别签名凭证、验签后建立可信机器身份，再复用租户/套餐、Casbin 与 Ent 上下文。要明确签名失败不回退到另一种认证方式，避免混用凭证绕过校验。
- 请求协议：确定方法、实际路径、查询参数、签名头及原始请求体摘要的规范化规则，检查时间窗并明确重放处理；客户端与服务端必须对同一字节内容计算签名。AWS 的规范化规则可作参考，但采用相似方式不等于兼容其 SDK，参见 [SigV4 请求签名步骤](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_sigv-create-signed-request.html)。
- 密钥与结构：当前代码只存 `SHA-256(SK)`，无法直接用于验证以原 SK 为密钥的常规 HMAC 签名。用户随后明确没有旧 Key、没有存量用户，因此直接改为加密 SK 并移除未使用的摘要/交换代码，不做旧凭证转换或兼容。主密钥由部署环境单独保管，结构由 Atlas 显式管理。现有加密工具允许缺密钥时明文直通，SK 场景必须拒绝这种降级。
- 授权和审计：仍需补齐 AK-ISSUE-01/04/05；改用签名不会自动给机器身份授权。审计当前从 Bearer 解析身份，需改为可靠记录已认证的 Key 与租户，避免记录密钥或可重放的完整认证材料。
- 代码替换：移除换 JWT 入口、签发分支和对应免鉴权登记；普通用户登录 JWT 继续使用。按无存量前提，不增加历史机器 JWT 回收或切换流程。每次验签读取 Key 当前状态时，停用/删除/重置可在后续请求生效，但不能撤回已通过认证并开始执行的请求。
- 验收：除成功调用外，至少覆盖请求篡改、时间边界、禁用/过期 Key、跨租户及越权拒绝、密钥不进入日志、首次初始化，以及普通登录 JWT 回归。只写出 HMAC 计算函数不算完成。

### 本轮验证

源码链路已核对：HTTP 路由 → Service → Repo/Schema → 机器 JWT 签发及 Redis 校验 → Casbin 策略来源与租户套餐 → API 审计。

- Ubuntu 独立源码快照、Go 1.26.7、SQLite + miniredis：`go test ./app/admin/service/internal/data ./app/admin/service/internal/service -run '^TestAccessKey' -count=1 -v`，既有 AccessKey 定向用例全部 PASS。
- 仅在远端快照增加诊断探针：分别停用、删除、重置和把有效期改至过去后，旧 AK/SK 已不能换新令牌，但原 JWT 仍能通过真实签名与 Redis 缓存校验；四种情形均复现。到期情形通过修改有效期模拟，不是墙钟等待到期。
- 使用生成的 AccessKey HTTP 路由、真实 IssueToken Service 和通用审计中间件，在进程内提交测试 AK/SK，确认明文 SK 到达 API 审计写入回调；没有使用真实凭证，也没有连接用户审计数据库。生产装配将该回调接到 ApiAuditLogRepo，实际已部署环境是否存有历史泄露记录未排查。
- 诊断用例的 PASS 表示成功复现现状，**不是缺口已修复**；探针仅保留在远端证据目录，未加入仓库回归测试。证据：`/home/ubuntu/Workspace/.codex-runs/governance-access-key-audit-20260922/access-key-audit.log`；探针位于其 `src/app/admin/service/internal/service/access_key_audit_probe_test.go`。
- 本轮仓库只修改本登记文件；没有部署或修改运行数据库。kind、真实 PostgreSQL/Redis 与业务 API 全链路为 `not_verified`。

源码入口：[HTTP 路由](../api/protos/admin/service/v1/i_access_key.proto)、[报文](../api/protos/access_key/service/v1/access_key.proto)、[业务实现](../app/admin/service/internal/service/access_key_service.go)、[仓储](../app/admin/service/internal/data/access_key_repo.go)、[表定义](../app/admin/service/internal/data/ent/schema/access_key.go)、[机器令牌签发与校验](../app/admin/service/internal/data/authenticator.go)、[角色策略来源](../app/admin/service/internal/data/authorizer_provider.go)、[API 审计](../pkg/middleware/logging/api_audit_log.go)、[首次初始化 SQL](../sql/bootstrap/001_initial.sql)、[VPC 入口身份检查](../app/admin/service/internal/service/network_service.go)。

## 功能组：VPC 详情查询的机器调用

### 当前交付：NET-01 / NET-ISSUE-01

`GET /api/v1/networks/vpcs/{vpc_id}` 已接受用户 JWT 或三个签名头之一整组，禁止混用；只接受规范 VPC 路径和空 query/body。可信主体经公共认证层进入租户/套餐/Casbin，数值租户经持久化 resource_tenant_id 映射，下游统一生成 `governance:user:<id>` 或 `governance:access-key:<id>`。

Network 在 66f787b 上仅整合 9e56e1c 必要 vpc-read 改动，保留主线 BaseConnectivity 映射和租户过滤，加入 Key actor；没有整支合并平台工作或改名。真实 NodePort → Governance → mTLS → Network → PostgreSQL 已 PASS；跨租户/不存在同样 404（仅 request_id 不同），伪造公网身份无效，缺/错误证书与 RPC/header 租户不一致拒绝，用户 JWT 查询与登出回归 PASS。NET-ISSUE-01 本批已解决，full 模式及其他 RPC 仍不在开放范围。

本次运行证据见 [本批记录](evidence/aksk-vpc-20260922/README.md)，以下保留实施前的分支核对和范围形成记录。

### 实施前历史评估

2026-09-22 只读评估。Governance 基线 `5a2a2e8`；本地 Network 源码 `66f787bd30134141726c596612501a83cf75bdb7`，与 Governance `go.mod` 固定的 API 模块版本一致。这里核对的是源码，不能据此推断实际部署镜像与实验接收端相同；本轮未部署、未运行跨服务验收，也未修改 Network 仓库。

| 编号 | 方法与路径 | 请求 / 响应 | 当前条件 |
| --- | --- | --- | --- |
| NET-01 | `GET /api/v1/networks/vpcs/{vpc_id}` | 路径 ID 为 `vpc_` + 32 位小写十六进制；无 body、拒绝查询参数；返回 `{vpc:{...}}` | 目前需要用户 JWT、非零租户与用户 ID、角色的 `network:vpc:get` API 授权、套餐 `NETWORK` 模块；不存在或跨租户资源按 404 处理 |

### 认证改造影响

无需替换现有 Casbin、套餐或租户隔离规则。建议由 Governance 入口校验 AK/SK 签名并建立可信的 Key ID、租户、角色上下文，再复用既有 API 权限和套餐检查。客户端仍调用 NET-01，不直接访问 Network 内部 gRPC；原始 SK 不传下游。

Governance 的两处适配必须修改：[NetworkService.GetVPC](../app/admin/service/internal/service/network_service.go) 拒绝 `userId=0`；[NetworkClient.GetVPC](../app/admin/service/internal/data/network_client.go) 也拒绝零用户 ID，并固定生成 `governance:user:<uid>`。需接受经过认证、授权的 Key 身份并保留可区分的审计标识（例如 `governance:access-key:<id>`，仅为建议，尚未冻结合同），不能简单删除守卫或伪造用户 ID。角色绑定缺口仍按 AK-ISSUE-01 处理。

**接收端的分支整合差异（NET-ISSUE-01，历史核对后纠正）**：此前仅检查 Network 当前 main `66f787b`，把缺失描述成“尚未实现、需要新补接收层”，不够准确。mTLS 没有被删除：[提交 `9e56e1c`](https://github.com/zhangzhe-ctrl/ani-network-service/commit/9e56e1c675bb2102c8e84adc5a8dfd2962823ddd) 已实现 Governance VPC 只读接收，保留在 `codex/install-ceph` 及对应远端跟踪分支；后续 `f198a1e` 没有删除它，当前 main 不包含该提交。源码包括 `internal/server/governance.go`、`cmd/ani-network-service/vpc_read.go`，需显式启用 `ANI_NETWORK_MODE=vpc-read`；full 模式仍保留历史认证延期边界。Governance 自己的 Network mTLS 客户端和共享证书配置也仍在。

该分支已有的 [GovernanceResolver/Unary](https://github.com/zhangzhe-ctrl/ani-network-service/blob/9e56e1c675bb2102c8e84adc5a8dfd2962823ddd/internal/server/governance.go) 校验证书链及精确 SAN `ani-governance`，随后信任它声明的单值 `x-ani-tenant-id`、`x-ani-actor`、`x-ani-request-id`，仅允许 GetVPC；校验 RPC 请求 `tenant_id` 与可信 header 一致。**Network 不重新查询用户归属或角色权限**，这些由 Governance 保证；一致性检查避免请求体的重复租户字段绕过 header 范围，并非另一套租户授权。当前 actor 格式只接受 `governance:user:<非零ID>`，接 API Key 时需扩展为可区分的机器 actor，同时复用原证书和租户信任链。

Network 的 [GetVPC SQL](../../ani-network-service/internal/data/queries/vpcs.sql) 已按 `tenant_id + vpc_id` 过滤，同租户关联也有条件；请求签名不要求改 VPC 查询或领域模型。后续应先整合已有接收实现，再适配 Key 身份，不重新建设 mTLS 或引入 IAM。Governance 的 `go.mod` 只固定消费的 API 模块，不决定 Network 实际运行的镜像/源码版本；2026-09-21 `a936623` 将模块 pin 更新为 main `66f787b`，没有删除独立分支的服务端代码。

历史 [2026-09-19 执行记录](https://github.com/zhangzhe-ctrl/ani-network-service/blob/9e56e1c675bb2102c8e84adc5a8dfd2962823ddd/docs/execution/records/governance-vpc-read-20260919.md) 保存 VPC 只读链路与 mTLS/隔离验收，不能把这些说成从未实现；它也不证明当前部署或新增 API Key 链路已通过。当前部署版本本轮未检查；分支整合、机器 actor、签名调用和原 JWT 回归仍为 `not_verified`。原“不含下游合同调整”的 3～5 人天估算不包含分支整合与两仓联调。

### 第一批收窄：先跑通签名查询 VPC

用户要求先跑通主流程，后续按实际问题迭代。第一批只验收“管理员配置租户 Key → 客户端签名 → Governance 验签和既有权限检查 → mTLS 调 Network GetVPC → 返回本租户 VPC”；不把所有业务 API、通用云厂商兼容列为前置。用户已明确没有旧 Key、没有存量用户，按首次部署实施。沿用此前 2～3 个开发人天的粗估，不是交付承诺。执行依据见 [AK/SK 执行文档](aksk-vpc-execution-plan.md)，尚未改实现。

- Governance：复用 AK 管理，增加加密 SK 和一个同租户角色绑定；入口增加签名认证，首批仅开放 NET-01，其他业务接口按后续批次接入。租户状态、套餐和 Casbin 继续复用。每次查 Key 当前状态即可，先不做权限/密钥缓存。
- Network：整合现有 `9e56e1c` 的 vpc-read 入口，扩展其 actor 校验支持 Key；不重写 mTLS、可信租户 header 或 VPC 查询。`git merge-tree --write-tree main 9e56e1c` 只预览、未实际合并：唯一文本冲突在 `internal/data/postgres.go`，一侧增加 BaseConnectivity 返回映射，另一侧增加连接失败错误分类，需同时保留。仍需实际编译和回归，合并预览不算验证通过。
- 数据与调用：Atlas 显式建表、首次种子授权；给目标租户配置 VPC 只读权限、SYSTEM/NETWORK 套餐；提供 Python 签名示例。直接移除未使用的交换代码，保留普通用户 JWT；不做旧 Key 迁移、存量增量授权补丁、停机切换或恢复演练。
- 必要验证保留：真实查询成功，错误/过时签名、无权限、跨租户、停用 Key 拒绝，以及原用户 JWT 查询仍成功；SK 加密存储且不进入日志。首批 GET 只读接口允许时间窗内重复查询，不宣称一次性请求或通用防重放；写接口接入时再确定相应重放/幂等语义。
- 后续再做：其他业务 API 和写请求、全功能 SDK、无中断密钥轮换、复杂权限组合、自助权限管理界面、性能优化和全面故障恢复验收。不会因本次收窄关闭 AK-* / NET-* 的未完成登记。

### API Key 风格对照与声明预览（历史方向，现已按执行文档实施）

2026-09-22：用户明确将**共用认证层**纳入范围，参照 [ANI OpenAPI](../../ANI/repo/api/openapi/v1.yaml) 查看预览后确认方向，并要求编写含 Python 示例的执行文档。用户进一步明确**没有旧 Key、没有存量用户**。后续以 [执行文档](aksk-vpc-execution-plan.md) 的接口、签名规范、首次部署与验收步骤为准；本节保留原样式对照；实现和真实运行结果以上述当前交付及执行文档为准。

参照文件在 `servers.url` 中已有 `/api/v1`，Key 路由写作 `/auth/api-keys`，完整路径为 `/api/v1/auth/api-keys`。其 `ApiKeyAuth` 使用单个 `X-API-Key` 长期凭证；创建字段为 `name/scopes/user_id/rate_limit_rpm/expires_at`，创建响应为 `key_id/key_value/key_prefix`，列表为 `items/total`。**这是静态 API Key 声明，不是 AK/SK 请求签名协议。**

建议沿用路径和 snake_case 外观，具体差异如下：

| 项目 | 本批建议 |
| --- | --- |
| 路径 | `/api/v1/auth/api-keys`，明细及操作使用 `/{key_id}` |
| 字段 | `access_key`、`secret_key`、`role_id`、`expires_at`、`is_active` 等使用 snake_case；API Key ID 继续使用当前数字类型 |
| 创建请求 | 保留本仓 `{data:{...}}` 约定；`name/role_id` 必填，`expires_at` 可选，默认启用 |
| 授权 | 一个同租户 `role_id` 复用现有角色及 API 权限；不复制旧 `scopes` 体系。可绑定角色需经过授权校验，不因为知道角色 ID 就能任意绑定 |
| 身份 | 租户和创建者来自可信认证上下文；不开放旧 `user_id` 代建参数 |
| 限流 | 首批不暴露 `rate_limit_rpm`，避免声明尚未实现的单 Key 限流能力 |
| 列表 | 沿用 `items/total`；保留现有分页机制，页参数暂不在本批统一改名。旧 YAML 的 Key 列表本身没有 cursor 参数，不因此新增游标分页 |
| 秘密 | 旧 `key_value` 拆成公开 AK 与一次返回的 SK；SK 仅创建/重置响应返回，后续读取不返回，服务端加密存储以支持 HMAC 验证 |
| 更新 | 保留 PUT 和 `update_mask` 部分更新语义，`is_active` 映射现有 ON/OFF；实现时须核对 FieldMask 的 JSON 编解码，不能仅改字段名就声称掩码路径兼容 |

以下均为**目标路由**，现有 AK-01～07 的当前路由仍以原登记为准：

| 现有编号 | 建议目标 | 行为与响应 |
| --- | --- | --- |
| AK-01 | `GET /api/v1/auth/api-keys` | 列表，200，`{items,total}`，不含 SK |
| AK-02 | `GET /api/v1/auth/api-keys/{key_id}` | 详情，200，Key 信息，不含 SK |
| AK-03 | `POST /api/v1/auth/api-keys` | 创建，建议 201，`{data,secret_key}` |
| AK-04 | `PUT /api/v1/auth/api-keys/{key_id}` | 修改名称、绑定角色、有效期或启停状态；`{data,update_mask}`，200 |
| AK-05 | `DELETE /api/v1/auth/api-keys/{key_id}` | 吊销并删除，建议 200，`{status:"revoked"}` |
| AK-06 | `PUT /api/v1/auth/api-keys/{key_id}/secret` | 重置 SK，200，`{data,secret_key}`，旧 SK 后续请求失效 |
| AK-07 | 不提供新的换 JWT 路由 | 签名直接调用业务 API；删除未使用的交换路由与签发代码，不做存量兼容 |

Key 管理路由使用用户 Bearer JWT 并要求对应管理权限；不因为新增签名认证就允许 Key 管理其他 Key。首次种子必须引用最终路径、补齐租户管理员授权，空库通过 admin init 显式初始化；不写存量路径迁移脚本。

创建预览（角色 12、ID 42 均为示例，不是初始化约定）：

```http
POST /api/v1/auth/api-keys
Authorization: Bearer <用户 JWT>
Content-Type: application/json

{"data":{"name":"vpc-reader","role_id":12,"expires_at":"2026-12-31T23:59:59Z"}}
```

```json
{
  "data": {
    "id": 42,
    "name": "vpc-reader",
    "access_key": "ak-...",
    "role_id": 12,
    "is_active": true,
    "expires_at": "2026-12-31T23:59:59Z"
  },
  "secret_key": "sk-..."
}
```

业务调用外观如下；头名和准确签名输入已写入执行文档。客户端以 SK 计算 HMAC-SHA256 签名，业务请求不发送 SK，也不预先换 JWT。首批仍只有 NET-01 签名闭环，不扩大为全部业务 API。

```http
GET /api/v1/networks/vpcs/vpc_0123456789abcdef0123456789abcdef
X-Access-Key: ak-...
X-Timestamp: <当前 Unix 秒时间戳>
X-Signature: <HMAC-SHA256 签名的十六进制值>
```

签名绑定方法、实际路径、查询、AK、时间戳和原始请求体摘要；首批只读 VPC 的 query/body 为空，七行字节规则及 300 秒时间窗详见执行文档第 3 节，Python 示例见第 7 节，现已落为可运行文件并完成真实联调。

OpenAPI 认证声明示意（片段，省略消息和响应定义）：

```yaml
components:
  securitySchemes:
    BearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT
    AccessKeyAuth:
      type: apiKey
      in: header
      name: X-Access-Key
      description: 公开 AK，不是 SK
    SignatureAuth:
      type: apiKey
      in: header
      name: X-Signature
      description: 使用 SK 对当前请求计算的 HMAC-SHA256 签名
    SignatureTime:
      type: apiKey
      in: header
      name: X-Timestamp
      description: 签名时间，Unix 秒

# NET-01 的 operation 级配置；不作为所有接口的全局默认值
security:
  - BearerAuth: []
  - AccessKeyAuth: []
    SignatureAuth: []
    SignatureTime: []
x-ani-authz:
  principal_kinds: [user, api_key]
```

按 [OpenAPI Security Requirement](https://spec.openapis.org/oas/v3.1.0.html#security-requirement-object) 语义，数组中的两项为二选一；同一项中的 AK、签名和时间戳要求同时提供。`type: apiKey` 在这里描述头部传输位置，不代表服务端直接信任 AK；扩展字段只是文档说明，实际支持范围仍需代码执行。OpenAPI 声明也不会自动为 Swagger UI 或生成客户端计算 HMAC。

共用认证层的目标：用户 JWT 和 Key 签名各自验证后，统一产出调用主体类型、主体 ID、租户和角色，再走现有套餐/Casbin；业务入口从同一可信上下文取身份，下游调用也共用 actor/header 生成逻辑。首批改好 VPC 入口及通用接入示例，后续新接口只声明支持的主体并登记权限，不逐个重写验签；既有硬编码非零用户 ID 的入口仍需按实际接入修正。用户专用功能继续只接受用户身份。

验证范围：只读对照两份源声明并登记草案；未生成、未构建、未部署，签名协议及新版路由均 `not_verified`。

## 功能组：通用配额与 GPU 本地模拟

2026-09-22 用户要求制定详细、强约束的执行计划；本轮只写计划，不实施功能。执行入口为 [本地执行计划](quota-gpu-local-execution-plan.md)，批次 QUOTA-GPU-LOCAL-01。**这批次所有的工作都可以在本地完成**，真实 GPU 服务尚未准备好，使用独立进程、真实 PostgreSQL 和持久 GPU 模拟器验收；不能将模拟结果标成真实 GPU 接入成功。

2026-09-22（同日第二批）：QUOTA-GPU-LOCAL-01 已按计划 P0～P8 本地执行完成：目录/账本/单次占额/累计退额/持久化转发与恢复/内部 mTLS 退额/本地 GPU 模拟闭环实现并通过指定验收（见 [验收证据](evidence/quota-gpu-local-01/README.md)）；QUOTA-01/02/03、QUOTA-LAB-01～04 标记为已实现（lab 路由仅 quota_lab 构建）。真实 GPU 服务接入与真实硬件分配仍为 not_verified，未标成接入成功。

用户确定：Governance 是统一入口，在转发前一次占额；成功不再实扣。资源服务只上报可退额事实，Governance 配额模块不维护资源运行状态。请求超时/操作失败本身不导致退额；只有确定创建已封闭且没有对应资源，或资源实际释放，才归还。计划采用累计 released_total 防重复/乱序多退。

### 计划接口与鉴权

以下全部为拟新增或拟扩展合同，**不是当前可调用接口**。

| 编号 | 方法/路径 | 报文与职责 | 鉴权、套餐关系、状态 |
| --- | --- | --- | --- |
| QUOTA-01 | GET /admin/v1/quota-definitions | 配额目录 code/name/unit/kind/enforcement；沿用分页 | 平台管理读权限，TENANT 模块；已实现（本地验收通过） |
| QUOTA-02 | GET /admin/v1/tenants/{id}/quota-accounts | 指定租户 limit/occupied/available/overLimit；占额不等于 Running GPU | 平台管理读权限，TENANT 模块；已实现（本地验收通过） |
| QUOTA-03 | gRPC /quota.service.v1.QuotaReleaseService/ReportQuotaRelease | event ID、原 operation、charge 与累计释放量、原因；没有公共 HTTP 映射 | 独立 listener、精确 owner mTLS 身份、账本归属校验；内部退额不受套餐到期阻断；已实现（本地验收通过） |
| QUOTA-LAB-01 | POST /api/v1/quota-lab/gpu-allocations | data.name/gpuCount，Idempotency-Key；202 与稳定操作/占额/资源 ID | 仅 quota_lab 构建；真实用户 JWT、角色与套餐检查，实验目录归 TENANT；已实现（本地验收通过） |
| QUOTA-LAB-02 | GET /api/v1/quota-lab/gpu-allocations/{resource_id} | 经 Governance 查询模拟资源，不扣额 | 同上，限本租户；已实现（本地验收通过） |
| QUOTA-LAB-03 | DELETE /api/v1/quota-lab/gpu-allocations/{resource_id} | 幂等转发删除，接受删除不立即退额 | 同上，限本租户；已实现（本地验收通过） |
| QUOTA-LAB-04 | GET /api/v1/quota-lab/operations/{operation_id} | 投递状态与账本标识，不维护资源 Running 状态 | 同上，限本租户与操作主体；已实现（本地验收通过） |
| 复用 PLAN-11～14 | 现有套餐配额 CRUD | 拟增加 quotaCode、唯一/必填/数值范围约束；旧枚举本批 deprecated 兼容 | 沿用平台套餐管理权限；未改现有接口 |
| 复用 TENANT-04/08 | 现有租户绑定与 usage | 绑定/到期复用；旧 QuotaUsage 仅补 code，旧统计能力不扩大 | 未改现有接口 |

### 已登记边界与执行约束

- QUOTA-ISSUE-01：当前固定配额类型只有 USER_LIMIT/STORAGE/API_CALL。计划将目录与额度计算规则分开，新增 gpu.count 只具备 LAB_ONLY 执行能力；不通过新增枚举假装完整接入。
- QUOTA-ISSUE-02：当前没有占额账本、稳定幂等转发及内部 mTLS 退额接收端。现有 Network request-id 和 last_operation_id 不代表已实现这些能力。
- QUOTA-ISSUE-03：PlanQuota/QuotaUsage 增加 code 使用新 Proto tag；旧数据要精确回填，NULL/重复/未知类型拒绝迁移，不自动合并。Atlas 升级与恢复只在本任务隔离库验证。
- QUOTA-ISSUE-04：实验 GPU 路由、故障入口和模拟 owner 不进入正式构建/正式 OpenAPI/首次种子。正常构建的真实 GPU 创建释放仍 not_verified。
- QUOTA-ISSUE-05：计划对已有配额历史的租户增加物理删除保护；对绑定租户的套餐保留删除保护，具体升级行为须在执行验收中验证并交接。套餐降额/切换不清空既有占额。
- QUOTA-ISSUE-06：本批不调整全局到期后用户 DELETE 权限；内部可信资源释放通道必须持续可用。旧三个配额项仍保持配置/现有统计，不宣称强制拦截已接入。

本轮验证仅为源码核对、计划交叉审查与文档检查；实现、生成、迁移、本地模拟验收均未执行。整体登记继续进行中。

## 功能组：审计归属地停止采集（IMAGE-SLIM-01）

2026-09-23 镜像瘦身批次移除 GeoIP（GeoLite2）依赖，执行计划见 [镜像瘦身与二进制拆分执行计划](image-size-reduction-plan.md)。影响面是**既有审计写入的报文内容**，接口路由与鉴权条件不变。

| 编号 | 涉及接口/报文 | 变更内容 | 鉴权、套餐关系、状态 |
| --- | --- | --- | --- |
| AUDIT-GEO-01 | 登录审计、API 审计、操作审计（写入）及对应审计查询响应中的 `geo_location` 子消息 | 不再采集 IP 归属地，新记录 `geo_location` 为 NULL/空；`ip_address`、设备信息、风险评分与签名不变 | 鉴权与套餐不变；Proto/Ent/DB 字段保留，字段删除另开一批；已实现（本机编译 + 定向测试通过，真实落库与查询展示 `not_verified`） |

### 已登记边界与执行约束

- AUDIT-GEO-ISSUE-01：登录策略 REGION 维度此前就未判定，移除地理库后仍无数据来源，登录闸门行为不变；IP/TIME/DEVICE 维度不受影响。
- AUDIT-GEO-ISSUE-02：历史审计行的 `geo_location` 保持原值，本批不做数据迁移；前端与下游消费方需容忍空值。
- AUDIT-GEO-ISSUE-03：本批不动 Proto/DB 字段；字段删除与对应 Atlas 迁移另开一批，不在本批声称完成。
- AUDIT-GEO-ISSUE-04：镜像形态变化（服务镜像与运维 CLI 镜像拆分、基础镜像换 distroless）不属于接口变更，登记在此仅为可追溯；部署命令变更见 [部署文档](deployment.md)。

本轮验证：本机 `go build ./...`、`go vet ./pkg/middleware/logging/...`、`go test ./pkg/middleware/logging/...` 通过；审计落库、查询响应与前端展示未验证（`not_verified`）。

## 功能组：Accelerator v1.2 管理与租户 BFF（GOV-ACC-V12-01）

2026-09-23 本批新增独立 `ACCELERATOR=13` 模块，保留旧枚举。正式依赖固定为
`github.com/zhangzhe-ctrl/ani-accelerator-service v0.0.0-20260923100416-9f9712198488`；
实际测试/后续升级版本以本批证据锁为准。以下为实现合同登记，运行通过状态须引用本批证据，不能继承历史 QUOTA-LAB 结论。

| 编号 | 方法/路径 | 下游 RPC / 语义 | 权限 |
| --- | --- | --- | --- |
| ACC-01 | GET /admin/v1/accelerator/clusters | ListClusters | accelerator:cluster:list |
| ACC-02 | POST /admin/v1/accelerator/clusters | RegisterCluster；data.display_name/connection_ref/idempotency_key | accelerator:cluster:register |
| ACC-03 | GET /admin/v1/accelerator/pools | ListPools；cluster_id | accelerator:pool:list |
| ACC-04 | POST /admin/v1/accelerator/pools | CreatePool；data.cluster_id/display_name/idempotency_key | accelerator:pool:create |
| ACC-05 | GET /admin/v1/accelerator/supply-groups | ListSupplyGroups；pool_id | accelerator:supply-group:list |
| ACC-06 | POST /admin/v1/accelerator/supply-groups:adopt | AdoptSupplyGroup；data 中固定节点、baseline、mode、queue；无 verification 写字段 | accelerator:supply-group:adopt |
| ACC-07 | POST /admin/v1/accelerator/supply-groups/{group_id}:set-admission | SetAdmission；data.expected_version/desired/reason/idempotency_key | accelerator:supply-group:set-admission |
| ACC-08 | GET /admin/v1/accelerator/profiles | AdminListProfiles；cluster_id | accelerator:profile:admin-list |
| ACC-09 | POST /admin/v1/accelerator/profiles | PublishProfile；data.spec/expected_group_version/verification_ref/idempotency_key | accelerator:profile:publish |
| ACC-10 | GET /admin/v1/accelerator/devices | ListDevices；cluster_id、可选 group_id | accelerator:device:list |
| ACC-11 | GET /admin/v1/accelerator/bindings | AdminListBindings；cluster_id、可选 physical_device_id；包括未关联事实 | accelerator:binding:admin-list |
| ACC-12 | GET /admin/v1/accelerator/capacity | AdminGetCapacity；profile_id/profile_version/include_devices | accelerator:capacity:admin-get |
| ACC-13 | GET /api/v1/accelerator/profiles | ListProfiles；cluster_id、可选 mode | accelerator:profile:list |
| ACC-14 | GET /api/v1/accelerator/profiles/{profile_id} | GetProfile；具体 profile_version | accelerator:profile:get |
| ACC-15 | GET /api/v1/accelerator/capacity | GetCapacity；具体 profile/version；租户合同无 include_devices 字段，永不返回设备明细 | accelerator:capacity:get |
| ACC-16 | POST /api/v1/accelerator/admission-preview | ResolveGpuRequest + CheckGpuFit + 本租户额度；只读，不返回 plan/runtime | accelerator:admission:preview |
| ACC-17 | GET /api/v1/accelerator/usages | ListGpuUsages；可选 owner_service/state | accelerator:usage:list |
| ACC-18 | GET /api/v1/accelerator/usages/{owner_service}/{resource_id} | 本库可信原 CREATE 构造 ref → GetGpuUsage | accelerator:usage:get |
| ACC-19 | GET /api/v1/accelerator/usages/{owner_service}/{resource_id}/bindings | 同上 → ListBindings；返回独立脱敏 DTO | accelerator:binding:list |
| QUOTA-04 | GET /api/v1/me/quota-accounts | 复用 QUOTA-02 账户读模型；tenant 来自 Principal | quota:account:self-read |

ACC-01～12 需要用户 JWT、平台身份及对应当前角色权限；平台身份不伪造下游 tenant=0。
ACC-13～19 需要用户 JWT、有效租户、套餐 `ACCELERATOR` 模块、当前角色权限；QUOTA-04
归既有 `TENANT` 模块，不要求 GPU 模块。AK/SK 白名单不增加上述路由。
上述 Governance 授权不能代替 Accelerator 启动配置中的 actor/action/tenant/cluster grant。

HTTP 不接受 tenant/actor/context 作为身份来源；服务从已验证 Principal 分列主体 type/id，
从持久 ResourceTenantID 映射取得 UUID。出站重建 metadata，清除 Bearer 和外部 identity headers。
客户端叶证书要求唯一精确 URI SAN `spiffe://ani.internal/service/ani-governance`，TLS≥1.3，
服务器由受管 CA 与配置的精确 DNS SAN 验证。配置见 `configs/accelerator.env.example`。

写请求保持 `{data:{...}}`；新增字段固定 snake_case；uint64/int64 按 protobuf JSON 为十进制字符串。
列表用 `page_size`（0 默认50，最大200）、`page_token`，响应 `next_page_token`；不伪造 total。
租户 DTO 不含 physical UUID、Pod 身份、原始 evidence、水位、source_fact_ref、完整 plan/runtime。
缺投影是 `USAGE_PROJECTION_NOT_READY`，不表示业务资源不存在；投影/ACK 不表示 Ready 或删除完成。
错误保留受限机器 reason，400/401/403/404/409/412/503/504 分别对应合同失败，原始下游正文不公开。

先执行 `admin sync-apis --dry-run`、复核后执行 `admin sync-apis`，再显式应用
`sql/patches/20260923_accelerator_permissions.sql` 登记精确权限关联。脚本不授予角色、不启用套餐、
不默认分配额度；部署方按明确租户/角色/套餐授权并刷新运行实例策略。接口目录存在不代表已授权。

`SyncGpuUsage` 仅独立恢复 worker 以 Governance 服务身份调用，不受当前用户委托撤销或套餐到期阻断；
`ObserveRelease` 仅 owner 直连，本批没有 Governance 公网代理。正式 registry 无生产 GPU owner，
新建仍失败关闭；预览的 owner_execution_readiness 取受控业务绑定，测试装配不能开启正式目录。

真实 HTTP 权限撤销验收发现历史策略装载未过滤禁用的 role-permission。该批将策略装载限制为
启用角色、启用 ALLOW 关联、启用权限和启用 API；禁用后必须刷新实例策略，旧 JWT 不因此获得
已撤销 API 权限。此修复也作用于共用该授权链的既有路由，需运行 Network/权限回归；初次失败
与修复后复跑分别保留，不修改历史批次结论。

本批隔离软件联调的 20 HTTP BFF、当前权限撤销、真实下游 mTLS、ENDED 后非空绑定脱敏及
非 GPU 回归已通过，详见 [BFF 验收证据](evidence/gov-acc-v12-01/bff/README.md)。这些结果不表示
真实 GPU 调度或生产 owner 已启用；原始失败日志与修复边界一并保留。

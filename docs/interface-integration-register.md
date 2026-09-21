# 功能对接与接口风格改动登记

总体状态：**进行中，未结项**。最后更新：2026-09-21。

本文件覆盖所有后续对接功能。每次用户对接一个新功能时，AI 先排查真实调用链，将涉及的接口、请求和响应、鉴权条件、现有问题追加到对应功能组。先累计登记，等用户指定某个批次后，再统一修改该批接口的路径、字段和响应风格；不能因排查或登记就自动改接口风格。

后续始终维护本文件，不按功能或对话另起登记文件。复用接口引用已有编号，避免重复登记；新功能使用自己的稳定编号。用户明确要求的即时功能修复可单独实施并记录，不必等待风格批次。尚未提供的旧项目格式标为“待指定”；只有用户确认全部改动完成后，整体登记才结项。

## 功能登记索引

| 功能组 | 接口编号 | 排查进度 | 风格改动批次 |
| --- | --- | --- | --- |
| 登录、登出及登录后初始化 | AUTH-01～AUTH-08；相关可选接口 AUTH-09～AUTH-14 | 已排查；已按单独要求去掉登录图形验证码 | 待用户分批，尚未统一改风格 |
| 租户管理 | TENANT-01～TENANT-09 | 已核对 HTTP、Service、Repository 和首次种子；本轮未做运行验收 | 待指定 |
| 租户管理员管理 | 复用 TENANT-06、ACCOUNT-01～ACCOUNT-09、ROLE-01～ROLE-05、AUTH-01/12 | 复用用户和租户角色；用户明确不做主管理员移交 | 待指定 |
| 套餐管理 | PLAN-01～PLAN-14；复用 TENANT-04/08 | 模块限制已有接线；数量配额只配置和统计 | 待指定 |
| 平台运营账号管理 | 复用 ACCOUNT-01～ACCOUNT-09、ROLE-01～ROLE-05、AUTH-02/12；SESSION-01/02 | 支持多个平台账号；运营/只读角色模板待 API 接入后再加 | 待指定 |

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


## 功能组：租户、管理员、套餐与平台运营账号

排查日期：2026-09-21，首次基于 `39008e4` 排查，随后与最新 `main`（`11fa857`，移除文件管理及对象存储）整合。首次排查只登记功能与接口。后续用户授权小范围修复账号停用/删除的会话吊销；不修改密码重置、接口风格或运行数据。上一轮 kind 验收覆盖租户及首管理员创建、平台/租户登录、菜单、套餐读取与登出；本表其余操作的本轮运行验收为 `not_verified`。

### 能力与身份边界

| 功能 | 已有实现 | 当前限制 |
| --- | --- | --- |
| 租户管理 | 列表/详情/查重、创建/编辑/删除；同时创建首管理员；设置套餐、有效期、启停/冻结状态；人数与用量查询；显式清理 Governance 本地数据 | 普通创建不自动建管理员；审核仅为状态字段；删除租户记录与清理数据是不同操作，没有下游资源统一销毁流程 |
| 租户管理员 | 开租户时复制管理员角色模板并绑定首管理员；也可通过用户接口增加本租户管理员、编辑资料/角色/状态、删除账号、重置密码；立即激活或邮件邀请 | `adminUserId` 是租户记录的主管理员指针，用户持有管理员角色是另一件事；用户已明确产品不提供主管理员移交，不再列为待实现项；现有源码未发现最后一个管理员保护 |
| 套餐 | 套餐增删改查；免费/标准/企业版本字段；模块白名单管理；用户数/存储/API 调用量配额配置；到期策略及数据保留天数字段 | 模块白名单已用于 API 和导航控制；没有统一的超配额拒绝逻辑；保留天数未找到自动清理消费方；不是订单/支付系统 |
| 平台运营账号 | 用户增删改查、分配系统角色、自定义角色及权限、状态修改、密码重置、在线会话查询与指定会话下线；默认管理员和本人有删除保护 | 账号 `tenantId=0`，角色类型 `SYSTEM`，平台登录要求至少一个 `platform:` 前缀角色并具有后台访问权限；种子只预置 `platform:admin`，无运营/只读角色模板 |

用户列表为共用接口：平台运营账号列表需明确筛选 `tenant_id=0`；租户管理员列表需筛选目标租户并结合管理员角色，不能把该租户所有用户都当管理员。租户登录上下文的创建操作会强制采用当前租户；平台上下文可指定目标租户。默认租户管理员授权包括用户管理和角色读取，不包括角色增删改及套餐/租户平台管理。

新增自定义平台角色可复用 `ROLE-*` 的 `permissions` 字段关联权限点；权限点里的 `menuIds`、`apiIds` 决定菜单与 API 授权。具体运营权限矩阵尚未指定，不能直接用 `sys:platform_admin` 充当“只读”。

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
| ACCOUNT-06 | `GET /admin/v1/users:exists` | 用户存在性查询 |
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

登录/登出复用 AUTH-01～04，邮件接受邀请复用 AUTH-12。`activationMode=IMMEDIATE`（默认）不需要 SMTP；`EMAIL_INVITATION` 需邮件通道及邀请入口配置，不接受预设密码，账号待激活，邀请有效期 24 小时。当前无邀请重发/撤销管理 HTTP 接口。权限点、菜单和 API 目录的独立管理待对应功能对接时详细登记，本轮未操作其同步接口。

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

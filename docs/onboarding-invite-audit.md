# 开通场景能力审计：平台管理员 / 租户管理员

审计范围：现有代码能否支撑

1. 开通平台管理员，可选「邮件邀请」或「立即激活」
2. 开通租户，可通过邮件邀请对方成为该租户管理员

审计方式：静态源码审计 + 并行多维度调研 + 逐条对抗性验证。**未做运行时链路验证**，所有结论以 `file:line` 为准，落盘时已复核。

审计日期：2026-09-21

---

## 一句话结论

| 分支 | 结论 |
|---|---|
| 场景1 立即激活 | ✅ 已端到端可用 |
| 场景1 邮件邀请 | ❌ 缺失 |
| 场景2 立即激活（建租户+管理员） | ✅ 已端到端可用 |
| 场景2 邮件邀请 | ❌ 缺失 |

缺的不是一张 invitation 表，而是**整条入口链路**：邀请记录、签发/接受/撤销接口、免鉴权入口、未认证自助设密、EMAIL 凭证写入、邮件模板、异步任务类型。

---

## 已具备的能力（可复用）

### 场景1：立即激活平台管理员

- 入口 `POST /admin/v1/users`：`api/protos/admin/service/v1/i_user.proto:33`
- 角色 `platform:admin`：`pkg/constants/role.go:15`；种子定义 `pkg/constants/default_data.go:191-217`，`RoleMetadata.Scope = PLATFORM`（`default_data.go:226`）
- 平台上下文（操作者 `tenant_id=0`）下用户创建强制校验角色为 `type=SYSTEM`：`app/admin/service/internal/service/user_service.go:388-397`
- 凭证直接 `ENABLED`，未传密码时回落默认密码：`user_service.go:427-450`

**限定**：种子角色仅持权限 `{1,2,4,6,7}`（`default_data.go:204`），权限→API 绑定为硬编码 ID（`default_data.go:106-121`），`scripts/model-lab/bootstrap-platform.sql:4-13` 仅补登记 6 条路由。故"可用"指**已登记路由可用**，不等于任意接口可通行。

### 场景2：立即激活（建租户 + 租户管理员）

- 入口 `POST /admin/v1/tenants:with-admin`：`api/protos/admin/service/v1/i_tenant.proto:51`
- 实现 `CreateTenantWithAdminUser`：`app/admin/service/internal/service/tenant_service.go:231-327`，单事务内：
  - code/name 查重 `tenant_service.go:248-258`
  - 建租户行 `:282`
  - 由模板实例化租户管理员角色 `:291`（实现 `app/admin/service/internal/data/role_repo.go:392-421`）
  - 建管理员用户并绑定 `role_id` `tenant_service.go:298-303`
  - 写 `USERNAME/PASSWORD_HASH` 凭证且直接 `ENABLED` `:306-318`
  - 回写 `tenant.admin_user_id` `:321`
  - 成功后 `authorizer.ResetPolicies` `:276`
- 装配：`app/admin/service/cmd/server/wiring_ent.go:173`

**已有租户追加管理员**：平台上下文（`tid=0`）调 `POST /admin/v1/users` 显式传 `tenant_id` + 租户管理员 `role_id`。`user_service.go:370-372` 不会覆写显式 `tenant_id`，`:388-397` 校验 TENANT 类型角色。

### 已定义但未接线（可直接复用的半成品）

| 构件 | 位置 |
|---|---|
| `Membership.INVITED = 3` | `api/protos/identity/service/v1/membership.proto:15` |
| `Membership` repo | `app/admin/service/internal/data/membership_repo.go:384` |
| `User.Status.PENDING = 2`（待激活） | `api/protos/identity/service/v1/user.proto:54` |
| `activate_token_hash` / `expires_at` / `used_at` | `app/admin/service/internal/data/ent/schema/user_credential.go:151-165`（含索引 `:230`） |
| SMTP 发信 | `pkg/mailer/smtp.go:27` |
| 角色模板复制 | `app/admin/service/internal/data/role_repo.go:392-421` |
| asynq 入队 + `MaxRetry` | `app/admin/service/internal/service/task_service.go:93`、`:447` |

### 已接线可用的邮件链路

`mailer.SendMail` 有 3 个真实业务调用点（非孤立工具包）：

- 忘记密码：`app/admin/service/internal/service/authentication_forgot_password.go:56`
- 通知渠道测试发送：`app/admin/service/internal/service/notification_channel_service.go:133`
- 绑定邮箱验证码：`app/admin/service/internal/service/user_profile_contact.go:32`

但三者均为**验证码模式**，无邀请令牌与接受流程。

---

## 坑（按风险排序）

### P0 — 无套餐租户近乎不可用（开通后最可能踩）

1. **全模块 403**：套餐为空时租户闸门直接拒绝
   `app/admin/service/internal/data/tenant_access_checker.go:101-104`
   ```go
   if planId == 0 {
       return adminV1.ErrorForbidden("no subscription plan")
   }
   ```
2. **菜单被清空**：`app/admin/service/internal/service/admin_portal_service.go:219-222`
   ```go
   if err != nil || t == nil || t.PlanId == nil || *t.PlanId == 0 {
       return nil // 无套餐或查询失败 → 清空菜单
   }
   ```
3. **到期扫描跳过**：无套餐关联的到期租户不冻结，且默认策略 `READONLY` 本身也不改状态
   `app/admin/service/internal/data/tenant_usage_repo.go:288-291`、`:305-308`
4. **根因**：建租户不自动绑套餐。`tenant_repo.go` 的 `plan_id` / `subscription_plan` 均为 `SetNillable`，`CreateTenantWithAdminUser` 未设置任何套餐字段。

> 后果：通过 `tenants:with-admin` 开通的租户若未显式绑套餐，管理员登录后菜单为空、业务接口全 403，且永不到期冻结。

### P0 — 忘记密码死锁（邮箱无法成为找回凭证）

5. `ForgotPassword` 按 `IdentityType=EMAIL` 查凭证，查不到时**静默返回成功但不发信**（防枚举）：
   `authentication_forgot_password.go:33-41`
6. 唯一写入 EMAIL 凭证的 `VerifyContact` **要求已登录**（`auth.FromContext`）：
   `user_profile_contact.go:63`
   且写入的是 `dummyPasswordHash` 占位（`:90`、`:104`）。
7. 邮箱**登录**本身可用：`user_repo.go:1166-1184` 反查 USERNAME 凭证。

> 后果：未绑定邮箱的新用户 → 无法找回密码 → 又无法在未认证状态下补绑邮箱。死锁。

### P1 — 免鉴权白名单是硬编码（新增邀请接受接口必踩）

8. 白名单位于 `app/admin/service/internal/server/rest_server.go:87-110`，逐条 `OperationXxx` 硬编码。
   当前仅覆盖：账密登录、平台账密登录、验证码、刷新令牌、MFA 挑战、AK/SK 令牌交换、忘记密码、凭码重置。
9. **无任何 signup / register / activate 类 RPC**（全量 proto 扫描确认）。

> 后果：受邀人无 access token，接受邀请接口若未加入白名单，请求在 auth 中间件即被拦；且"按需加入白名单"会破坏"白名单仅含未认证入口"的现有不变量。

### P1 — 接口登记与权限闸门

10. Api 表未登记的 `(path, method)` → 租户闸门 fail-closed（新接口不登记即不可用）。
11. `SyncApis` 是**清空后全量重建**，非无损增量；已有 ID 与权限关联会被重建，不能当作升级路径。
    实现 `app/admin/service/internal/service/api_service.go:145`，启动自动调用 `:61`。

### P1 — 发信侧可用性前置

12. 发信为**同步调用、失败即阻断**（3 个调用点均直接返回 5xx）。
13. 通知渠道是**平台级、无租户维度**，`configs/` 样例中**无 smtp 段**，`default_data.go` 不播种渠道。
    → 新环境必须人工先建渠道，否则 `GetFirstEnabledEmailChannel` 返回 "email channel is not configured"。
14. 无邮件模板机制，正文全部硬拼字符串（`authentication_forgot_password.go:54-55`）。
15. asynq 已注册任务类型仅 4 个（Backup / TenantExpiryScan / AuditLogArchive / BroadcastMessage）：
    `app/admin/service/internal/server/asynq_server.go:38-66`。**无邮件专用任务类型**。

### P2 — 接口契约与语义

16. `CreateTenantWithAdminUser` 返回 `google.protobuf.Empty`（`tenant_service.go:326`），前端拿不到 `tenant_id` / `admin_user_id`，无法串联后续邀请动作。
17. `CreateTenantWithAdminUser` **不创建** membership 行、组织单元、岗位、套餐绑定。
18. membership 语义默认关闭：`DefaultUserTenantRelationType = UserTenantRelationOneToOne` 是**编译期常量、无配置开关**（`pkg/constants/tenant.go:20`）。
    切换 `OneToMany` 会波及登录/授权分流（`app/admin/service/internal/service/authentication_service.go:501-596`）。
    → 不建议为本场景改动该常量。
19. 建租户时 `username` 不做全局查重，仅在 `(tenant_id, username)` 维度唯一（`tenant_service.go:260-263` + `ent/schema/user.go` 唯一索引）。

---

## 落地清单（若要支持"邮件邀请"）

必须新写：

1. invitation 实体：ent schema + 迁移 + proto 消息
2. invite / accept / revoke / resend 四个 RPC + admin BFF 路由
3. 免鉴权接受入口，并登记进 `rest_server.go:87-110` 白名单
4. 未认证自助注册/设密入口（当前全量 proto 无此类 RPC）
5. 创建 EMAIL 凭证（现有 onboarding 只写 USERNAME）
6. 邮件模板 + 渲染层（替换硬拼字符串）—— 引入 nf 后由 nf 承接，见下文
7. 邮件专用 asynq 任务类型 —— 引入 nf 后由 nf 承接，见下文

配套必做：

8. 新接口的 Api 登记 `(path, method)` + 权限点绑定（否则 fail-closed）
9. 建租户时显式绑套餐，或修正 P0 三条 403/空菜单/不冻结逻辑
10. 通知渠道播种或改为配置化，避免新环境发信不可用
11. `CreateTenantWithAdminUser` 返回租户与管理员 ID，供前端串联

---

## 补充：引入 ani-notification-service 的可行性评估

外部仓库：`/home/ubuntu/Workspace/ani-notification-service`（下称 nf）。
邮件契约唯一入口：`api/notification/v1/notification_service.proto`（无 HTTP 绑定，仅内部 gRPC）。

### 契约自带的职责边界（NOTIFY-0.md §5）

| 归 nf | 归 IAM（本项目承担） |
|---|---|
| 模板、locale 回退、投递策略（`NOTIFY-0.md:111`） | Invitation、token 有效性、接受、重发版本（`:109-110`） |
| Notification/Delivery/Attempt 状态（`:112`） | 租户生命周期、Principal/Membership/Role（`:106-107`） |
| SMTP 凭据与重试（`:113`） | 已验证邮箱归属（`:108`） |
| — | "是否需要发邀请"这一决定（`:110`） |

原文明示：*"Notification does not become a second IAM database."*
注意：契约里的 "IAM" 指已废弃的 ani-iam 项目，其职责由本仓承接。

### 引入后消掉的坑

一次性解决 P1-12/13/14/15：

- 同步发信阻断 → 改为持久受理；Provider 失败走 Delivery 状态，不回灌为 RPC 错误（`NOTIFY-0.md:248-249`）
- 无模板 → 模板归 nf（`template_revision`）
- 无 smtp 配置段 / 渠道需人工播种 → 凭据归 nf
- 无邮件 asynq 任务类型 → 重试归 nf
- 附带 `GetSubmissionStatus` 状态回查（`masked_destination`、`attempt_count`、`DeliveryReason`）

现有 `pkg/mailer/smtp.go` 直发 + `notification_channel` 存 SMTP 账号，长期应退场；
风险最小的切换是先替换忘记密码那条硬拼模板路径。

### 引入后仍然存在的坑

落地清单第 1-5 项不变（invitation 实体、令牌、接受/撤销/重发接口、免鉴权白名单、EMAIL 凭证）。

**P0-2 忘记密码死锁不消失**：卡点是 governance 侧没有 EMAIL 凭证，换通道依旧不发。
根因在 `user_profile_contact.go:63` 要求已登录。

### 场景差异

- **租户管理员邀请**：命中契约。`IAM_TENANT_INVITATION` 即为此设计，且明确允许 IAM workload 用
  `DirectRecipient` + `EmailDestinationSnapshot` 直传邮箱（`NOTIFY-0.md:158-163`），
  因为受邀人此时尚无 Principal。
- **平台管理员邀请**：契约无对应类型，仅 `IAM_TENANT_INVITATION` 与 `IAM_PASSWORD_ACTION`
  两种（`notification_service.proto:158-162`）。可行替代：建 `User.Status.PENDING` 用户 +
  发 `IamPasswordAction{purpose: SETUP, audience: BOSS}`。NOTIFY-0 已冻结，新增类型是新决策。

### 契约侧硬约束

1. `request_id` 为 UUID；幂等键 `(producer_id, request_id)`（migrations `UNIQUE (producer_id, request_id)`）
   → governance 必须持久化 request_id。
2. 重发 = 同一 invitation ID + **更高 version** + **新 request_id**（`NOTIFY-0.md:222-224`）。
3. ID 形态不一致：nf 要 UUID（tenant / `source.resource_id` / request_id），本仓为 uint32。
   桥接点已存在：`tenant.resource_tenant_id`（uuid / immutable / unique，`ent/schema/tenant.go:36`）。
   invitation 需自建 UUID + version。
4. `deliver_before <= invitation_expires_at`，否则 `REQUEST_INVALID`（`NOTIFY-0.md:238`）。
5. `action_url` 必须 HTTPS 且命中精确 origin 白名单，否则 `ACTION_URL_NOT_ALLOWED`
   （`NOTIFY-0.md:239`、`:572-573`；配置项 `allowed_action_origins`，`conf.proto:53`）。

---

## 补充：nf 鉴权体系与本仓不兼容（需改 nf，不改本仓）

### 两套模型对照

| | 本仓（对 ani-network-service 的实际做法） | nf 期望 |
|---|---|---|
| 身份载体 | mTLS 客户端证书，校验 `leaf.DNSNames` 含精确 `ani-governance`（`internal/data/network_client.go:53-61`），服务端 SAN 固定 `ani-network-service`（`:64`） | 在线回调身份服务 `VerifyCaller(ctx, target)`，比对 `caller.PrincipalID()`（`internal/data/iam_producer.go:42-48`） |
| 关键概念 | 无 | `principal_id` / `trust_domain` / `environment` / `iam_server_name`（`internal/conf/v1/conf.proto:61-71`） |
| 校验时机 | 握手期本地证书校验 | 每次 RPC 回调 IAM |

不同构，但 nf 的 seam 干净：业务层只认 `TrustedProducerIdentityFromContext`
（`internal/server/identity.go:40`），resolver 接口仅一个方法
（`internal/server/workload_identity.go:12-14`）。换 resolver 不涉及业务层、proto、能力矩阵。

### 两个阻塞点

**A. 唯一的生产 resolver 硬绑废弃 IAM**
`ResolveWorkload` 全仓仅一个实现：`internal/data/iam_producer.go:37`。
其 import `github.com/zhangzhe-ctrl/ani-iam/sdk/grpcworkload`（`:5`），
`cmd/ani-notification-service/workload.go:31` 必须建立 IAM 连接，
且 `NewIAMProducerResolver` 硬编码 `producerID != "ani-iam"` → `InvalidArgument`（`:20`）。
IAM 已废弃 ⇒ workload runtime 构造即失败；而 `trusted_identity_resolver` 是 hard readiness gate
（`cmd/.../app.go:31`、`workload.go:55`）。

**B. 该 resolver 未授予租户邀请能力（更关键）**
`internal/data/iam_producer.go:23-31` 仅 grant：

```go
NewSubmitGrant(NotificationTypeIAMPasswordAction,
    NewAllOfScopeKindSelector(ScopeKindHumanPrincipal),
    RecipientKindHumanPrincipal, DestinationKindEmail)
```

而能力矩阵写死 `IAM_TENANT_INVITATION` 必须 `ScopeKindTenant` + `RecipientKindDirect`
（`internal/biz/model.go:284-286`）。

→ **即便 IAM 复活，该 resolver 也提交不了租户邀请。**
仓库中唯一构造出邀请 grant 的位置是 `internal/biz/capability_test.go:37-48`，属测试内手工构造，非生产装配。

结论：**租户管理员邀请在 nf 当前代码中未被授权给任何调用方。**

### 建议的 nf 改动面

1. 新增 `internal/data/mtls_producer.go`：实现 `WorkloadProducerResolver`，
   从 `peer.FromContext` 的 TLSInfo 取对端证书 SAN，比对配置白名单（`ani-governance`），
   返回 `biz.NewProducerIdentity(...)`。
2. grant 必须显式给全（形状见 `capability_test.go:37-45`）：
   `NewSubmitGrant(NotificationTypeIAMTenantInvitation, NewAllOfScopeKindSelector(ScopeKindTenant), RecipientKindDirect, DestinationKindEmail)`
   + `NewGetOwnGrant()`
3. `conf.proto` 的 `WorkloadIdentity` 增 mode / allowed_sans / producer_id（`:61-71`）
4. `cmd/.../workload.go` 按 mode 选 resolver，不再强制 `NewWorkloadOnlyClient`
5. `cmd/.../app.go:42-54` 装配分支；readiness `trusted_identity_resolver` 语义由
   "IAM `client.Check()` 存活"改为"resolver 已装配且 mTLS 配置有效"（原实现 `workload.go:54-55`）
6. 在 `allowed_action_origins` 登记本仓邀请链接 origin

### 改动时必须守住的不变量

- **不要绕开能力矩阵**（`biz/model.go:279-299`）。grant 本身就是白名单。
- **身份只能取自 transport**，不得读 header / metadata / 请求字段
  （ADR 0004:20-21、`internal/server/identity.go:13-15`）。
- **mTLS 只证明"对端是 ani-governance"，不证明"本次提交属于该租户"**。
  契约亦声明 `tenant_id` 是业务域而非鉴权（`NOTIFY-0.md:180-181`）。
  采用 `AllOfKind(Tenant)` 意味着本仓可对任意租户发邀请，租户归属校验仍留在本仓隔离逻辑内
  （见 AGENTS.md 实现规则第 9 条）。
- **幂等命名空间天然隔离**：producer_id 由 `ani-iam` 变为 `ani-governance` 后，
  与历史数据在 `UNIQUE (producer_id, request_id)` 下互不影响。
  但须主动用 SAN 白名单收口，避免退化为"任何持证者均可提交"。

### 本仓侧改动（鉴权体系不动）

- 新增 notification client：照抄 `internal/data/network_client.go` 的 fail-closed mTLS 模式
  + `NetworkConfigFromEnv()` + 精确 DNS SAN 校验，替换 `networkv1` 目标
- `go.mod` 加 `ani-notification-service` 固定版本（当前仅有 `ani-network-service`，`go.mod:207`），
  遵守 AGENTS.md 的 GOPROXY 约束
- 建议配 outbox + 既有 asynq 重试，避免投递失败回滚业务事务（`SubmitNotification` 为同步 gRPC）

### 暂缓项

`IAM_PASSWORD_ACTION` 要求 `ScopeKindHumanPrincipal` + `RecipientKindHumanPrincipal`，
即必须传 principal UUID；本仓用户 ID 为 uint32。需额外引入 principal UUID 映射，
不建议与本场景捆绑实施。

---

## 未验证项（not_verified）

- 所有 SMTP 实际投递、`GetFirstEnabledEmailChannel` 真实返回
- 白名单新增后的鉴权链路端到端
- 无套餐租户登录后菜单/403 的实际表现
- 邀请接受后的授权链（`authorizer.ResetPolicies`）行为
- nf 代码未编译、未运行测试；mTLS resolver 尚未实现
- nf 现有证书由 `grpcworkload.TLSFiles.ServerTLSConfig()` 生成，其对端 SAN 形态（DNS vs URI）未确认，落地前需核对
- 本仓邀请链接 origin 未定，`allowed_action_origins` 待登记
- ADR 0004 的恢复门槛（`NOTIFY-0-AUTH-INTEGRATION`）尚未通过；L3（真实 IAM Dispatcher + 平台 workload identity）为 `not_verified`

以上均为静态代码推断，需真实环境验收后才能确认。

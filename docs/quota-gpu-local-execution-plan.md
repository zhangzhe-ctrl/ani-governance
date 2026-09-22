# 租户套餐配额与 GPU 模拟闭环：本地执行计划

日期：2026-09-22。批次：QUOTA-GPU-LOCAL-01。

状态：**仅计划，尚未实现、尚未执行本计划的验收。**

业务源码核对基线：5a5d0d80e1cf350f250dc65cbb34ae9bc2b65ab9。

本文件供执行 AI 直接使用。文中的“必须”“禁止”“仅允许”是执行约束，不是可选建议。未经用户后续明确修改，不得自行替换本文件确定的数据口径、接口、协议或验收范围。

**这批次所有的工作都可以在本地完成。** 包括代码修改、依赖获取、代码生成、编译、单元测试、真实 PostgreSQL 测试、本地容器、独立进程模拟测试、进程重启和故障注入。无需 SSH、远程构建机、真实 GPU、Kubernetes、真实 GPU 资源服务或云账号。先前其他批次的“仅远程构建/测试”要求不适用于本批。

长期接口与问题状态继续维护在 [接口登记文件](interface-integration-register.md)。本文件是执行计划，不是第二份接口登记表。登记中的 QUOTA-* 为计划接口，执行通过前不得标成已实现。

## 0. 可直接复制给执行 AI 的任务指令

> 在 ani-governance 仓库执行 docs/quota-gpu-local-execution-plan.md 的 QUOTA-GPU-LOCAL-01 批次。先完整阅读本文、仓库 AGENTS.md 和本文列出的源码，再严格按 P0～P8 实施。所有工作均在本地任务隔离环境完成。实现可复用的套餐配额目录和单次占额/退额账本；Governance 是统一入口，在转发前占额，资源服务只上报可退额事实。GPU 用独立、持久化的模拟资源服务验证，不得伪造真实 GPU 接入成果。不得新增预占转实扣阶段，不得自动超时退额，不得绕过真实 PostgreSQL、现有用户认证、套餐和权限检查。不得改动其他仓库、推送、部署或做无关重构。每阶段交付可核验文件和测试证据；缺少必需条件应记录 fail 或 not_verified，不能降低门槛。完成后给出逐项验收表、改动清单、复现命令、残留问题和清理结果。

本文当前落盘只完成“制定计划”，不表示上述执行任务已开始。

## 1. 用户已确定的行为

1. 管理员能够创建套餐，从系统已经支持的配额项中选择项目并设置数量。
2. 套餐绑定到租户；有效期保存在租户绑定侧，沿用现有 tenant.plan_id / tenant.expired_at。
3. Governance 是所有本批用户资源请求的统一入口，兼任转发网关。
4. **Governance 转发前只占额一次。** 创建成功不再实扣第二次。
5. Governance 配额模块不接收或维护资源的创建中、运行中、调度中、健康状态。
6. 资源服务仅报告“对应数量已不再占用且不会迟到创建”，Governance 据此退额。
7. 操作成功结束不等于资源释放；失败也不自动证明没有资源。
8. 真实 GPU 资源服务尚未准备好。本批只使用本地持久化模拟器，不到其他仓库补写一个正式 GPU 服务。

固定主流程：

~~~text
用户 → Governance 认证/授权/套餐检查
     → 一个 PostgreSQL 事务：保存操作和转发意图 + 原子占额
     → 携带稳定 operation_id / charge_id 转发给模拟资源服务
     → 模拟器持久化接受并执行；成功不再向额度账本报成功

模拟器确认资源释放/创建已封闭且清理完成
     → 内部 mTLS 退额 RPC
     → Governance 幂等归还对应额度
~~~

## 2. 本批交付范围与排除项

### 2.1 必须交付

- 配额目录；已有套餐配额 CRUD 使用稳定 quota_code，保留本批明确的旧字段兼容。
- 租户配额账户、占额明细、持久化转发操作、退额回执。
- 通用单次占额、累计释放、幂等转发和重启恢复。
- 独立内部 mTLS 退额服务；不得复用公网免登录路径。
- 一个只在 quota_lab 构建中存在的 GPU 用户入口和下游适配器。
- 独立进程的 GPU 模拟资源服务及独立持久化的 GPU 分配事实。
- 本地真实 PostgreSQL 迁移、并发、隔离、故障、恢复和退额验收。
- 生成物、接口登记、部署说明增量、可复现脚本和脱敏证据。

### 2.2 明确不做

- 不接入真实 GPU、CUDA、NVML、MIG、HAMi、Kubernetes、真实调度器。
- 不实现 GPU 型号、地域、资源池、共享显存、利用率或 GPU 小时计费。
- 不实现订单、支付、优惠、自动续费、套餐历史计费或账单。
- 不实现按月 API/Token 累计额度和周期重置。
- 不接入 USER_LIMIT / STORAGE / API_CALL 的强制拦截；保持其现有配置/统计边界。
- 不调整 Plan.version、模块枚举、整个项目的接口风格、认证体系、角色模型。
- 不改写登录、自助资料、密码、通知、Model 或 Network 的业务行为。
- 不增加资源 Running/Failed 状态同步、MQ 集群、分布式事务框架或通用工作流引擎。
- 不把本批实验 GPU 路由包装为正式 GPU API。
- 不创建前端项目；只交付后端合同和测试。
- 不自动提交、推送、发布、部署；用户之后明确要求时另行执行。

### 2.3 结果边界

成功交付只能写：

> 通用配额模块及本地持久 GPU 模拟闭环通过指定验收；真实 GPU 服务接入和真实硬件分配 not_verified。

禁止写“真实 GPU 创建释放通过”“GPU 生产可用”“所有配额都已强制生效”。正常生产构建本批没有 GPU 创建路由，套餐中出现 gpu.count 也不代表真实 GPU 能力已启用。

## 3. 必须阅读的源码与事实

| 入口 | 执行前必须确认的事实 |
| --- | --- |
| [套餐配额 Proto](../api/protos/identity/service/v1/plan_quota.proto) | quota_type 是 tag 3 的枚举，quota_value 是 tag 4 |
| [租户 Proto](../api/protos/identity/service/v1/tenant.proto) | QuotaUsage.quota_type 是 tag 1，quota_value 是 tag 2 |
| [PlanQuota schema](../app/admin/service/internal/data/ent/schema/plan_quota.go) | 当前只有类型/数量，plan 边关联；不存在新账本 |
| [PlanQuotaRepo](../app/admin/service/internal/data/plan_quota_repo.go) | 枚举转换、CRUD、关联及 FieldMask 的现状 |
| [TenantUsageRepo](../app/admin/service/internal/data/tenant_usage_repo.go) | 当前 storageUsedBytes 固定为 0；不能作为外部存储真实用量 |
| [租户访问检查](../app/admin/service/internal/data/tenant_access_checker.go) | API/模块白名单 fail-closed，到期策略还存在自己的闸门 |
| [NetworkService](../app/admin/service/internal/service/network_service.go) | 可信身份 → resource tenant UUID → 下游；目前这里只是 GetVPC |
| [NetworkClient](../app/admin/service/internal/data/network_client.go) | 出站 mTLS；每次新 request-id 不等于业务幂等标识 |
| [REST 装配](../app/admin/service/internal/server/rest_server.go) | 用户认证、租户闸门、Casbin、API 目录注册方式 |
| [手写 wiring](../app/admin/service/cmd/server/wiring_ent.go) | 构造层次、cleanup、现有依赖 |
| [进程入口](../app/admin/service/cmd/server/main.go) | 目前只有 REST/Asynq/SSE，没有现成内部 mTLS gRPC listener |
| [schema 导出器](../app/admin/service/cmd/schema/main.go) | 保留已有 AK 同租户角色复合外键 |
| [部署说明](deployment.md) | Atlas、初始化、API 同步、内存策略刷新顺序 |
| [Atlas 配置](../app/admin/service/atlas.hcl) | schema.sql 是声明式来源；不得改为直接 ent:// |

不得把 operation audit log、日志 request-id 或下游返回的 last_operation_id 当成已有持久化幂等账本。

## 4. 本地环境、权限与工具约束

### 4.1 工作区

- 仓库为 /home/chabking/workspace/ani-governance。
- P0 保存 git status、HEAD、已有 diff 和工具版本到任务证据目录。
- 不覆盖其他改动，不运行 git reset --hard、git clean -fd、全仓 checkout、自动 stash。
- 若需要分支，使用 codex/quota-gpu-local；同名已存在时先核对归属，不强制覆盖。
- 本文基线是源码核对点，不授权回退当前分支。若执行时业务源码已有变化，先核对差异并更新基线说明；发现与本文合同冲突则报告阻塞，不自由改协议。

### 4.2 本地运行隔离

- 宿主机发布端口和本地业务进程只绑定 127.0.0.1。PostgreSQL/Redis 容器内部允许监听自己的隔离网络地址，以供本机端口映射连接；禁止 host 网络和向宿主机外网发布。
- Docker 只能连接本机 unix socket；发现 DOCKER_HOST 或当前 context 指向远程，立即停止容器阶段，不修改用户 context。
- 使用本任务唯一容器名称、目录和端口清单。固定端口冲突时由脚本选取空闲本地端口并写 manifest，不结束占用端口的未知进程。
- PostgreSQL 使用 16 系列、Redis 使用 7 系列。P0 解析可用镜像为 digest，写入 versions.lock；后续命令只用已锁 digest，不继续跟随浮动 tag。
- 允许本地下载固定工具/依赖；不执行远程计算，不访问真实业务数据库或集群。
- 新建四类隔离数据库：Governance、GPU owner、模拟 provider、Atlas dev；迁移升级/恢复测试另建库。
- owner/provider 运行账号不能读写 Governance 业务表；Governance 运行账号不能读写 owner/provider 表。
- 运行账号无 DDL 权限。schema/data 初始化使用独立迁移账号，完成后服务只使用运行账号。
- 若使用 Redis，必须是本任务独立容器；不使用现有共享实例。
- 机密文件权限 0600，不提交私钥、JWT、密码、完整 DSN。日志/证据必须脱敏。

### 4.3 固定生成基线

| 工具 | 版本 |
| --- | --- |
| Go | 1.26.7 |
| gow | v1.0.3 |
| Buf | 1.60.0 |
| Ent generator | v0.14.6 |
| Atlas CLI | v1.3.0；记录二进制校验值 |
| protoc-gen-go | v1.36.11，沿用现有定向切片 |
| protoc-gen-go-grpc | v1.6.0 |
| protoc-gen-go-http | v2.0.0-20251205160234-b9fab9a5a5ab |
| protoc-gen-validate | v1.3.3 |
| protoc-gen-openapi | v0.7.1 |

- 不执行含 @latest 的 make plugin / make cli。
- 不运行 gow wire、generate-model-slice.sh。
- 不盲跑 scripts/post-generate-clean.sh，它可能 checkout 覆盖本批生成改动。
- 新增 scripts/generate-quota-slice.sh、api/buf.quota.gen.yaml、api/buf.quota-lab.gen.yaml；模板 clean:false，固定输入清单和版本。
- 生产配额与实验 GPU Proto 分别定向生成；实验路由不得进入正式 OpenAPI。
- Ent 首选 gow ent admin；定向脚本可使用同等固定 Ent 命令，必须包含 privacy、entql、sql/modifier、sql/upsert、sql/lock。
- 随后执行 schema 导出器和 make openapi。不要手改生成 Go 文件。
- 不升级 go.mod/go.sum 既有业务依赖；工具获取产生的必要变动单独审查，禁止顺带 go get -u / 广泛 tidy。

## 5. 固定配额口径与配置合同

### 5.1 目录

目录是平台配置元数据，没有 tenant_id；套餐也仍是当前共享模板。租户余额和操作记录必须有 tenant_id。

本批只登记下列四项：

| quota_code | 单位 | 计数模型 | 旧枚举 | 本批执行能力 |
| --- | --- | --- | --- | --- |
| user.count | user | CONCURRENT | USER_LIMIT | LEGACY_CONFIG_ONLY |
| storage.bytes | byte | CONCURRENT | STORAGE | LEGACY_CONFIG_ONLY |
| api.calls | request | COUNTER | API_CALL | LEGACY_CONFIG_ONLY |
| gpu.count | gpu | CONCURRENT | 无 | LAB_ONLY |

- GPU 单位是“整张卡的占用承诺”，不是利用率，也不是已经 Running 的卡数。
- 创建中、结果未知及已分配都占相同额度。查询应显示“已占额度”，不能命名为“真实运行 GPU 数”。
- 本批目录仅提供读取，不开放任意创建/修改 code、单位或执行能力的公共 CRUD。
- 目录新增使用显式版本数据脚本；执行能力由已注册适配器决定，不能由前端提交 supported=true 获得。
- 正式构建对 gpu.count 返回 LAB_ONLY / 无生产执行器，不能谎报 production enforcement enabled。
- 后续新增类似并发资源项目只需新目录项目和业务适配器，不新增枚举或资源专用余额表。

### 5.2 旧字段兼容：本批固定为增量兼容

- PlanQuota 新增 string quota_code，固定 tag 5，JSON 名 quotaCode。
- QuotaUsage 新增 string quota_code，固定 tag 3，JSON 名 quotaCode。
- 原 quota_type 字段及 enum 数字值保留并标 deprecated；不得改 tag 的类型、重编号或新增 GPU 枚举。
- DB 新增 quota_code；旧 quota_type 暂保留 nullable，只有兼容映射器读写它。权威业务字段改为 quota_code。
- 旧请求只有 quotaType 时，按上表转换；同时给出 code/type 时必须一致，否则 400。
- 新 GPU 请求只传 quotaCode；读取时旧 enum 返回 UNSPECIFIED，绝不能冒充 USER_LIMIT。
- 老三项仍输出一致的旧 enum，保持旧客户端读取能力。
- 新前端使用 quotaCode。禁止在 Service/Repo 多处各写一套映射 switch；集中到一个映射文件。
- 本批不删旧字段、不宣称旧字段已移除。未来删除必须另开明确兼容批次。

### 5.3 套餐设置

- 复用现有 PLAN-11～14，不创建第二套套餐 API。
- 每个套餐同一 quota_code 只能有一行；plan_id、quota_code、quota_value 必填。
- quota_value 为整数，范围 0～9223372036854775807；0 明确表示不允许新增。
- 缺少配额项表示不允许申请该项，不能解释为无限制。
- 修改套餐限额立即用于后续新占额。账户不缓存另一个可漂移的 effective_limit。
- 允许将上限降低到当前占用以下；available=max(limit-occupied,0)，拒绝新增，不自动驱逐或退额。
- 套餐切换沿用租户绑定接口；余额属于租户，不属于套餐，切换套餐不得清零。
- 同一套餐被多个租户引用时，额度是每租户独立上限，不是租户间共享一个余额。
- Update 必须非空 updateMask；允许 quotaValue、quotaCode，但不允许改 planId；改 code 等于替换该套餐的一个政策项，按同样唯一约束与政策锁执行。
- 不允许通过 CRUD 指定任意主键、allowMissing 隐式创建或清空必填字段。
- 变更 code/type 的 mask 兼容处理集中实现；两个字段同时出现仍须映射一致。

示例：

~~~json
{
  "data": {
    "planId": 12,
    "quotaCode": "gpu.count",
    "quotaValue": "8"
  }
}
~~~

沿用 protobuf JSON：uint32 ID 为数字，uint64 数量为十进制字符串；拒绝负数、小数和越界。FieldMask 值仍为 lowerCamel 字符串。

## 6. 数据模型与数据库约束

表名和最少字段固定如下，允许增加时间戳、错误摘要和索引，不得改变职责或另建一套平行账本。

| 表 | 字段与约束 |
| --- | --- |
| sys_quota_definitions | code 唯一主键、display_name、unit、accounting_kind；code/unit/kind 本批不可经 API 修改 |
| sys_plan_quotas 增量 | quota_code 非空 FK、plan_id 非空 FK、quota_value 非空；UNIQUE(plan_id,quota_code)；旧 quota_type 兼容保留 |
| sys_quota_accounts | id、tenant_id、quota_code、occupied_units、version、created_at、updated_at；UNIQUE(tenant_id,quota_code)；occupied_units>=0 |
| sys_quota_operations | operation_id UUID、tenant_id、resource_tenant_id UUID、resource_id UUID、create_operation_id nullable UUID、actor_type、actor_id、owner_service、action、idempotency_key、request_hash、canonical_request、dispatch_state、attempt_count、lease_generation、retry_blocked、last_error_code、next_attempt_at、lease_owner、lease_until、ack_json、created_at、updated_at |
| sys_quota_charges | charge_id UUID、tenant_id、operation_id、quota_code、original_units、released_units、created_at、updated_at；UNIQUE(tenant_id,operation_id,quota_code)；0<=released_units<=original_units |
| sys_quota_release_receipts | receipt_id UUID、tenant_id、owner_service、release_event_id UUID、payload_hash、payload_json、created_at；UNIQUE(owner_service,release_event_id) |

约束细节：

1. operation 幂等唯一键为 (tenant_id,actor_type,actor_id,action,idempotency_key)。同租户不同用户也不能读取彼此的操作响应，除非另有现有管理权限。
2. action 使用字符串和 adapter 注册表，本批只注册 LAB_GPU_CREATE、LAB_GPU_DELETE，禁止将这两个实验值写成数据库/Proto 的通用账本枚举。CREATE 的 create_operation_id 为空；DELETE 必须持久保存原 create_operation_id，以 (tenant_id,create_operation_id) 复合 FK 关联原创建操作，并从该操作获取原 charge。DELETE 不新建 charge、不新增占额。两种操作都持久保存同一 resource_id，重启后不能依赖内存关联。
3. 每个 operation 的请求、owner、租户、actor、配额向量一旦提交不可改写。
4. canonical_request 保存经校验的业务参数；不得保存 Bearer、密码、Cookie、签名头或私钥。
5. canonical_request 显式 schema_version=1；幂等哈希包含可信 tenant/actor/action/owner 与校验后的业务参数，不包含每次生成的 request-id、时间戳或新 UUID。创建的 resource_id 先生成一次并随原操作持久化，重试读取原值；不得因重试重新生成 ID 导致异报文冲突。固定字段顺序序列化，禁止直接对未规范化的用户 JSON/map 求哈希。
6. 所有外部请求 ID 和内部 ID 区分：日志 request-id 每次可不同；operation_id、charge_id、idempotency_key 重试保持不变。
7. 配额数量字段采用 PostgreSQL bigint / Go int64 安全范围；不用浮点数，不允许溢出截断。
8. accounts、operations、charges、receipts 均按 tenant_id 查询；禁止无租户 GetByID 后原样返回。
9. operations 增加 UNIQUE(tenant_id,operation_id)，charges 增加 UNIQUE(tenant_id,charge_id)。
10. charges 通过 (tenant_id,operation_id) 复合 FK 引用 operation，通过 (tenant_id,quota_code) 引用 account；不只做单列 FK。
11. 本批固定 receipt.payload_json 保存整个已验证批次，不另外拆明细表；receipt.tenant_id FK 引用 tenant。每次 RPC 的 items 必须属于同一 tenant、owner 和原创建 operation，不能混批跨租户。
12. account/operation 到 tenant 的 FK 使用 RESTRICT。本批存在配额账户或历史操作的租户不得物理删除，返回 409 QUOTA_HISTORY_PRESENT；只在 Tenant.Delete 增加明确保护，不顺带实现资源退场或删除历史。未产生新配额记录的租户保留原行为。
13. plan 删除必须保护绑定租户；不得通过删除套餐级联删除租户账本。只补与配额安全直接相关的保护并登记。
14. 不使用 RLS 代替显式租户过滤；复合约束由 Ent schema 与 schema 导出器共同保留。
15. operation 同时就是持久化转发意图；本批不再建立第二个同义 dispatch/outbox 表。
16. 不维护 reserved_units / allocated_units 两套额度，不新增 ConfirmQuota 接口。
17. 对原始创建记录建立 (tenant_id,owner_service,resource_id) 的条件唯一索引，条件为 create_operation_id IS NULL，不把 LAB_GPU_CREATE 写入索引条件。DELETE 通过原创建操作定位同一资源。不能因为删除 action 不同就把另一租户的 resource_id 关联进来。

核心恒等式，每轮验收结束都必须通过 SQL 重算：

~~~text
account.occupied_units
  = SUM(charge.original_units - charge.released_units)
    WHERE tenant_id 和 quota_code 相同

available_units = MAX(effective_plan_limit - occupied_units, 0)
~~~

effective_plan_limit 在读取/新占额事务中从当前 tenant.plan_id 对应政策读取；没有该项时按 0 展示并返回配置缺失原因。

## 7. 锁顺序与一次占额事务

### 7.1 固定锁顺序

- 新占额：tenant 行 FOR UPDATE → 当前 plan 行 FOR SHARE → 同 operation/幂等记录 → 按 quota_code 排序的 account → charge。
- 退额：根据不可变 charge 找到 tenant 后，tenant 行 FOR UPDATE → operation → 按 quota_code 排序的 account → charge → receipt。
- 套餐配额变更：plan 行 FOR UPDATE → 该 plan 的 quota 行；不得反向再锁 tenant/account。
- 租户换套餐：tenant 行 FOR UPDATE → 目标 plan 行 FOR SHARE → 更新绑定。
- tenant 删除保护在持有 tenant 锁后检查新账本。各路径不得先锁 account 再锁 tenant/operation。
- worker 领取只锁 operation，不在持有 operation 锁时再进入占额/退额事务；先提交领取再做网络调用。

这样政策写入与占额读取相互串行化，禁止用异步刷新或进程内 mutex 假装数据库并发安全。所有路径保持短事务，网络 RPC 不能放在数据库事务里。

### 7.2 占额算法

1. HTTP 入口先做真实用户认证、Casbin、租户/套餐模块和报文校验。
2. 从可信 Principal 获取 tenant/actor，解析持久 resource_tenant_id；不采信客户端 tenantId、actor、chargeId。
3. 校验必需 Idempotency-Key 为 UUID；构造规范请求和配额向量。
4. 开事务，按上述顺序加锁。
5. 优先查同幂等记录：内容一致则返回既有 operation；不同则 409 IDEMPOTENCY_CONFLICT。不得再次扣额。
6. 对新操作再次检查租户 ON、plan 存在、expired_at 为 nil 或数据库当前时间严格小于到期时间。到期时间相等视为到期。
7. 本批新占额无论 READONLY/BLOCK_LOGIN/FREEZE 哪种策略，到期一律拒绝；不等待小时定时任务。
8. 对每个配额项检查目录、适配器支持及政策；本次 GPU 请求数量 1～16。
9. 多项额度必须全部满足才提交；一项失败，operation/charge/account 变更全部回滚。
10. 创建或读取 account，用安全算术检查 occupied+requested<=limit。
11. 同事务保存 operation(QUEUED)、各 charge 和 occupied 增量。
12. 提交成功后才能通知 worker 转发；提交失败禁止下游调用。
13. HTTP 返回 202 与稳定 operation_id。HTTP 202 只表示治理侧持久接受，不表示 GPU 创建完成。

已接受操作的恢复遵循接受时的授权决定；套餐随后到期/降额不撤销此前已接受操作，不再次扣额。新 HTTP 请求仍须通过当前身份权限；后台恢复不保存或依赖旧用户 token。

## 8. 持久化转发与恢复

### 8.1 Governance 只记录投递状态

允许状态固定为：

| 状态 | 含义 |
| --- | --- |
| QUEUED | 事务已提交，尚未尝试发送 |
| DISPATCHING | 发送前已持久标记，可能已到达下游 |
| UNKNOWN | 发送结果不确定，仍占额 |
| ACKED | 下游已持久接受命令，不代表创建成功 |
| CANCELED_UNSENT | 明确从未尝试发送，已封闭后续转发并本地退额 |

不增加 PROVISIONING/RUNNING/GPU_FAILED 等资源状态。完全退额由 charge 的 released_units 判断，不改变资源状态。

### 8.2 worker 行为

- worker 使用 PostgreSQL 领取；同进程和多进程都通过行锁/租约协调。每次领取递增 lease_generation；完成/失败回写必须匹配领取时的 generation 和允许的前态，旧 worker 不得将 ACKED/CANCELED_UNSENT 回退为 UNKNOWN。
- 先提交 DISPATCHING、attempt_count+1，再发 RPC。网络请求超时后不可回到“从未发送”。
- 租约固定 15 秒，RPC 超时 3 秒，退避 1/2/4/8/16/30 秒，最多 30 秒；恢复仍用同一操作 ID 和报文。
- worker 租约到期只允许接管投递，不退额。
- 成功持久接受后标 ACKED，停发该命令。收到回执前退额事件可能已到达，仍合法。
- 响应丢失/连接超时/进程退出：保持占额，重启后同 ID 重投。禁止换 ID“再试一次”。
- 下游明确拒绝业务执行也不能由 Governance 凭错误字符串退额；已转发操作须由 owner 发可退额回执。
- 永久合同错误不得无限高频重试：持久保存 UNKNOWN、retry_blocked=true 和 last_error_code，暂停该操作自动重试并显式报警/列入证据；不得退款掩盖问题。
- 解除暂停只能通过内部 ResumeDispatch 方法清除 retry_blocked 并保留原 operation/charge/request_hash；本批仅允许 lab 控制钩子调用，记录审计，不开放公共控制接口，不允许改账本余额。恢复后仍由正常 worker 重试原命令。
- 不做通用 URL/方法自由转发。owner/action 必须命中编译注册的 adapter；用户不能指定 URL 或服务名称。
- 下游适配器缺失或配置不全时，新操作在占额前拒绝；若恢复遇到缺失 adapter，保留账本并报错。
- 构造函数不查询数据库、不启动 goroutine；worker 在应用 Start 后运行，在 Stop 时停止领取并等待有限时间退出。

### 8.3 从未发送的本地撤销

只允许专用内部方法在 operation=QUEUED 且 attempt_count=0 时撤销。先用不可变归属定位 tenant，再按 tenant → operation → accounts → charges 的顺序开启并持有锁；禁止持有 operation 锁后另起公共退额事务。在同一事务内：

1. 锁定 operation，标记 CANCELED_UNSENT；
2. 封闭未来 worker 领取；
3. 全额退还对应 charge 并写本地原因记录。

没有公共“强制退额”接口。任何一次发送尝试发生后，必须走 owner 的封闭/清理/退额协议。取消和领取竞争必须有真实并发测试。

## 9. 内部退额协议：只上报可退额事实

### 9.1 RPC

新源 Proto：api/protos/quota/service/v1/quota_release.proto。

固定服务：quota.service.v1.QuotaReleaseService。

固定方法：ReportQuotaRelease。不带 HTTP 注解、不进入 admin BFF、公共 Swagger 或登录白名单。

请求字段：

| 字段 | 含义 |
| --- | --- |
| release_event_id | UUID；同一逻辑通知重试保持不变 |
| operation_id | 原创建 operation_id |
| items | 非空，最多 16 项；每项 charge_id、quota_code、released_total |
| reason | 仅 ABORTED_CLEANED 或 RESOURCE_RELEASED |
| resource_refs | 本次事实涉及的模拟资源标识，最多 64 项；仅审计，不用它推导可信租户 |

请求**不包含可作为身份依据的 tenant_id 或 owner_service**。租户/owner 从 charge 及已验证证书取出。协议内部数量用整数类型，日志/JSON 遵循 protobuf 表达。

响应每项返回 charge_id、applied_delta、released_total；重复通知也返回当前权威累计值。

### 9.2 累计释放规则

资源服务报告的是“该 charge 自创建以来累计可退还多少”，不是本次随意扣减多少。

~~~text
incoming_total 必须满足 0 <= incoming_total <= original_units
new_total = MAX(stored_released_total, incoming_total)
delta = new_total - stored_released_total
occupied_units -= delta
~~~

- 同一 RPC 各 item 原子处理，任一非法整笔回滚。
- 同一个 release_event_id 同内容重试幂等；同 ID 不同内容返回冲突。
- payload_hash 基于固定结构、排序后的唯一 items/resource_refs；排除传输 request-id，不把 JSON 字段顺序视为不同业务内容。
- 不同 event_id 携带相同累计值不重复退额。
- 旧累计值晚到是合法 no-op；先到 2 后到 1，仍保持已释放 2。
- 0 允许作为 no-op，不产生负占用；items 为空不允许。
- released_total 超过 original、跨 operation 的 charge、重复 item、计量 code 不匹配全部拒绝。
- 未知 charge 返回 NotFound，绝不自动创建一个负余额“补偿”。
- receipt 与 charge/account 修改同事务提交，数据库错误返回 Unavailable，让 owner 保留并重试。
- 完全释放的 charge 永久保留幂等信息。本批不做历史清理或时间窗淘汰。

### 9.3 身份与部署边界

- 新建独立 gRPC listener，TLS 最低 1.3，RequireAndVerifyClientCert。
- 验证链后必须匹配精确 DNS SAN 与已注册 owner；同 CA 的另一服务不能退他人额度。
- 不接受 X-Forwarded-*、owner header、Bearer 或 tenant 参数替代证书身份。
- lab 的 owner 身份固定 ani-gpu-simulator；Governance 服务身份固定 ani-governance。
- 仅 quota_lab 构建注册模拟 owner。正式构建不能配置一个字符串就加载模拟 adapter。
- 配置：ANI_QUOTA_ENABLED、ANI_QUOTA_INTERNAL_ADDR、ANI_QUOTA_CA_FILE、ANI_QUOTA_CERT_FILE、ANI_QUOTA_KEY_FILE。
- 默认 disabled；enabled 时地址/证书/owner 映射缺失或不合法，启动报错，不降级明文。
- 退额不检查套餐是否到期、租户是否 ON、当前余额是否已超限、原用户 token 是否有效；仍严格验证 owner 和原账本归属。
- 本批不改全局到期闸门。过期租户主动发 DELETE 仍受原策略约束；测试通过资源服务后台清理验证到期后内部退额可用。

## 10. 对外管理接口与实验接口

### 10.1 正式管理接口

| 登记号 | 方法/路径 | 合同 |
| --- | --- | --- |
| QUOTA-01 | GET /admin/v1/quota-definitions | 目录读取，分页沿用现有分页合同；items 含 code/name/unit/kind/enforcement |
| QUOTA-02 | GET /admin/v1/tenants/{id}/quota-accounts | 平台管理读取指定租户账户；返回 limit/occupied/available/overLimit，不冒充真实运行量 |
| QUOTA-03 | gRPC /quota.service.v1.QuotaReleaseService/ReportQuotaRelease | 仅内部 mTLS |
| 复用 PLAN-11～14 | 现有套餐配额 CRUD | 增加 quotaCode 和约束，保持已有路径 |
| 复用 TENANT-04/08 | 租户绑定/旧 usage | 保留既有合同；旧 usage 补 quotaCode，不偷偷更改旧统计值 |

QUOTA-01/02 本批仅平台管理权限，租户身份不能通过修改 path id 查询他人账户；未来自助入口另行设计。Api 模块明确登记为 TENANT，与现有套餐管理一致，角色授权使用独立读权限点，不授予所有 SYSTEM 角色。

catalog/accounts 读取沿用现有成功 JSON 风格，不加全局 code/data envelope。不得因本批统一其他接口。

账户响应示例：

~~~json
{
  "tenantId": 7,
  "items": [
    {
      "quotaCode": "gpu.count",
      "unit": "gpu",
      "limit": "8",
      "occupied": "2",
      "available": "6",
      "overLimit": false,
      "enforcement": "LAB_ONLY"
    }
  ]
}
~~~

### 10.2 仅实验构建的用户入口

| 登记号 | 方法/路径 | 合同 |
| --- | --- | --- |
| QUOTA-LAB-01 | POST /api/v1/quota-lab/gpu-allocations | {data:{name,gpuCount}}；Idempotency-Key 必需；202，operationId/chargeId/resourceId |
| QUOTA-LAB-02 | GET /api/v1/quota-lab/gpu-allocations/{resource_id} | 经 Governance 查询 simulator；不触发占额和资源状态推进 |
| QUOTA-LAB-03 | DELETE /api/v1/quota-lab/gpu-allocations/{resource_id} | Idempotency-Key 必需；202，删除 operationId；不立即退额 |
| QUOTA-LAB-04 | GET /api/v1/quota-lab/operations/{operation_id} | 只返回本用户操作的投递状态和账本标识，不写数据库状态 |

- 实验入口接受真实用户 JWT，不支持 AK/SK 写签名。本批不得扩大现有只读 AK/SK 合同。
- 一般资源业务请求不接受 tenantId、actorId、ownerService、quotaCode、chargeId、故障参数。
- POST gpuCount 为 1～16 的整数；name 长度 1～64，去除首尾空白后不能为空。
- resource_id 由 Governance 随创建 operation 生成稳定 UUID；重试不得重新生成。
- DELETE 按 tenant/resource/原 charge 关联校验归属，不查询其他租户后泄露存在性；跨租户统一 404。
- 两个用户的同一幂等 key 各自独立，但不能互读对方操作。tenant 管理角色的实际资源权限由 fixture 明确授予。
- 仅 lab API 目录使用 TENANT 模块，fixture 给测试套餐该模块并给角色相应权限。这不是正式 GPU 模块归属决定。
- 新 lab Proto 放 quota_lab 独立目录，消息与 HTTP BFF 分开；不得塞入现有 Model/Network Proto。

### 10.3 错误合同

| 情形 | HTTP / gRPC 与 reason |
| --- | --- |
| 非法数量、未知字段、缺少 key | 400 INVALID_QUOTA_REQUEST |
| 相同幂等 key 不同有效报文 | 409 IDEMPOTENCY_CONFLICT |
| 额度不足 | 409 QUOTA_EXCEEDED |
| 配额未配置/适配器不可用 | 403 QUOTA_NOT_CONFIGURED / 503 QUOTA_ADAPTER_UNAVAILABLE |
| 套餐到期/租户不可新增 | 403 QUOTA_ADMISSION_DENIED |
| 占额数据库不可用 | 503 QUOTA_STORAGE_UNAVAILABLE，不转发 |
| 已持久接受，转发未知 | 202，稳定 operationId，不伪装成未发生 |
| 跨租户资源/操作读取 | 404，不泄露对方对象 |
| 退额错误服务身份 | gRPC Unauthenticated 或 PermissionDenied |
| 超额退还/同事件异内容 | gRPC FailedPrecondition，QUOTA_RELEASE_CONFLICT |
| 未知 charge | gRPC NotFound |
| 退额存储故障 | gRPC Unavailable |

沿用现有 Kratos 错误载体。可以在 quota 源域新增错误定义或集中错误构造文件，不修改全局错误编码器，不记录凭据。

## 11. 模拟器：必须能证明失败恢复

### 11.1 构建隔离

- 所有模拟实现置于 app/admin/service/internal/quotalab/，所有文件含 quota_lab build tag。
- 模拟器命令置于 app/admin/service/cmd/quota-gpu-simulator/，同样限定 build tag。
- Governance 仍复用 cmd/server 的真实装配；新增 wiring_quota_default.go 与 wiring_quota_lab.go，使用互斥 build tags 提供很小的装配钩子。
- 正常钩子不导入 quotalab、不注册实验路由/adapter；实验钩子添加这些内容。
- REST 可以增加一个显式 registrar 参数用于 lab 装配，不得复制一份弱化认证的 Governance。
- 生产配额 Repo/Service/worker/内部退额 server 不带 lab tag；必须由实验构建直接调用同一份实现。
- 正式 OpenAPI/首次种子不含 quota-lab 路由；lab API/权限通过任务 fixture 显式登记。
- 用正常二进制实际请求、路由清单和 go list -deps 证明隔离，不能只说“默认关闭开关”。

### 11.2 持久模型

模拟器不是 return success 的空桩。至少包括：

1. **owner 数据库**：命令/幂等表、资源记录、累计释放事实、待发送退额通知。
2. **provider 数据库**：GPU 单元分配表、operation 执行代次/封闭标记。
3. owner 与 provider 使用不同数据库和连接；不存在跨库事务。
4. 每个 GPU 单元有稳定 ID，由 resource_id 和序号确定；重复创建同单元不会新增第二条分配。
5. 创建请求先持久化 owner 操作，再推进 provider 分配，最后记录 owner 完成；允许在两者之间故障。
6. owner 重启后按 operation/resource ID 查询 provider，发现已经创建的单元并继续处理，不能因 owner 状态落后再次分配。
7. owner 的终态/幂等记录不在本批清理；全额退额后重放创建必须返回旧结果且不再分配。
8. owner/provider 的每张操作、资源、单元、释放事实、待通知表都带 tenant_id UUID，来源是 Governance 的持久 resource_tenant_id；其同库关联使用含 tenant_id 的复合约束，不用跨数据库 FK。
9. 模拟 provider 总容量固定 64 个整卡名额，测试没有多型号/共享切片；容量不足由 owner 走已接受操作的失败清理和退额流程，不能修改租户套餐额度。

provider 只是硬件行为的持久化模拟；不得把其数据库行称为物理 GPU 分配证据。

### 11.3 冻结的下游实验 RPC

在独立 lab 源 Proto 定义 quota_lab.service.v1.GpuSimulatorService，无 HTTP 注解。只允许以下三个 RPC，不开发通用资源 CRUD 框架：

| RPC | 固定输入 | 固定输出与规则 |
| --- | --- | --- |
| AcceptCreate | operation_id、resource_id、tenant_id(UUID)、actor、request_hash、name、gpu_count、charge_id、quota_code、charged_units | 持久化接受后返回 operation_id/resource_id/accepted=true；quota_code 必须 gpu.count，charged_units 必须等于 gpu_count |
| AcceptDelete | 删除 operation_id、原 create_operation_id、resource_id、tenant_id(UUID)、actor、request_hash、原 charge_id | 持久化接受后返回删除 operation_id/resource_id/accepted=true；原创建记录必须属于同租户和资源 |
| GetResource | tenant_id(UUID)、resource_id | 返回资源及模拟状态/单元数量；纯读取，不推进创建/清理 |

- Governance→simulator 同样使用 TLS 1.3 和双向证书验证；simulator 只接受精确身份 ani-governance。
- actor 复用当前可信 Principal.Actor() 的用户格式；不透传用户 Bearer 或公网身份头。
- 同 operation_id 同规范请求只返回原接受结果；同 ID 异内容为永久合同冲突，不能覆盖原操作。
- 协议字段/身份不合法时不执行资源变更。合法接受后的调度/容量/创建失败必须持久化处理并发可退额通知，不能只返回一个错误后丢掉操作。
- 没有成功回调到 Governance。ACK 只用于结束投递重试；真实资源状态由 GET 经 Governance 转发读取。
- 若测试需要检查内部完整事实，使用 simulator 控制面或只读 SQL，不给正式 Governance 增加资源状态同步任务。

### 11.4 防止迟到创建

provider 的 Allocate 与 Fence/Close 在同一 operation 的 provider 行锁上串行化：

- Allocate 提交前检查 execution_generation 匹配且未 closed。
- 终止操作先提交 closed，阻止所有旧代次继续分配。
- 然后枚举和清理已存在的 provider 单元。
- 有单元清理失败就保留对应占额，不发全额释放。
- 只有已经封闭且确认没有残留资源的部分，才允许 ABORTED_CLEANED。

必须通过“暂停旧创建 → 终止并清理 → 恢复旧创建”的可控屏障测试，证明旧创建被拒绝。不能仅在 owner 内存设置 canceled=true。

### 11.5 可靠退额

- owner 将累计释放事实和待发通知在自己的同一事务提交。
- owner 的释放事实以 (charge_id,unit_ordinal) 唯一。未创建但已封闭的名额也使用原申请中的 ordinal，和以后任何资源释放共享同一唯一性。不同 DELETE key、重复 provider 回执、ABORT 清理与 DELETE 并发都不得让同一名额产生两次退款事实。
- 累计值从这些唯一释放事实求和，绝不能“每次删除返回成功就 total+=1”。provider 必须保留可查的已释放墓碑，资源行不存在本身不作为新的退款事实。
- 回调失败保留通知；重启继续发送；成功 ACK 后标 delivered。
- 每个事件 ID 固定；同 charge 的累计释放量只能单调增加。
- 允许故障脚本模拟丢响应、重复发送、反序发送。
- 不使用 MQ；用数据库待发记录与后台 worker。
- 本批创建组采用整体成功或清理回滚。部分创建故障时先封闭创建，再清理已分配单元。
- 删除可以逐单元释放；每释放一部分即可上报累计数量，剩余失败单元继续占额。

### 11.6 故障控制面

- simulator 的控制面在独立 127.0.0.1 listener 提供。
- Governance 另有仅 quota_lab 装配的 worker 同步屏障钩子，通过任务专用控制 listener 或 pipe 接受脚本指令。正式构建使用无控制功能的钩子，不导入实验控制实现、不注册控制路由。
- 需要任务随机控制 token，文件 0600；不得放到公网用户 API。
- 必需控制点：占额提交后暂停投递、owner 接受后丢 ACK、provider 提交后暂停 owner 记录、创建第 N 单元失败、释放第 N 单元失败、阻断退额、重复/乱序回调、旧执行暂停与恢复。
- 使用显式 barrier/ack，而不是依赖任意 sleep 猜时序。
- FAIL-02 的 Governance 屏障必须在所有 worker 首次领取前安装；收到“占额事务已提交、尚未领取发送”ACK 后才终止 Governance。不能在 simulator 接收端阻断已发送 RPC 冒充这个窗口。
- FAIL-15 使用同一 Governance 屏障协调领取与 CANCELED_UNSENT 竞争。lease_generation 迟到回写也须可控复现。
- 能对 Governance 和 simulator 执行真正的进程终止/重启；内存函数返回错误不能代替重启验收。

## 12. 迁移、数据升级与恢复

### 12.1 迁移文件顺序

新增两份结构迁移，既有 baseline 和 atlas.sum 的既有条目不得重写：

1. 20260922190000_quota_expand.sql：目录/账本表、新 nullable quota_code、必要索引；暂不对旧套餐数据加非空/唯一约束。
2. sql/quota/001_catalog_and_backfill.sql：单独审查的版本数据脚本，插入固定目录，检查并回填旧行。
3. 20260922190100_quota_constraints.sql：验证回填完成，加套餐新非空/唯一/FK/数值检查。

禁止将目录初始化放进服务构造/启动，也不重跑 sql/bootstrap/001_initial.sql 来补新数据。更新 docs/deployment.md 明确这次 expand→data→constraints 顺序。

### 12.2 数据预检

先区分两类行：quota_code 为空的是待迁移旧行；已有有效 quota_code 的是已迁移/新建行。

对待迁移旧行必须检查：

- plan_id、quota_type、quota_value 为 NULL；
- 未知 quota_type；
- 同套餐同类型重复；
- 数量负数/越界；
- 不存在的 plan 引用；

对已有 quota_code 的行：检查目录存在、plan/value 合法、按 code 唯一；旧三项若提供 quota_type，必须一致；gpu.count 的 quota_type=NULL 是合法新行。不得因合法 GPU 行没有旧枚举而拒绝数据脚本重跑。

出现任一问题：数据脚本整笔失败，输出脱敏主键清单，不能 MAX/SUM/取第一条合并，不能删除问题行。测试里这些坏 fixture 必须证明“迁移拒绝且数据未变”。

正常旧行精确映射并保留 ID、plan_id、数量与审计时间，只回填 quota_code 为空的合法旧行。重复执行 data 脚本应校验一致并 no-op，不能覆盖已存在的不同定义。验收必须包含“首次升级后新增 GPU 配额，再重跑数据脚本”。

### 12.3 Atlas 与已知偏差

- 从 Ent schema 生成实现并刷新 app/admin/service/schema.sql。
- schema 导出器追加新复合约束，保留既有 AK 约束。
- Atlas diff 输出必须逐行审查，拆成上述两份结构迁移。
- 已知 baseline 与当前 Ent 对 files、sys_users.avatar 有偏差；本批不执行这些 DROP，不修改其历史迁移。将排除项写入证据。
- 排除无关候选后重新计算新迁移校验并验证 atlas.sum，不修改已执行版本。
- 迁移只能在本任务新库/升级副本执行；禁止 --allow-dirty 掩盖脏基线。
- 本地脚本用 migrate status 核对当前位置，按一次一份 migrate apply 1 推进；每步核对实际版本。不能直接对未回填库 apply 全部 pending migrations。
- 服务启动配置始终 migrate:false，运行账号 DDL 测试应被拒绝。

### 12.4 存量和回滚

- 旧三个配额项只迁移配置，不伪造其真实已用量。
- 真实 GPU 不存在于本批隔离环境，模拟初始清单为空，GPU 账户可以从 0 开始。
- 若执行时发现拟接入环境已有真实 GPU 分配，停止该环境接入；其盘点/导入属于后续真实服务批次，不直接初始化为 0。
- 模拟验收必须至少恢复一次同一数据库中的非零占额，不能每次重建数据库“通过重启”。
- 迁移前备份基线库；恢复测试必须恢复到另一个任务库，检查原 ID/数量/约束和版本。
- 不承诺旧二进制能安全理解 GPU 新账本。旧版回滚只验证隔离备份恢复，不做在线回退；有未结束新操作时先停新请求并保全 owner/provider/账本证据。
- 不提供删除新表的通用 down 脚本，不用“清空后重建”替代数据保留测试。

## 13. 文件修改边界

### 13.1 允许的主要路径

~~~text
api/protos/quota/service/v1/                 新目录/账户/内部退额消息与 RPC
api/protos/admin/service/v1/i_quota_*.proto  正式管理 BFF
api/protos/identity/service/v1/plan_quota.proto
api/protos/identity/service/v1/tenant.proto  仅 QuotaUsage 新 code 字段
api/protos/quota_lab/                       实验消息和 BFF，独立生成
api/buf.quota.gen.yaml
api/buf.quota-lab.gen.yaml
api/gen/go/                                仅工具生成
app/admin/service/internal/data/ent/schema/quota_*.go
app/admin/service/internal/data/ent/schema/plan_quota.go
app/admin/service/internal/data/ent/        仅工具生成
app/admin/service/internal/data/quota_*.go
app/admin/service/internal/data/plan_quota_repo.go
app/admin/service/internal/data/tenant_usage_repo.go
app/admin/service/internal/data/tenant_repo.go  仅绑定串行化/删除保护
app/admin/service/internal/data/plan_repo.go    仅配额政策锁/删除保护
app/admin/service/internal/service/quota_*.go
app/admin/service/internal/service/plan_quota_service.go
app/admin/service/internal/service/tenant_service.go  仅配额相关保护映射
app/admin/service/internal/server/quota_internal_server.go
app/admin/service/internal/server/rest_server.go      最小参数/正式路由/实验钩子
app/admin/service/cmd/server/wiring_ent.go
app/admin/service/cmd/server/wiring_quota_default.go
app/admin/service/cmd/server/wiring_quota_lab.go
app/admin/service/cmd/server/main.go                  生命周期追加
app/admin/service/cmd/schema/main.go
app/admin/service/schema.sql
app/admin/service/internal/quotalab/
app/admin/service/cmd/quota-gpu-simulator/
migrations/20260922190000_quota_expand.sql
migrations/20260922190100_quota_constraints.sql
migrations/atlas.sum
sql/quota/001_catalog_and_backfill.sql
scripts/generate-quota-slice.sh
scripts/quota-lab/
docs/deployment.md
docs/interface-integration-register.md
docs/evidence/quota-gpu-local-01/
~~~

同目录必要测试文件允许增加。生产 OpenAPI 嵌入产物按原生成路径更新。若需要额外路径，先说明与本批的直接依赖；涉及其他域行为或改变本计划合同必须停止并报告，不能边做边扩范围。

### 13.2 明确禁止

- 不修改其他仓库，不添加对模拟器的正式外部 Go 模块依赖。
- 不把配额扣减塞进通用 HTTP 日志、通用租户中间件或所有 POST 的自动拦截。
- 不按 URL 关键词推断 GPU 数量；数量由显式注册的业务 adapter 提取。
- 不用 Redis/内存计数器作为配额权威账本。
- 不在 request header 或 JSON 中相信调用方的 tenant/owner/释放权限。
- 不因模拟器缺少能力而绕过 mTLS、Casbin、租户 FK 或数据库事务。
- 不为测试方便将 tenant=0 当作可创建任意租户 GPU 的身份。
- 不将模拟接口加入生产首次种子。

## 14. 执行阶段与每阶段退出条件

### P0：输入和本地环境冻结

1. 读完本文与第 3 节源码，记录基线、工作区、工具版本、镜像 digest。
2. 创建任务目录和 manifest，确认 Docker/监听/数据库均本地且独占。
3. 列出未来改动文件、生产/实验构建边界和 API 清单。
4. 先跑受影响的现有定向测试，保存原有失败；不修无关问题。
5. 编写 evidence/README.md 的验收表空模板，所有项初始 not_verified。

退出条件：本地隔离前提成立，未执行任何真实环境操作，已知前置缺口明确。

### P1：合同、目录与兼容

1. 更新接口登记计划状态，复用既有 PLAN/TENANT 编号。
2. 写新 Proto、兼容转换器、目录 schema 和管理读取接口。
3. 先固定第 5、9、10 节请求/响应及错误测试，再生成代码。
4. 更新旧 PlanQuota CRUD 和 QuotaUsage 映射，所有兼容判断集中。
5. 不接入模拟器、不伪称强制限额完成。

退出条件：旧三项请求读取保持兼容；gpu.count 可配置；非法/重复/错 mask 被拒绝。

### P2：迁移与持久账本

1. 写 Ent schema、数据库约束、schema 导出增量。
2. 按第 12 节生成/审查 expand、数据脚本、constraints。
3. 用真实 PostgreSQL 验证空库、带旧数据升级、坏数据拒绝、备份恢复。
4. 实现占额/累计释放纯数据库事务，执行并发/隔离/多项原子测试。
5. 完成 tenant 删除保护及政策更新锁，登记行为变化。

退出条件：核心不变量、复合 FK、限额变更与并发测试通过；不能仅 SQLite 通过。

### P3：Governance 转发和内部退额

1. 实现通用 adapter registry、operation 投递 worker、未知结果恢复。
2. 实现内部 mTLS server 和退额 RPC；测试证书链/精确 owner/重复通知。
3. 在手写 wiring 和 app 生命周期接入；构造无 I/O，Stop 可回收。
4. 验证缺配置 fail-closed、disabled 不监听、enabled 无凭据启动失败。

退出条件：数据库提交前无下游调用；转发重试固定 ID；未知结果不退额。

### P4：持久 GPU 模拟器和实验入口

1. 实现 build-tag 隔离钩子，复用真实 Governance 认证与配额实现。
2. 实现独立 simulator 进程、owner/provider 两个数据库和执行封闭机制。
3. 实现真实 mTLS 下游命令/查询和内部退额客户端。
4. 实现故障控制面与明确 barrier。
5. API 目录/角色/套餐/两租户数据只通过任务 fixture 初始化。

退出条件：最小成功创建和释放可复现，owner/provider/Gov 三方持久记录可关联。

### P5：正常链与权限链

1. 使用实际登录取得 JWT；平台创建套餐，设置 gpu.count=8，绑定两个测试租户，设置未来到期时间。
2. 租户用户创建 2 张，观察 Governance occupied=2/provider 单元=2。
3. 查询资源经 Governance 转发；成功不再扣第二次。
4. 删除资源，确认 provider 无占用后累计释放到 2，Governance occupied=0。
5. 跑跨租户、无权限、无套餐、未配置配额、错误证书等负向。

退出条件：所有正常链/权限链断言有业务和 DB 证据。

### P6：并发、故障、重启、政策变化

按第 16 节矩阵逐项执行，不合并或删除用例。每次故障记录注入点、前后账本、owner/provider 状态、重试次数和最终不变量。

退出条件：所有必需恢复用例 pass；不能把资源泄漏或保留占额写为“暂时可接受”而关闭。

### P7：构建边界、回归与可复现性

1. 正式与 lab 构建分别编译。
2. 实际运行正式二进制，实验路径返回 404，且无 simulator 依赖。
3. 二次生成相关文件，无未解释差异。
4. 跑相关既有测试和新增完整测试；检查迁移/文档/接口登记一致。
5. 使用脚本从新的任务目录再完成一次最小创建/释放，不手工补 SQL。

退出条件：生产不夹带模拟业务，复现脚本完整，回归边界明确。

### P8：交付和清理

1. 完成证据索引、逐项验收表、未验证项、恢复说明。
2. 更新接口登记为“本地模拟通过；真实 GPU 未接入”，不关闭总登记。
3. 停止任务进程；按 manifest 清理任务容器/资源，不删除其他本地数据。
4. 保留脱敏证据；敏感临时文件按任务清理规则处理。
5. 给用户文件清单、复现命令、真实接入剩余条件；不自动 commit/push。

## 15. 固定本地脚本与命令合同

以下新脚本是执行阶段必须实现的交付物，**当前尚不存在**。统一入口 scripts/quota-lab/run.py，子命令固定为：

~~~text
preflight  冻结环境与版本，只做检测/清单
prepare    创建任务隔离依赖、证书、配置
migrate    按 expand/data/constraints 顺序迁移与验证
build      编译正式、quota_lab Governance、simulator
start      启动服务并等待真实健康检查
seed       显式离线首次初始化、同步/登记 API、准备权限与 fixture 输入
accept     执行指定验收，输出机器可读结果
collect    汇总脱敏证据
stop       停止任务进程，保留数据供重启
cleanup    只清理 manifest 标记的任务资源
~~~

公共参数 --run-dir 必填，为任务独占绝对路径；路径名必须含 quota-gpu-local-01。prepare 不得复用未知目录；cleanup 校验 marker/容器 label/PID 命令行，禁止范围清理。

必须同时实现 accept --suite normal、migration、auth、concurrency、recovery、boundary、all；all 包含全部必需项。单用例允许 --case FAIL-02 等稳定 ID。accept 保存参数/配置摘要，执行失败保留现场，不自动清空数据库后重试。

build 固定生成三种二进制，输出到 run-dir/bin：

~~~bash
go build -o "$QUOTA_LAB_RUN_DIR/bin/governance" ./app/admin/service/cmd/server
go build -tags quota_lab -o "$QUOTA_LAB_RUN_DIR/bin/governance-quota-lab" ./app/admin/service/cmd/server
go build -tags quota_lab -o "$QUOTA_LAB_RUN_DIR/bin/gpu-simulator" ./app/admin/service/cmd/quota-gpu-simulator
go build -o "$QUOTA_LAB_RUN_DIR/bin/admin" ./app/admin/service/cmd/admin
~~~

这里是三种服务二进制加一个既有 admin 工具。正常构建和 lab 构建必须输出到不同文件，不能覆盖后误用。

复现命令模板：

~~~bash
export GOWORK=off
export GOMAXPROCS=2
export GOFLAGS=-p=2
export QUOTA_LAB_RUN_DIR="$(mktemp -d /tmp/quota-gpu-local-01.XXXXXX)"
python3 scripts/quota-lab/run.py preflight --run-dir "$QUOTA_LAB_RUN_DIR"
python3 scripts/quota-lab/run.py prepare --run-dir "$QUOTA_LAB_RUN_DIR"
python3 scripts/quota-lab/run.py migrate --run-dir "$QUOTA_LAB_RUN_DIR"
bash scripts/generate-quota-slice.sh
python3 scripts/quota-lab/run.py build --run-dir "$QUOTA_LAB_RUN_DIR"
python3 scripts/quota-lab/run.py seed --run-dir "$QUOTA_LAB_RUN_DIR"
python3 scripts/quota-lab/run.py start --run-dir "$QUOTA_LAB_RUN_DIR"
python3 scripts/quota-lab/run.py accept --run-dir "$QUOTA_LAB_RUN_DIR" --suite all
python3 scripts/quota-lab/run.py collect --run-dir "$QUOTA_LAB_RUN_DIR"
python3 scripts/quota-lab/run.py stop --run-dir "$QUOTA_LAB_RUN_DIR"
~~~

seed 中数据库首次初始化与 API 同步在启动前执行；需要登录 API 创建的套餐/租户 fixture 由 start 完成后 accept 的 setup 步骤执行。不得为了满足命令顺序调用未启动服务。明确将 seed 分为“离线初始化”和 accept.setup“真实 API fixture”。

执行开发阶段先生成新 schema/迁移再运行 migrate；上述复现模板假设代码与迁移已交付。不得在 accept 时现场生成或修改迁移。

新增强制测试名称：

~~~bash
go test ./app/admin/service/internal/data ./app/admin/service/internal/service -run '^TestPlanQuota' -count=1
go test ./app/admin/service/internal/server ./pkg/middleware/auth -count=1
go test -tags quota_pg ./app/admin/service/internal/data -run '^TestQuotaPostgres' -count=1
go test ./app/admin/service/internal/service -run '^TestQuota' -count=1
go test ./app/admin/service/internal/server -run '^TestQuota' -count=1
go test -tags quota_lab ./app/admin/service/internal/quotalab/... -count=1
make build_only
~~~

- 强制 PostgreSQL 集成测试使用 quota_pg build tag，避免让普通无数据库单测隐式连接环境。显式执行该 tag 时，从任务环境文件读取专用 DSN；缺少 DSN 必须失败，不得 Skip 后称通过。accept --suite all 必须包含这组测试，不能因普通 go test 通过而省略。
- 全进程 suite 必须实际调用生成/真实路由及 gRPC，不得替换核心 repo 为内存 mock。
- 现有 SQLite 测试可做映射回归，不能代替 PostgreSQL 锁、复合 FK、迁移和恢复验收。
- 如隔离环境允许，新增并发测试再运行 race；race 结果不能替代真实数据库并发测试。
- 不默认要求修复全仓已有无关测试；记录基线失败和影响分析，受影响必需测试必须通过。

## 16. 必需验收矩阵

每行一个独立结果 ID，不允许整组一个笼统 PASS。accept 失败退出码非零；最终报告必须保留 fail/not_verified。

| ID | 场景 | 必须观察到的结果 |
| --- | --- | --- |
| CFG-01 | 自定义套餐，gpu.count=8，绑定租户，未来到期 | API 读回一致，不改代码即可改为 10 |
| CFG-02 | 两租户绑定同套餐 | 各自独立 8，不能相互扣额 |
| CFG-03 | 旧三枚举请求/新 code 请求 | 映射正确，旧字段和新字段冲突拒绝 |
| CFG-04 | 同套餐重复 code、空值、负数、小数、越界 | 拒绝且无脏数据 |
| CFG-05 | 目录显示能力 | 老项 LEGACY_CONFIG_ONLY，GPU LAB_ONLY，不误报真实支持 |
| DB-01 | 空库顺序迁移 | 版本/目录/FK/唯一/检查约束齐全 |
| DB-02 | 旧行升级 | ID、plan_id、数值、审计字段保持 |
| DB-03 | NULL/未知类型/重复行 fixture | 明确拒绝，不能合并或丢弃 |
| DB-04 | 直接 SQL 跨租户关联 | 复合 FK 拒绝 |
| DB-05 | 运行账号 DDL | 拒绝；启动不迁移/不播种 |
| DB-06 | 备份恢复 | 恢复到另一任务库且数据、版本核对一致 |
| DB-07 | 升级后新建 GPU 配额，再重跑数据脚本 | 合法 NULL 旧 enum 不报错，现有数据不变 |
| FLOW-01 | 创建 2 / 查询 / 删除 | 占 2 一次；provider 释放后才退到 0 |
| FLOW-02 | 创建成功 | Governance 不需要成功回调，不二次扣额 |
| FLOW-03 | 查询重复 20 次 | 不扣额、不推进资源状态 |
| FLOW-04 | 删除已删除资源/重放相同请求 | 不多退、不新增资源 |
| AUTH-01 | 未登录/权限不足 | 在占额前拒绝，owner 无收到操作 |
| AUTH-02 | 租户 A 访问 B 的资源/操作/账户 | 拒绝；不泄露对象数据 |
| AUTH-03 | 平台 tenant=0 请求实验创建 | 拒绝，不默认代租户创建 |
| AUTH-04 | 无配额项目/无套餐/额度为 0 | 拒绝新增，不视为无限 |
| AUTH-05 | 无证书/错误 CA/错误 SAN/同 CA 其他服务 | 内部退额拒绝 |
| AUTH-06 | 公网伪造 tenant/actor/owner/charge | 拒绝或忽略身份伪造，余额不变 |
| AUTH-07 | AK/SK 调实验写接口 | 拒绝，不扩大现有签名范围 |
| CON-01 | 上限 8，20 个并发请求各 1 | 只接受 8 个，持久 occupied=8 |
| CON-02 | 同幂等 key 并发 20 次 | 一个 operation/charge，一次逻辑创建 |
| CON-03 | 同 key 不同 gpuCount/name | 409，不新增或改写账本 |
| CON-04 | 多配额向量第二项不足 | 第一项也不扣；仅 repo 合成 fixture，不暴露新产品项 |
| CON-05 | 两 Governance 实例并发 | 共享 PG 仍不超额，无进程内锁假安全 |
| CON-06 | 同资源不同 DELETE key 并发，ABORT 与 DELETE 交叉 | 同一 ordinal 仅一个释放事实，不误退尚未释放单元 |
| POL-01 | 8 已占 6，将上限降到 4 | occupied=6，available=0，新申请拒绝 |
| POL-02 | 降额与新占额并发 | 能按数据库提交顺序解释，无使用失效政策的越序放行 |
| POL-03 | 换套餐/删套餐配额项 | 余额保留，按新政策拒绝或放行 |
| POL-04 | 到期边界及三种 expiry policy | 新占额立即拒绝，不等定时任务 |
| POL-05 | 到期/降额/用户 token 失效后内部释放 | 可信 owner 仍能退额 |
| POL-06 | 删除有配额历史的 tenant | 409 QUOTA_HISTORY_PRESENT，数据保留 |
| FAIL-01 | 占额事务失败 | 无下游调用，无残余 operation/余额 |
| FAIL-02 | 提交占额后、首次发送前 kill Governance | 同库重启恢复一个逻辑创建，不再扣额 |
| FAIL-03 | owner 接受后丢 ACK | Governance UNKNOWN 持额，同 ID 重试不重复创建 |
| FAIL-04 | provider 已分配、owner 未写完成时 kill simulator | 重启找到原分配，不再新增 GPU 单元 |
| FAIL-05 | 创建第 2 张失败，第 1 张清理失败 | 未证实释放部分继续占额，不整单退 |
| FAIL-06 | 清理恢复成功 | 封闭后无残留，再完成累计退额 |
| FAIL-07 | 旧创建暂停→终止封闭→恢复旧创建 | 旧代次在 provider 提交点被拒绝 |
| FAIL-08 | DELETE 第 2 单元释放失败 | 第 1 单元可部分退额，第 2 继续占额 |
| FAIL-09 | 退额服务断开/owner 发出后丢响应 | 持久通知重启重试，最终只退一次 |
| FAIL-10 | 先累计 2 后累计 1、重复累计 2 换 eventId | 保持 2，不重复扣减 |
| FAIL-11 | 相同 eventId 不同内容 | 冲突，整笔回滚 |
| FAIL-12 | released_total 超 original / 错 operation / 未知 charge | 拒绝，无负数和补造账 |
| FAIL-13 | 退额先于 Governance ACK 到达 | 合法处理，之后 ACK 不复活占额 |
| FAIL-14 | 全部退额后重放旧创建 | 命中终态，provider 不重新分配 |
| FAIL-15 | QUEUED 撤销与 worker 领取竞争 | 只出现“未发撤销”或“已尝试须 owner 处理”两种结果 |
| FAIL-16 | Governance DB 暂时不可用 | 新请求不转发，owner 退额保留待重试 |
| FAIL-17 | owner 永久合同错误 | 先证明暂停持额；撤销故障注入、ResumeDispatch 同一操作，完成执行与清理后归零；禁止直接改余额 |
| FAIL-18 | 租约接管后旧 worker 迟到返回失败 | generation 检查拒绝旧回写，新的 ACKED 不回退 |
| EXT-01 | 测试内注册第二个合成 CONCURRENT 项 | 相同账本支持，无新增资源专用表/核心 switch；不放生产目录 |
| BOUND-01 | 正式构建请求所有 quota-lab 路径 | 404，OpenAPI/首次种子无实验路径 |
| BOUND-02 | 正式 go list -deps | 不含 quotalab 或 simulator 实现 |
| BOUND-03 | 构造与默认启动 | disabled 无内部监听；enabled 缺证书 fail-closed |
| BOUND-04 | 两次定向生成 | 无未解释 diff，不手改生成代码 |
| INV-01 | 每个 suite 结束，按租户/项目重算账本 | occupied 恒等式全部成立 |
| INV-02 | 最终释放全部模拟资源后 | provider 占用 0、账本 0、待通知 0、没有未处理故障 |

多维度和 EXT-01 的合成项目只在隔离测试事务/测试 fixture 中出现，不能扩展正式产品目录或对外 API。

## 17. 证据格式和交付标准

提交到 docs/evidence/quota-gpu-local-01/：

- README.md：基线、范围、命令、结果索引、真实 GPU not_verified。
- acceptance.json：每个测试 ID 的 pass/fail/not_verified、开始/结束时间、证据文件、失败原因。
- versions.lock：工具版本/校验值、容器 digest，不含凭据。
- migration-report.md：旧数据预检、expand/data/constraints 版本、排除的既有 schema 偏差、恢复结果。
- contract.md：最终请求/响应例子、错误、幂等和累计释放语义；接口状态仍以统一登记为准。
- recovery-report.md：故障屏障、被终止进程、重启同库证据、稳定 operation/charge/resource 关联。
- invariant-report.json：SQL 重算的逐租户/项目结果，及模拟 provider 最终状态。
- production-boundary.md：正式路由、OpenAPI、依赖检查结果。
- cleanup-report.md：仅本任务资源的保留/清理结果。

日志只保留定位需要的信息，禁止提交 JWT、SK、密码、证书私钥、完整 DSN 或用户敏感数据。大体积原始日志留在任务目录，仓库只放摘要和校验值；证据不能只有文字声明，没有可复现命令和观察结果。

完成定义：

1. P0～P8 完成且第 16 节全部必需项 pass。
2. 真实 GPU 一栏固定 not_verified，不算本批失败，也不能改成 pass。
3. 无未解释生成 diff，无无关代码更改，git diff --check 通过。
4. 既有认证、API Key/VPC 查询、套餐管理相关回归通过或有明确执行前已存在的独立失败证据。
5. 交付用户前清楚说明新增 tenant 删除保护、旧字段兼容和 lab-only 能力。

## 18. 立即停止并报告的情况

- 需要真实 GPU/远端主机/其他仓库才能继续，而本地模拟方案无法满足当前步骤。
- 想把模拟路由暴露到正式构建、绕过认证，或用内存代替必需 PG 测试。
- 迁移遇到歧义旧数据、未知 schema 偏差、非任务数据库或无法验证的工具版本。
- 必须改变本文冻结的计量、幂等、身份、累计退额或租户隔离合同。
- 发现资源实际存在却已全额退还，或重复/迟到执行能重新创建资源。
- 无法证明失败后的资源封闭/清理，却准备通过 TTL、人工改余额或删除记录“修复”。

报告必须包含：失败阶段、复现步骤、实际观察、受影响文件/数据、已保全证据、可继续的独立工作。不得把阻塞项标为通过，也不得仅剩硬问题时宣称整个批次完成。

## 19. 后续真实资源服务接入条件（本批只记录）

真实资源 owner 准备好后，下一批必须重新核对其权威分配来源、持久幂等、迟到执行封闭、真实释放证明及回调重试能力，再注册正式 adapter/路由和服务身份。

本批可复用的是目录、套餐配置、租户账本、一次占额、累计退额、转发恢复和合同测试。模拟 provider 的数据库锁不是现实硬件侧 fencing 的证明，不能原样声称真实服务已经满足。

不得将下一批工作顺带实施，也不得因为它尚未开始而把本批本地验收留空。

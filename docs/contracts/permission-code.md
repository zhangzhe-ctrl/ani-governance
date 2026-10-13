# 权限码规范

本文件规定 ani-governance 控制面**权限码**（permission code）的形态、生成规则与权威来源，并给出与 [接口前缀统一映射表](../plans/gov-auth-session-api-unification/route-map.tsv) 的对应关系。它是权限目录（`sys_apis` / `sys_permissions` / `sys_permission_apis`）对齐与 `permissions sync` 重建的**权威依据**；实现与本规范冲突时，以本规范为准并在 [接口集成登记](../interface-integration-register.md) 记录差异。

> 日期：2026-10-13。范围：全部 6 个业务域（Network 租户 / Network 平台 / Image / Inference / Accelerator / Instance）与本次 `/admin/v1/* → /api/v1/admin/*` 前缀移位涉及的 admin 路由。本文件**只固化现行有效规则**，不引入新的推导算法，也不改变任何已上线授权码的值。

## 1. 两类 code（不可混用）

| 名称 | 载体 | 用途 | 产生方 |
|---|---|---|---|
| **授权码** | `sys_permissions.code`（唯一索引 `uix_perm_code`） | RBAC 的**授权单位**；角色经 `sys_role_permissions → permission_id → sys_permission_apis → api_id` 获得访问权 | ①首次种子（`sys:*`）②运维注册脚本 ③`permissions sync` 重建 |
| **推导码** | **无独立存储**，是 `appendAPis` 内的中间量 | 由 `(method, path)` 现算，用于把 API 聚合成权限并与授权码对齐 | [ConvertCodeByPath](../../pkg/utils/converter/api.go) + 覆盖表（§4） |

**关键事实**：运行时鉴权（[authorizer_provider.go](../../app/admin/service/internal/data/authorizer_provider.go)）按 `api.Path` + `api.Method` 装载 Casbin 策略，**从不读 code**。因此：

- 修改推导规则**本身不改变鉴权结果**；
- 但推导结果决定 `permissions sync`（`POST .../permissions/sync:perms`）**重建出的授权码**是什么。

`permissions sync` 是「**硬删 + 重建**」：[TruncateBizPermissions](../../app/admin/service/internal/data/permission_repo.go) 物理删除所有非 `sys:` 前缀权限，再按当前路径重新推导。故**路径一变、推导规则不配套，重建即塌缩**。相关机制与影响分析见 [接口前缀统一实施文档](../../local-remote/接口评估/认证会话与接口前缀/接口前缀统一-api-v1-admin-实施文档.md) §2/§9.1。

## 2. 授权码命名规则

统一为 `段(:段)*` 形态：全小写、`:` 分隔。已上线的形态如下。

| 形态 | 示例 | 来源 |
|---|---|---|
| `sys:{名称}` | `sys:platform_admin`、`sys:audit_logs` | 首次种子 [001_initial.sql](../../sql/bootstrap/001_initial.sql#L35-L42)，共 **7** 个；受 `TruncateBizPermissions` 保护（`sys:` 前缀不删），是唯一免于 sync 重建的授权码 |
| `{域}:{动作}` | `user:view`、`setting:view` | 推导（无覆盖命中时） |
| `{域}:{子资源}:{动作}` | `network:vpc:list`、`image:registration:create`、`accelerator:cluster:register`、`instance:operation:read` | 覆盖表注册码（§4） |
| `{域}:platform:{子资源}:{动作}` | `network:platform:vlan-network:create` | 平台网络覆盖表注册码（§4） |
| `quota:{子资源}:{动作}` | `quota:account:self-read` | Accelerator 注册脚本 |

- **动作词**：推导路径只产出基本四动作 `view` `create` `edit` `delete`；覆盖表可用领域动作（`list` `get` `read` `start` `stop` `exec` `adopt` `verify` `allocate` `bind` `register` `publish` `admin-list` 等）。**同一域内动作词不强制统一**（如 network 租户用 `list/get/create/delete`，平台用 `create/get/list/delete/verify/allocate`）。
- **域段（第一段）** 决定权限分组归属：`appendAPis` 以推导/覆盖码的首段前缀与已有菜单派生码比对，命中则归入对应模块，未命中落 `UncategorizedPermissionGroup`。因此域段必须与业务域一致（`network`、`image`、`accelerator`、`instance`、`inference`），**不得使用 `admin` 这类功能分组段**（前缀统一后 `admin` 已不是资源名，见 §3）。
- **码值一经上线即稳定**：运维脚本用 `ON CONFLICT(code) DO NOTHING` 幂等注册，重建亦须收敛到同一值（§6）。改码等于改 RBAC 授权单位，属独立变更，不并入本批。

## 3. 推导规则（`ConvertCodeByPath`）

对 `(method, path)` 现算 `resource:action`，步骤（[api.go](../../pkg/utils/converter/api.go) `pathToResource` / `methodToAction`）：

1. **剥前缀**（`stripVersionPrefix`）：去首段 `api`，去首个版本段（`vN`）。
2. **去参数段**（`removePathParams`）：移除整段 `{...}`。
3. **跳过功能分组段**：剥前缀后若首段为 `admin` 且仍有后续段，则跳过后取下一段作资源（否则 `/api/v1/admin/users` 会塌缩为 `admin:view`）。仅剩 `admin` 一段时保留 `admin:view`。
4. **取资源**：对剩余首段做单数化（`inflection.Singular`），取 `:` 之前的部分。
5. **动作**（`methodToAction`）：`GET→view`、`POST→create`、`PUT/PATCH→edit`、`DELETE→delete`；路径以 `/list` 结尾恒为 `view`；其他方法取小写方法名。

示例（现行行为）：

| 方法 | 路径 | 推导码 |
|---|---|---|
| GET | `/api/v1/admin/users` | `user:view` |
| POST | `/api/v1/admin/users` | `user:create` |
| GET | `/api/v1/admin/settings` | `setting:view` |
| GET | `/api/v1/admin` | `admin:view` |
| GET | `/api/v1/admin/networks/vlan-networks` | `network:view`（覆盖未命中时的回退） |
| GET | `/api/v1/instance-operations/{operation_id}` | `instance-operation:view` |
| POST | `/api/v1/instances/{data.instance_id}:start` | `instance:create` |

> `ConvertCodeByOperationID`（按 RPC operation 推导）已实现但**未启用**；启用属独立评估（P2），见 §7。

## 4. 覆盖表（手工注册码优先）

推导码在下列**覆盖表**命中 `(method, path)` 时被替换为注册码，使 `permissions sync` 重建结果与 `sys_permissions` 注册码收敛一致。优先级（`appendAPis`）：**推导 → Image → Network → PlatformNetwork**，首个命中即覆盖。

| 覆盖函数 | 定义文件 | 条目 | 路径空间 | 权限码形态 |
|---|---|---|---|---|
| `ImagePermission` | [image_policy.go](../../pkg/middleware/auth/image_policy.go) | **11** | `/api/v1/images/*` | `image:{子资源}:{动作}` |
| `NetworkPermission` | [network_policy.go](../../pkg/middleware/auth/network_policy.go) | **25** | `/api/v1/networks/*` | `network:{子资源}:{动作}` |
| `PlatformNetworkPermission` | [platform_network_policy.go](../../pkg/middleware/auth/platform_network_policy.go) | **27** | `/api/v1/admin/networks/*` | `network:platform:{子资源}:{动作}` |

- 三表路径空间**互不重叠**（`images` / `networks` / `admin/networks`），故分支顺序不影响结果。
- 覆盖表同时充当该路径空间的**注册码权威源**：与运维注册脚本（[sql/data](../../sql/data)、[scripts/ops/sql](../../scripts/ops/sql)）必须逐条一致。
- 覆盖函数只做 `(method, path) → code` 的映射；**不授予角色、不启用模块**，鉴权仍走身份 + 租户 + 套餐 + Casbin。

## 5. 映射表（`route-map.tsv`）

[route-map.tsv](../plans/gov-auth-session-api-unification/route-map.tsv) 是路由与权限码的**全量候选映射**，列定义：

| 列 | 含义 |
|---|---|
| `method` | HTTP 方法 |
| `old_path` / `new_path` | 迁移前 / 迁移后路径（`keep` 时二者相同） |
| `operation` | RPC operation（`Service_Method`；SSE 与新增接口为占位） |
| `service_tag` | Proto service tag，用于业务模块归类 |
| `classification` | `management_or_shared` / `existing_api_domain` / `existing_target_prefix` / `tenant_business` / `browser_auth` / `stream` |
| `action` | `rename` / `keep` / `new` / `rename_custom` |
| `source_file` | 手写 Proto / 配置文件路径 |
| `source_sha` | 冻结时的 main SHA（全表一致） |

分类计数（现行，共 **290** 数据行）：

| 维度 | 取值 |
|---|---|
| `action` | `rename` **191**、`keep` **97**、`new` **1**（csrf）、`rename_custom` **1**（`/events`） |
| `classification` | `management_or_shared` **180**、`existing_api_domain` **70**、`existing_target_prefix` **27**、`tenant_business` **10**、`browser_auth` **2**、`stream` **1** |

`keep` 97 = `existing_api_domain` 70（含 Instance 9）+ `existing_target_prefix` 27（平台网络）。

- `source_sha` 须等于**冻结时的最新 main**；main 前进后需重新冻结并补录新增路由，否则新路由游离在映射与验收之外。
- 映射表是 R08 等价性验收的输入：对每条 `rename`，须验证 `new_path` 的**权威码**（§6）等于迁移前 `old_path` 的授权码。

## 6. 一致性判据

对映射表中每条路由，其**权威码**定义为：

1. 覆盖表命中 → 取注册码；
2. 未命中 → 取 `ConvertCodeByPath` 推导码。

一致性要求：**`permissions sync` 重建出的授权码 == 迁移前该路由已上线的授权码**。任一不等即视为塌缩未消除，停止迁移。

| 路径空间 | 权威码来源 | 现状 |
|---|---|---|
| admin 191 条移位 | 推导（无覆盖） | 依赖 P1（§3 步骤 3）保持资源级码不变 |
| Network 租户 25 条 | `NetworkPermission` | 已覆盖，收敛 |
| Network 平台 27 条 | `PlatformNetworkPermission` | 已覆盖，收敛 |
| Image 11 条 | `ImagePermission` | 已覆盖，收敛 |
| Instance 9 条 | 推导（**无覆盖**） | **缺口**，见 §7 |
| Accelerator 20 条 | 推导（**无覆盖**） | **缺口**，见 §7 |
| Inference 2 条 | 推导（**无覆盖**） | **缺口**，见 §7 |

## 7. 已知缺口与残留

以下是**现行有效、但未被规范收敛**的项，登记为已知残留，不在本批处理：

1. **无覆盖函数的三域**：Instance(9)、Accelerator(20)、Inference(2) 只有运维脚本注册码，无取码覆盖。一旦执行 `permissions sync`，其注册码会被推导码替换（如 `instance:list → instance:view`、`network:vpc:list → network:view` 类）。修法同 §4（新增取码函数并接入 `appendAPis`），属独立变更。
2. **`POST` 一律映射 `create`**：动作维度塌缩（`methodToAction` 无路径语义），是既存问题；迁移不改变其输出，也不影响 §6 等价性判据。启用 `ConvertCodeByOperationID` 可一并解决（P2）。
3. **`permissions sync` 会丢角色绑定**：硬删重建后授权码拿到新 `permission_id`，`sys_role_permissions` 悬空且不自动重挂（[ListApiIDsByPermissionIDs](../../app/admin/service/internal/data/permission_repo.go) 只认存在且 `ON` 的权限）。该风险由 `SyncPermissions` 本身引入，与覆盖表无关；生产不应把它当发布步骤。详见 [部署说明](../deployment.md)。

## 8. 校验与登记

- 定向验证：`go test ./pkg/utils/converter ./pkg/middleware/auth`（推导规则 + 三条覆盖表的表驱动断言）。
- 权限目录一致性由 `admin sync-apis --dry-run` 复核；显式写入脚本见 [sql/data](../../sql/data)、[scripts/ops/sql](../../scripts/ops/sql)。
- 本规范的变更（新增覆盖、改推导规则、扩域）须在 [接口集成登记](../interface-integration-register.md) 增独立条目，**不并入接口风格批次**。

## 附：关键源码索引

| 主题 | 位置 |
|---|---|
| 推导规则 | [pkg/utils/converter/api.go](../../pkg/utils/converter/api.go)、[README](../../pkg/utils/converter/README.md) |
| 覆盖表 | [pkg/middleware/auth](../../pkg/middleware/auth)：`image_policy.go`、`network_policy.go`、`platform_network_policy.go` |
| 权限聚合与重建 | [permission_service.go](../../app/admin/service/internal/service/permission_service.go)（`appendAPis` / `SyncPermissions`）、[permission_repo.go](../../app/admin/service/internal/data/permission_repo.go) |
| 运行时鉴权 | [authorizer_provider.go](../../app/admin/service/internal/data/authorizer_provider.go)、[pkg/authorizer](../../pkg/authorizer) |
| 种子与注册脚本 | [001_initial.sql](../../sql/bootstrap/001_initial.sql)、[sql/data](../../sql/data)、[scripts/ops/sql](../../scripts/ops/sql) |
| 运维说明 | [API 权限运维](../api-role-permission-ops.md) |

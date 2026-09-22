# AK/SK 签名认证与 VPC 查询执行文档

日期：2026-09-22。状态：**本批实现、远端定向测试及真实隔离联调已完成**。本文继续作为接口和签名合同；第 9 节记录当前交付，完整证据见 [本批验收记录](evidence/aksk-vpc-20260922/README.md)。本次上线于独立 kind namespace，不代表共享生产环境切换。

**用户已明确：没有旧 Key，没有存量用户。按首次部署实施，不做存量数据迁移、旧凭证兼容或切换方案。**

长期接口与问题状态继续维护在 [接口登记文件](interface-integration-register.md)，复用 AK-01～07、AK-ISSUE-01～05、NET-01 和 NET-ISSUE-01。本文是执行说明，不另建一套接口登记或提前关闭问题。

## 1. 本批交付范围

完成这一条链路：

```text
租户管理员登录 → 创建绑定角色的 AK/SK
                           ↓
Python 使用 SK 签名 → Governance 共用认证层 → 租户/套餐/Casbin
                           ↓
                  现有 mTLS + 可信租户/actor
                           ↓
                  Network GetVPC → 本租户 VPC
```

- Key 管理采用 `/api/v1/auth/api-keys` 和 snake_case 字段，保留本仓 `data` 包装；普通用户 JWT 继续使用。
- 共用认证层统一用户与 Key 调用者，首批业务 API 只开放 `GET /api/v1/networks/vpcs/{vpc_id}`。
- 一个 Key 绑定一个本租户角色，复用现有 API 权限和套餐；不增加独立 IAM、scope 授权系统、单 Key 限流或全功能 SDK。
- Network 整合已有 VPC 只读 mTLS 接收代码并支持 Key actor；不重写证书体系或 VPC 查询。
- 移除现有“AK/SK 换 JWT”代码，直接实现签名调用。数据库结构由 Atlas 显式管理，启动不建表、不播种。
- 前端、其他业务 API、写请求签名、防重复执行、无中断主密钥轮换不属于本批。

## 2. API 合同

### 2.1 Key 管理

以下六个接口要求**用户 Bearer JWT 和对应管理权限**，Key 签名不能用于管理 Key。首批写权限只授予租户管理员；授权创建/改绑 Key 同时意味着允许其委派本租户角色。服务端必须校验被绑定角色属于当前租户、状态 ON、类型 TENANT，拒绝平台/模板角色；不能仅凭客户端传来的 `role_id` 放行。

首批新建业务 Key 要求可信租户 ID 大于 0；平台身份的租户 0 不是任意租户入口，不新增平台代建参数。Key 不继承创建者角色，运行时以绑定角色为准。

| 登记号 | 方法和路径 | 请求 | 成功响应 |
| --- | --- | --- | --- |
| AK-01 | `GET /api/v1/auth/api-keys` | 沿用当前 page/pageSize 分页 | 200，`{items,total}`，每项为 Key 信息 |
| AK-02 | `GET /api/v1/auth/api-keys/{key_id}` | 数字 ID | 200，Key 信息 |
| AK-03 | `POST /api/v1/auth/api-keys` | `{data:{name,role_id,expires_at?}}` | 201，`{data,secret_key}` |
| AK-04 | `PUT /api/v1/auth/api-keys/{key_id}` | `{data:{...},update_mask:"..."}` | 200，`{}` |
| AK-05 | `DELETE /api/v1/auth/api-keys/{key_id}` | 数字 ID | 200，`{status:"revoked"}` |
| AK-06 | `PUT /api/v1/auth/api-keys/{key_id}/secret` | `{}` | 200，`{data,secret_key}` |
| AK-07 | 移除 `POST /admin/v1/access-keys/token` | 不提供新的交换路由 | 删除对应 RPC、签发方法和免鉴权登记 |

Key 信息包含 `id/name/access_key/role_id/is_active/expires_at/created_at/last_used_at`。`id`、`role_id` 保持数字；SK 及其摘要/密文不进入信息对象。`last_used_at` 表示最近成功验签时间，不保证该次业务查询成功。未设置到期时间表示长期有效，创建默认启用。普通查询不能取得 SK；重置后只返回新 SK，AK 保持不变。

创建示例（角色 12 必须替换为目标租户真实角色）：

```http
POST /api/v1/auth/api-keys
Authorization: Bearer <租户管理员 JWT>
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

更新只允许 `name/role_id/is_active/expires_at`，要求非空 `update_mask`，不得通过通用更新修改 AK、SK、租户或创建人；不支持更新不存在对象时自动创建。掩码包含 `expires_at` 且值为 null 时清除到期时间，未列入掩码的字段不变；`role_id` 不允许清空。角色与其他字段在同一事务中更新。

停用请求：

```json
{"data":{"is_active":false},"update_mask":"isActive"}
```

这里保留一个明确的编码例外：外层字段名为 `update_mask`，其字符串值遵守 Protobuf FieldMask 的 lowerCamel 规则，例如 `roleId,expiresAt,isActive`。服务端解码后得到 `role_id/expires_at/is_active`，再把 `is_active` 映射为现有 ON/OFF 状态。不要写成 `"update_mask":"is_active"`，也不要为这一处更换整个项目的 JSON 解码器。[FieldMask 官方定义](https://protobuf.dev/reference/protobuf/google.protobuf/#field-mask)

### 2.2 业务接口与 OpenAPI

NET-01 保持原路径和响应 `{vpc:{...}}`，接受两种凭证之一：用户 JWT，或者本节约定的 Key 签名。请求没有查询参数和 body；VPC ID 为 `vpc_` 加 32 位小写十六进制。普通错误继续使用项目现有 Kratos 错误结构，不借本批统一所有接口风格。

| 情况 | 预期状态 |
| --- | --- |
| 缺少/错误/过时签名，Key 不存在、停用、删除或过期 | 401 |
| 同时提交 Bearer 和任一签名头 | 400，拒绝混用，不回退认证方式 |
| 已认证但无角色权限、租户/套餐拒绝、Key 调用户专用功能 | 403 |
| VPC 不存在或属于另一租户 | 同样的 404 |
| ID、查询参数、body 不符合本接口约束 | 400 |
| Key 存储/密钥解密或 Network 依赖故障 | 503，不伪装为凭证错误或放行 |
| 已连接的 Network 调用超时 | 504，沿用现有映射 |

在 [admin_doc.proto](../api/protos/admin/service/v1/admin_doc.proto) 添加 Bearer 和签名头的 security schemes，并只在 NET-01 的 operation 上声明 `BearerAuth` **或** `AccessKeyAuth + SignatureAuth + SignatureTime`。这三个签名 scheme 分别描述 `X-Access-Key/X-Signature/X-Timestamp`，同一 security 项内同时要求；现有其他接口的全局认证声明不批量改动。Key 管理明确只接受用户 Bearer。生成 OpenAPI 与实际代码限制必须一致，Swagger UI 不会仅凭 security 声明自动计算 HMAC。

## 3. 首批签名规范

本节给 Python 与 Go 一份相同的执行基准。它是 ANI 本批协议，不声明兼容 AWS 或其他云 SDK。

### 3.1 传输和规范化

| 项目 | 规则 |
| --- | --- |
| 算法 | HMAC-SHA256 |
| `X-Access-Key` | 服务端签发的公开 AK；`ak-` 前缀加 ASCII 字母、数字、`_` 或 `-` |
| `X-Timestamp` | 当前 Unix 秒，十进制正整数，不带空格、符号或前导零；以 int64 范围解析 |
| `X-Signature` | 64 位小写十六进制 HMAC 结果 |
| 时间窗 | 服务端当前 Unix 秒与请求时间的差在 `[-300,300]` 秒内，含边界 |
| 重放 | 首批只读 GET 允许时间窗内重复查询，不维护 nonce；不要据此开放写接口 |
| 认证头 | 三个头各出现一次；缺失、重复或逗号合并值拒绝，不接受查询参数中的凭证 |
| 方法 | `GET` 大写；首批只允许 NET-01，其他接口默认拒绝 Key 身份 |
| 路径 | 实际请求路径，包含 `/api/v1` 和真实 VPC ID，不使用 `{vpc_id}` 路由模板 |
| 查询与 body | 首批均为空；query 行保留为空行，body 摘要为 SHA256 空字节串 |
| URL | 正式使用 HTTPS；首批路径禁止百分号编码、额外斜杠等替代写法，代理保留路径，不依赖外部 `X-Forwarded-*` 计算签名 |

不包含 scheme、host、port、fragment；客户端直接配置 Governance 对外入口，不依赖重定向。不同环境使用不同凭证。后续需要 query/body 的 API 再补规范化与重放约定，本批不实现一个未经使用的通用签名 SDK。

### 3.2 签名输入

以下七项以单个 LF（`\n`）连接，UTF-8 编码，**最后一行没有换行**：

```text
ANI-HMAC-SHA256
GET
实际路径
查询字符串（本批为空行）
Access Key
时间戳
原始 body 的 SHA256 小写十六进制摘要
```

计算式：`hex_lower(HMAC_SHA256(SK的UTF-8原始字节, 上述规范化请求字节))`。SK 包含返回值中的完整 `sk-` 前缀，不先做 SHA256、不做 Base64 解码。服务端用恒定时间比较签名。空 body 摘要固定为：

```text
e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

固定测试向量（都是公开的虚构数据，只用于离线计算）：

| 输入 | 值 |
| --- | --- |
| AK | `ak-doc-example` |
| SK | `sk-doc-example-not-a-real-secret` |
| 时间戳 | `1700000000` |
| 路径 | `/api/v1/networks/vpcs/vpc_0123456789abcdef0123456789abcdef` |
| 规范化字节数 | 170 |
| 预期签名 | `7df61f06b799ae477b51975097825e2e154ff62dffc46842964a51ed1afaebeb` |

这个时间戳不能用于在线验收。Go 和 Python 都先对齐这组结果，再使用当前时间发请求；改动任一输入应导致验签失败。

## 4. 共用认证与下游身份

认证应集中在 `pkg/middleware/auth`，不要在每个 Service 中写验签。共同结果至少有主体类型 `user/api_key`、主体 ID、租户 ID、当前角色；Key ID 与用户 ID 明确区分，不能把 Key ID 或创建者 ID 塞进 `user_id`。

1. 入口选择一种认证方式。用户 JWT 沿用签名、会话等校验；签名请求按 AK 查询 Key 当前记录，校验状态、有效期、租户与绑定角色，解密 SK 并验签。
2. 只允许凭证查询使用最小范围的内部查找：以唯一 AK 找到候选记录，验签后建立租户上下文。不要因尚未取得租户就给整个请求设置平台/管理员 viewer。
3. 认证成功后共用租户状态、套餐模块、Casbin 以及 Ent tenant viewer 注入。Key `user_id=0` 不能被解释为平台或管理员；首批不复用用户个人数据范围来冒充人工身份。
4. `auth.FromContext` 相关上下文适配必须让新业务统一取调用者；已有用户业务保持兼容。只有明确开放的 operation 接受 Key，未知入口默认只接受用户。后续新接口只登记主体类型与权限；旧入口硬编码非零用户 ID 的判断，在实际接入时修正。
5. 审计从已验证的公共身份记录主体类型/ID/租户，不再仅解码 Bearer 推测身份。注意当前审计包裹在认证外层，需保证认证结果能到达审计回调；认证失败不能记录为某个已确认用户。不得记录 SK、完整签名头或含 SK 的创建/重置响应。

每次请求查 Key 和绑定角色当前状态，本批不加密钥缓存；停用、删除、重置、到期在后续请求生效，已经开始执行的请求不撤回。角色权限更新沿用既有策略刷新机制，不能把“每次查 Key”说成所有 Casbin 修改无需刷新。

Governance 仍将数值租户 ID 解析为持久化的 `resource_tenant_id` UUID；统一 actor 生成规则为：

```text
用户：governance:user:<非零用户ID>
Key：governance:access-key:<非零KeyID>
```

继续重建 `x-ani-tenant-id/x-ani-actor/x-ani-request-id`，不透传公网伪造头、原始 SK 或签名。Network 在验证 Governance mTLS 身份后信任这些头，校验请求 `tenant_id` 与 header 一致，按租户查询 VPC；不新增用户/角色二次查询。

Network 的已有实现位于提交 `9e56e1c675bb2102c8e84adc5a8dfd2962823ddd`，保留在 `codex/install-ceph`，本轮核对 main `66f787bd30134141726c596612501a83cf75bdb7` 尚不包含它。先检查执行时的最新状态再整合该提交；之前 merge-tree 预览只有 `internal/data/postgres.go` 冲突，要同时保留 main 的返回字段映射和该提交的依赖故障分类。不要整支合入其他平台工作。使用已有 `ANI_NETWORK_MODE=vpc-read`，扩展 actor 解析和对应测试。历史记录不等于本次新版已验收。

## 5. 数据与部署准备

### 5.1 Schema 与 SK

- Ent schema 使用敏感的 `secret_ciphertext` 和必填 `role_id`；删除已不使用的 `secret_hash` 及交换逻辑，不保留过渡字段。
- 使用已有 AES-256-GCM 实现的显式实例，给 AK/SK 增加专用部署输入 `ANI_ACCESS_KEY_ENCRYPTION_KEY_FILE`（已实现）：读取独立随机密钥文件，所有副本一致，文件不入库/镜像/版本控制。缺失或不合要求时启动报错；加密失败不写入 Key，不允许现有 `EncryptIfNeeded` 的无配置明文直通，也不允许解密时把无密文前缀的内容当明文使用。
- 首批约定文件内容为 32 字节安全随机值的 64 位十六进制文本，可带一个结尾换行；验证后使用该文本作为现有 Encryptor 的输入。部署 Secret 同步备份，丢失后仅凭数据库密文不能恢复 SK；本批不实现主密钥热轮换。
- 新建 Key 的角色与租户绑定同时写入；增加 `(tenant_id,role_id)` 指向角色 `(tenant_id,id)` 的约束及所需唯一索引，角色删除有引用时拒绝。不存在未分配角色的兼容状态。
- Ent schema 和 Atlas 结构文件保持一致，确保空库能得到最终结构。保留仓库显式 Atlas 执行方式即可，不新增旧 Key 转换脚本。
- 构造函数和服务启动不写结构、不播种；初始化通过独立 `admin init` 执行一次。

### 5.2 API、角色和套餐

1. 新路由继续映射现有 `AccessKeyService → SYSTEM` 模块，VPC 保持 `NETWORK`。新二进制嵌入最终 OpenAPI，由 `admin init` 在首次初始化时登记正确路由。
2. 直接修改首次种子，为六条新管理路由建立权限关联，给租户管理员授予 Key 管理权限；不能只修平台管理员，导致租户登录后仍不能创建 Key。种子删除已不用的旧路径引用，不写增量授权补丁。
3. Key 绑定的租户角色单独授予 `network:vpc:get`。可以继续使用 [bootstrap-network-access.sql](../scripts/bootstrap-network-access.sql)，但它要求真实租户/角色 ID、resource tenant UUID 和已开放 NETWORK 的套餐，且不会自动开放套餐。
4. 目标租户套餐保留基础 `DASHBOARD/OPM`（含登出、个人信息和角色查询），再增加 `SYSTEM`（管理 Key）与 `NETWORK`（查 VPC）；不要建一个只有后两项的套餐。如果套餐被多个租户共用，确认同套餐租户都会受影响，必要时使用专用套餐。开放模块不等于获得其中所有 API 权限。
5. 完成首次授权配置后刷新内存策略，最小方式为重启，随后重新登录。日后新增接口仍走既有 `sync-apis --dry-run` 和显式同步，本批不另建路径迁移流程。

### 5.3 首次部署顺序

执行前确认两仓版本、目标主机/集群和空业务库；沿用用户指定环境，不把历史 Ubuntu/Fedora 记录当作本次现状。本次已按第 9 节在独立环境执行，未清空共享资源。

1. 构建 Governance 服务/admin 和已整合的 Network vpc-read；提供 PostgreSQL、Redis、mTLS、SK 加密 Secret 配置。
2. 按 [部署说明](deployment.md) 对目标库显式建表；`ANI_DATABASE_DSN` 由部署环境提供，不把真实连接串写进命令记录：

   ```bash
   ./scripts/atlas.sh migrate status
   ./scripts/atlas.sh migrate apply --dry-run
   ./scripts/atlas.sh migrate apply
   ./bin/admin init --username admin --password-file /run/secrets/governance-admin-password
   ./bin/admin check
   ```

3. 启动两个服务，Governance 保持 `migrate: false`；启动不会重新执行 `init`。admin 制品须包含新 OpenAPI/种子，首管理员密码通过 Secret 文件传入。
4. 平台管理员登录，准备包含 `DASHBOARD/OPM/SYSTEM/NETWORK` 的套餐，创建租户及管理员（`IMMEDIATE` 激活即可）。按第 5.2 节为本租户角色配置 VPC 查询权限，确认 resource tenant UUID，并准备对应租户的 VPC 查询数据。
5. 租户管理员登录、创建绑定角色的 AK/SK，用第 7 节 Python 调真实 VPC，再执行第 8 节验收。

代码清理直接删除未使用的机器 JWT 签发分支和 `/token` 白名单。用户 JWT 只代表非零用户 ID，Key 由签名分支提供独立身份；没有历史机器令牌回收、Redis 批量清理或兼容期任务。

## 6. 实施顺序与文件入口

| 顺序 | 工作与主要入口 | 本步完成条件 |
| --- | --- | --- |
| 1 | 固定本文签名向量；修改 [Key 消息](../api/protos/access_key/service/v1/access_key.proto)、[BFF 路由](../api/protos/admin/service/v1/i_access_key.proto)、[Network BFF](../api/protos/admin/service/v1/i_network.proto) | 目标路由、JSON 名、201/删除响应、FieldMask 编码与本文一致；生成文件来自源 Proto |
| 2 | [Ent schema](../app/admin/service/internal/data/ent/schema/access_key.go)、[Repo](../app/admin/service/internal/data/access_key_repo.go)、[Service](../app/admin/service/internal/service/access_key_service.go)、Atlas | 加密 SK、同租户角色绑定、更新/清除/停用/重置能独立验证 |
| 3 | [auth](../pkg/middleware/auth/auth.go)、[REST 装配](../app/admin/service/internal/server/rest_server.go)、[Authenticator](../app/admin/service/internal/data/authenticator.go)、[审计](../pkg/middleware/logging/api_audit_log.go)、[wiring](../app/admin/service/cmd/server/wiring_ent.go) | 签名与 JWT 进入同一权限链；删除交换代码/白名单；Key 不冒充用户；审计不泄密 |
| 4 | [NetworkService](../app/admin/service/internal/service/network_service.go)、[NetworkClient](../app/admin/service/internal/data/network_client.go)，以及 Network 既有接收端 | 两类主体共用租户/actor 构造；已有 mTLS 接收整合后识别 Key |
| 5 | [模块映射](../pkg/constants/module_mapping.go)、[首次种子](../sql/bootstrap/001_initial.sql)、配置与部署说明 | 首次初始化即可获得管理权限，目标租户能配置 VPC 权限 |
| 6 | 定向测试、两仓入口编译、隔离部署、Python 实调与验收记录 | 第 8 节有真实证据；未覆盖项仍标 not_verified |

按仓库约定使用 Go 1.26.7 / gow v1.0.3；本批使用 `scripts/generate-aksk-slice.sh` 限定 Proto/Ent/OpenAPI 生成（Buf 1.60.0，底层插件固定），以及标准 `make openapi` 复验；不手改生成代码、不运行 Wire、不触发暂摘的 Model 生成脚本。生成的 `app/admin/service/schema.sql` 由 `cmd/schema` 补充 Ent 无法直接表达的同租户复合外键，Atlas 部署读取此文件，无需 Go 编译器。按已有 Atlas 结构管理方式让空库建表达到最终 schema，无需数据迁移程序。Network 若没有改变 API proto，无需仅因接收端实现改动升级 Governance API 模块依赖。创建 201 要通过手写 HTTP 装配/响应编码实现并实测，不能只改 OpenAPI 或手改生成 handler。

测试覆盖 `pkg/middleware/auth`、Key Repo/Service、Network client/service 与接收端；编译 Governance 服务/admin 和 Network 受影响入口即可。执行位置遵守当次用户指定的远端要求。构建成功不代替 PostgreSQL、权限、mTLS 和真实 HTTP 验收。

## 7. Python 客户端示例

将下面完整代码保存为 `aksk_vpc_client.py`。只使用 Python 3 标准库；默认验证 HTTPS 证书、不跟随重定向、不输出 SK/认证头。自签测试 CA 通过 `SSL_CERT_FILE` 指定，不关闭证书验证。HMAC 使用 [Python hmac](https://docs.python.org/3/library/hmac.html)，HTTP 使用 [urllib.request](https://docs.python.org/3/library/urllib.request.html)。

```python
import hashlib
import hmac
import json
import os
import re
import ssl
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


def sign_vpc(access_key, secret_key, vpc_id, timestamp=None):
    if not re.fullmatch(r"ak-[A-Za-z0-9_-]+", access_key):
        raise ValueError("invalid access key format")
    if not secret_key or secret_key != secret_key.strip():
        raise ValueError("secret key is empty or has surrounding whitespace")
    if not re.fullmatch(r"vpc_[0-9a-f]{32}", vpc_id):
        raise ValueError("invalid VPC ID")
    ts = str(int(time.time()) if timestamp is None else timestamp)
    if not re.fullmatch(r"[1-9][0-9]*", ts) or int(ts) > 2**63 - 1:
        raise ValueError("timestamp must be positive Unix seconds")
    path = "/api/v1/networks/vpcs/" + vpc_id
    canonical = "\n".join([
        "ANI-HMAC-SHA256",
        "GET",
        path,
        "",  # Empty query string; keep this line.
        access_key,
        ts,
        hashlib.sha256(b"").hexdigest(),
    ]).encode("utf-8")  # No trailing newline.
    signature = hmac.new(
        secret_key.encode("utf-8"), canonical, hashlib.sha256
    ).hexdigest()
    return path, {
        "X-Access-Key": access_key,
        "X-Timestamp": ts,
        "X-Signature": signature,
        "Accept": "application/json",
    }


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def get_vpc(base_url, access_key, secret_key, vpc_id):
    u = urllib.parse.urlsplit(base_url)
    if (u.scheme not in ("https", "http") or not u.hostname
            or u.username is not None or u.password is not None
            or u.path not in ("", "/") or u.query or u.fragment):
        raise ValueError("base URL must be an origin, e.g. https://host:port")
    if u.scheme == "http" and os.getenv("ANI_ALLOW_HTTP_FOR_TEST") != "1":
        raise ValueError("HTTP requires ANI_ALLOW_HTTP_FOR_TEST=1 in an isolated lab")
    path, headers = sign_vpc(access_key, secret_key, vpc_id)
    url = urllib.parse.urlunsplit((u.scheme, u.netloc, path, "", ""))
    req = urllib.request.Request(url, headers=headers, method="GET")
    opener = urllib.request.build_opener(
        NoRedirect(),
        urllib.request.HTTPSHandler(context=ssl.create_default_context()),
    )
    with opener.open(req, timeout=10) as response:
        result = json.load(response)
    vpc = result.get("vpc") if isinstance(result, dict) else None
    if not isinstance(vpc, dict) or vpc.get("id") != vpc_id:
        raise ValueError("response does not contain the requested VPC")
    return result


def self_test():
    _, headers = sign_vpc(
        "ak-doc-example", "sk-doc-example-not-a-real-secret",
        "vpc_0123456789abcdef0123456789abcdef", 1700000000,
    )
    expected = "7df61f06b799ae477b51975097825e2e154ff62dffc46842964a51ed1afaebeb"
    if not hmac.compare_digest(headers["X-Signature"], expected):
        raise RuntimeError("signature vector mismatch")
    print("signature self-test: PASS (offline only)")


if __name__ == "__main__":
    if sys.argv[1:] == ["--self-test"]:
        self_test()
    elif sys.argv[1:]:
        raise SystemExit("usage: python3 aksk_vpc_client.py [--self-test]")
    else:
        try:
            result = get_vpc(
                os.environ["ANI_BASE_URL"], os.environ["ANI_ACCESS_KEY"],
                os.environ["ANI_SECRET_KEY"], os.environ["ANI_VPC_ID"],
            )
            print(json.dumps(result, ensure_ascii=False, indent=2))
        except urllib.error.HTTPError as exc:
            raise SystemExit("Governance returned HTTP " + str(exc.code)) from None
        except (KeyError, ValueError, urllib.error.URLError) as exc:
            raise SystemExit(str(exc)) from None
```

先离线检查签名：

```bash
python3 aksk_vpc_client.py --self-test
```

新版服务完成部署后，先由租户管理员用第 2 节管理接口获得 AK/SK，再调用真实 VPC。下面的地址和 VPC ID 必须替换；输入 SK 不回显，也不把 SK 写进 shell 历史：

```bash
export ANI_BASE_URL='https://governance.example.com'
export ANI_VPC_ID='vpc_0123456789abcdef0123456789abcdef'
read -r -p 'Access Key: ' ANI_ACCESS_KEY
read -r -s -p 'Secret Key: ' ANI_SECRET_KEY
export ANI_ACCESS_KEY ANI_SECRET_KEY
python3 aksk_vpc_client.py
unset ANI_SECRET_KEY
```

隔离 NodePort 实验确需 HTTP 时，设置实际 `ANI_BASE_URL=http://<主机>:<NodePort>` 并显式设置 `ANI_ALLOW_HTTP_FOR_TEST=1`；正式环境保持 HTTPS。`ANI_BASE_URL` 只包含 origin，不再加 `/api/v1`。环境变量是示例输入方式，部署调用方应使用自己的 Secret 注入机制。

常见问题：401 检查机器时间、SK 是否已重置、空 query 行和结尾换行；403 检查绑定角色、SYSTEM/NETWORK 套餐和 API 权限；404 对照真实租户与 VPC；503 检查 Governance 密钥配置/数据库及 Network mTLS/地址。不能用重新初始化数据库处理这些问题。

## 8. 验收清单

使用独立租户 A/B 和各自真实持久化的 VPC；可用隔离数据库 fixture，不要求本批创建云网络，但应将“查持久化记录”与“验证网络数据面”分开说明。

| 项目 | 必须得到的证据 |
| --- | --- |
| 协议一致 | Go 与本文 Python 对固定向量一致；当前时间真实 HTTP 查询成功 |
| 主流程 | 租户管理员创建 Key → 签名 → Governance → Network mTLS → 返回与数据库一致的 VPC |
| 篡改和时间 | 错误 SK、替换路径/时间、过时或过远未来时间、缺/重复头被拒绝；受控时钟检查 300 秒边界 |
| 身份与权限 | 无权限、非启用/跨租户角色、无 NETWORK 套餐拒绝；签名 Key 不能调用用户专用和 Key 管理入口 |
| 隔离 | A 读 B 与不存在对象同样 404；公网伪造租户/actor 无效，Key 管理也不能跨租户读改 |
| 生命周期 | 停用/删除/到期拒绝；重置后旧 SK 拒绝、新 SK 成功；角色改绑后的权限符合更新结果 |
| 普通登录回归 | 新建平台/租户用户的登录、登出及用户 JWT 查询 VPC 均符合权限；Key 不冒充用户 |
| mTLS | 正确 Governance 证书成功；缺失/错误证书及 RPC/header 租户不一致拒绝；原用户 actor 仍可用 |
| 密钥与审计 | 数据库只有加密 SK，缺主密钥不能启动；缺/坏密文不降级；审计可区分 Key，日志和列表没有 SK/完整认证材料 |
| 首次初始化 | 空库显式建表和初始化后能登录、管理 Key、查询 VPC；服务重启不产生结构或初始化写入 |

每项记录 pass/fail/not_verified、两仓版本、命令/测试名称和证据位置。单元测试或 HTTP 200 不单独证明数据库隔离、真实 mTLS 或完整主流程；任何未完成项保留在原登记中。

## 9. 本批实际交付与验收（2026-09-22）

本批 AK-01～07、AK-ISSUE-01～05、NET-01/NET-ISSUE-01 的必要实现与验收已完成；整体接口登记仍进行中。实现基线为 Governance `5a2a2e8c6c303a280acf2f6168d63fcebca8990c`、Network `66f787bd30134141726c596612501a83cf75bdb7`。Network 仅按文件整合 `9e56e1c` 必要接收改动并扩展 Key actor，保留主线返回映射，没有整支合入平台工作。

- 实际环境：`ssh ubuntu` / `i-8yg2l7u8`，context `kind-kind-test`，独立 namespace `aksk-vpc-20260922`，独立 PostgreSQL/Redis/PVC、证书与 Secret。
- 实际 NodePort：`http://172.18.0.2:30188`（从 ubuntu 访问）。可通过 `ssh -N -L 17788:172.18.0.2:30188 ubuntu` 转发；没有把该内部入口声称为公网或生产 HTTPS。
- Governance 镜像：`ani-governance:aksk-20260922-06d94eac1f25e7d7`；Network：`ani-network-service:aksk-20260922-f3c503a6c8a4bce6`。实际运行 imageID、制品哈希和源码清单见证据。
- 从空库显式 Atlas 建表、admin init/check、平台创建套餐/租户/角色、租户管理员登录并创建 Key **201**、Python 直签、Governance 认证/权限、Network mTLS 及持久化 VPC 对照：**pass**。
- 第 8 节全部必要类别：**pass**。覆盖错误/篡改签名、±300 秒受控时钟边界、缺/重复/合并头、混用凭证、query/body、跨租户角色和资源、无权限/套餐、租户状态、Key/角色生命周期、用户专用限制、真实错误证书和租户上下文不一致。
- 缺主密钥启动拒绝，数据库缺/明文/坏密文均 503 且不降级；日志/审计无 SK 或完整请求签名，已验证用户与 Key 分别审计，错误签名不伪记已认证主体：**pass**。
- 新平台/租户登录、用户 JWT VPC 查询、登出与旧 JWT 失效；服务重启前后结构及 11 张初始化/业务表摘要一致、运行角色无 DDL 权限：**pass**。

复现入口：[`scripts/aksk-lab/README.md`](../scripts/aksk-lab/README.md)，执行 `prepare → build-images → deploy → setup → contract → mtls → final_checks → security_checks`。`setup` 通过有权限的平台用户创建本租户角色，默认租户管理员的角色写权限没有被扩张。Python 文件为 [`scripts/aksk_vpc_client.py`](../scripts/aksk_vpc_client.py)，支持 `--self-test`。

真实凭据仅保留于 ubuntu `/home/ubuntu/workspace/aksk-vpc-20260922/private/`（0700/0600）及本 namespace Secret，不提交或打印。原始失败和最终通过证据均保留；脚本修正包括角色配置操作者、NodePort 实际就绪、错误响应动态 request_id 比较和 TLS 负向后的独立转发。

必要事项无未完成项。网络数据面/云资源创建、其他业务 API、前端、公网 HTTPS、独立 IAM、存量迁移/回滚不在本批范围，**not_verified**。Atlas 同时发现实施前初始 SQL 与 Ent 在 `files`/`sys_users.avatar` 上的既有差异；未执行这两项 DROP，本批 AK/SK/审计/复合约束无差异，后续生成迁移仍须审查。

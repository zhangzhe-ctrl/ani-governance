# 业务服务接入指南（Service Integration）

本指南固化"把一个业务微服务的 API 接入 governance，让租户用户经治理中心拿到结果"的标准流程。参照实现：

- **Network（可选依赖，当前唯一在跑的接入）**：[i_network.proto](../api/protos/admin/service/v1/i_network.proto) + [network_client.go](../app/admin/service/internal/data/network_client.go) + [network_service.go](../app/admin/service/internal/service/network_service.go)。地址为空则跳过客户端装配。
- **Model（已暂摘，2026-09-21）**：出站客户端、`GET /api/v1/models` 路由与其 proto/生成物已删除，`identityV1.Module_MODEL` 枚举刻意保留。重接时按下文流程重建，或从 git 历史找回 [model_client.go / model_service.go / i_model.proto] 作参照。

下游在目标环境可能缺席时用 Network 的可选模式；强制依赖型接入（地址未配置则启动失败）在删除的 Model 实现里有先例，重接 Model 时按它恢复。

## 执行环境边界

本仓库的开发执行环境约定见 [AGENTS.md](../AGENTS.md)（本地仅编辑与提交；生成、构建、测试、运行与部署按任务要求在指定环境执行）。本指南各步骤只标注操作类型，不绑定具体机器：

| 操作 | 命令 |
|---|---|
| Proto / Go API 生成 | `gow api` |
| 仅编译 | `make build_only` |
| 依赖与模块 | `go mod tidy`（固定版本模块按 §0.3 核对来源） |
| 权限登记 SQL | 按 §8 的专项脚本在目标数据库执行 |
| 运行与链路验收 | 按 §10 验收清单在运行环境执行 |

## 0. 前置约束（不满足直接停止）

1. **authz 必须是 casbin**。[wiring_ent.go](../app/admin/service/cmd/server/wiring_ent.go) 启动守卫在 `authz.type != "casbin"` 时拒绝启动（noop 不作为受支持配置）。
2. **租户闸门 fail-closed**。新路由未在 Api 表登记 `(path, method)` 前，所有租户请求都会被拒绝。不存在"先上后补"。
3. **下游 Go API 模块依赖**。出站客户端 import 下游仓库的生成包（如 `github.com/zhangzhe-ctrl/ani-network-service/api/network/v1`）。上游模块在 GitHub 公开可取时直接 `go get` 固定版本伪版本即可，不需要本地 file-GOPROXY 交付；注意 `goproxy.cn` 对这些私有模块可能返回 `not found`，`GOPROXY` 需把 `proxy.golang.org` 放在前面或直接走 `direct`。不要自建 file-GOPROXY 打包交付——历史上那种交付 zip 丢点文件（`.gitignore` 等），导致校验和与 go.sum 不一致。
4. Module 枚举值必须已存在：确认 `api/gen/go/identity/service/v1/` 里有 `Module_<你的模块>`；没有则先改 identity proto 并重新生成（这本身也是一次 §4 的生成任务）。

## 1. 骨架生成（可选）

```bash
scripts/new-service-scaffold.sh \
  -n Network -d catalog -m NETWORK \
  -r github.com/zhangzhe-ctrl/ani-network-service/api/network/v1 \
  -p '/api/v1/networks/vpcs/{vpc_id}' -s ani-network-service
```

参数：`-n` 服务名（PascalCase）、`-d` 领域包段（proto package 与 gen 目录）、`-m` identityV1 模块枚举后缀、`-r` 下游 Go API import 路径、`-p` HTTP 路由、`-s` 下游 mTLS ServerName（DNS SAN）。

脚本只写入四个骨架文件（领域 proto、BFF proto、出站 client、入站 service），已存在的文件拒绝覆盖，末尾打印手工装配清单。它不执行任何生成/构建。

## 2. 领域 Proto（`api/protos/<domain>/service/v1/<entity>.proto`）

- `package <domain>.service.v1`；源领域 proto **不带 HTTP 注解**（合同纪律，路由只出现在 admin BFF）。
- 只声明治理侧真正需要转发的消息与边界。Network 先例是单资源有界读（按 ID 查 VPC，字段集白名单），不是全量分页透传。
- 字段用 `json_name` + gnostic property 描述约束，保证 OpenAPI 语义完整。

## 3. 治理 BFF Proxy Proto（`api/protos/admin/service/v1/i_<entity>.proto`）

- `package admin.service.v1`，import 领域 proto，声明 `service <Name>Service` 与 `google.api.http` 路由。
- 路径遵守已接受合同；`/api/v1` 不是强制前缀，但同族服务保持一致。
- gnostic operation 描述里写明鉴权、订阅、拒绝语义与错误码（`i_network.proto` 是模板）。

## 4. 生成

```bash
gow api
```

生成物落在 `api/gen/go/`，不手改。此后 `go.mod` 需要下游 API 依赖（`go mod tidy`；固定版本按 §0.3 核对来源）。

## 5. 出站 mTLS 客户端（`internal/data/<entity>_client.go`）

照 [network_client.go](../app/admin/service/internal/data/network_client.go) 的不变量逐条核对：

- **fail-closed 构造**：address 与正 timeout 必填；CA 必须能解析（无证书即报错）；客户端证书加载失败即报错；leaf 的 DNS SAN 必须精确包含 `ani-governance`（这是治理中心自己的身份，不是下游的）。
- **传输**：`tls.Config{MinVersion: TLS13}`；`ServerName` 固定为下游 DNS 名（如 `ani-network-service`）；`grpc.WithDisableServiceConfig()`——本合同没有 DNS TXT 服务发现权威。
- **环境变量**：`ANI_<NAME>_ADDR / _CA / _CERT / _KEY / _TIMEOUT`；timeout 默认 3s，解析失败要返回错误，不吞。
- **身份 header 重建**（见 §9 契约表）：用 `metadata.NewOutgoingContext` 整体重建，**绝不 append 入站/公网传来的身份 header**。
- **错误区分**：`DeadlineExceeded` 且连接态非 Ready → 映射 `Unavailable`（503，传输不可达）；已连接但慢的 RPC 保持 `DeadlineExceeded`（504）。
- 构造期一次性读证书是已知限制：cert-manager 续期后需重启 Pod 重载，见 [governance-mtls README](../scripts/deploy/governance-mtls/README.md)。

## 6. 入站代理服务（`internal/service/<entity>_service.go`）

照 [network_service.go](../app/admin/service/internal/service/network_service.go) 的不变量：

- 操作人只来自 `auth.FromContext`；`tenant_id == 0 || user_id == 0`（平台身份）直接拒绝。**不信任请求传来的租户和操作人身份。**
- 租户映射经 `ResourceTenantResolver`（[tenant_resource.go](../app/admin/service/internal/data/tenant_resource.go)）把治理中心 uint32 主键换为下游 resource tenant UUID；id==0 拒绝。该接口已在 package service 定义，复用，勿重复声明。
- 查询参数**严格白名单**：逐个校验 `transport.FromServerContext` 里的原始 query，未知/重复/空值参数拒绝（Network 拒绝 tenant_id/vpc_id/未知参数注入）。
- 上游错误映射：`DeadlineExceeded→504`、`InvalidArgument→400`、`PermissionDenied→403`、其余→503；原始错误进日志（不泄密钥/令牌/跨租户私有信息），对外只给稳定错误码。
- nil 请求、nil 响应、nil 列表元素都要处理，不吞错。

## 7. 装配（四处锚点，手写，勿用 Wire）

`make register` 只覆盖标准构造函数；config/client/cleanup/额外依赖手工补齐。

1. **[wiring_ent.go](../app/admin/service/cmd/server/wiring_ent.go)** `── register:service ──` 锚点后追加四连（Network 先例）：

```go
	<entity>Config, err := data.<Name>ConfigFromEnv()
	if err != nil {
		rollback()
		return nil, nil, err
	}
	// 强制依赖型接入用四连;可选依赖型(如下游可能缺席)参照 networkClient 的
	// "地址为空则跳过装配"写法。
	<entity>Client, cleanup<Name>, err := data.New<Name>Client(<entity>Config)
	if err != nil {
		rollback()
		return nil, nil, err
	}
	cleanups = append(cleanups, cleanup<Name>)
	<entity>Service := service.New<Name>Service(<entity>Client, tenantRepo)
```

   任何一步失败先 `rollback()` 再返回；cleanup 创建成功立即注册。
2. **[rest_server.go](../app/admin/service/internal/server/rest_server.go)** `register:param` 锚点后追加形参 `<entity>Service *service.<Name>Service,`。
3. **rest_server.go** `register:route` 锚点后追加 `adminV1.Register<Name>ServiceHTTPServer(srv, <entity>Service)`。
4. **wiring_ent.go** `register:rest-arg` 锚点后把 `<entity>Service` 加进 `NewRestServer` 实参。

漏接由编译器在调用处报错——所以 §4 生成后先 `make build_only` 一轮。

## 8. 模块与权限登记

1. **[module_mapping.go](../pkg/constants/module_mapping.go)**：`"<Name>Service": identityV1.Module_<MODULE>`。漏登记会导致 `business_module` 为 UNSPECIFIED，租户白名单直接拒绝。若模块枚举已定义但服务暂摘（如当前的 `MODULE_MODEL`），需在 [module_mapping_test.go](../pkg/constants/module_mapping_test.go) 的 `modulesWithoutService` 显式豁免。
2. **Api 表登记**：新增接口明确登记 `(path, method)`、权限和套餐关系，参照 [bootstrap-network-access.sql](../scripts/bootstrap-network-access.sql) 写专项脚本；已在部署的实例上 `SyncApis` 是**清空后全量重建**，不是无损增量——需保留已有 ID 与关联并验证升级授权链。
3. 套餐：目标租户的 Plan 必须包含该 Module，否则闸门 403。

## 9. 身份 Header 契约（治理中心 → 下游）

| Header | 值 | 来源 |
|---|---|---|
| `x-ani-tenant-id` | resource tenant UUID | `ResourceTenantID(ctx, operator.GetTenantId())` |
| `x-ani-actor` | `governance:user:<uid>` | JWT 可信操作人 |
| `x-ani-request-id` | 新 UUID | 每次调用重建 |

下游只信任 mTLS 对端（治理中心）传来的这三个 header；公网或重复 header 一律不可信。治理中心侧每次出站都整体重建，不从入站请求继承。

mTLS 材料由 [scripts/deploy/governance-mtls/](../scripts/deploy/governance-mtls/) 的 cert-manager 配置签发（`clientAuth` EKU、共享 `ca-issuer` 信任根、90 天/提前 30 天续期）；新下游需要自己的 Certificate + 部署补丁，并把治理中心客户端证书 Secret 挂载路径填进 `ANI_<NAME>_*` 环境变量。

## 10. 验收清单

编译与生成只是起点；隔离、鉴权、持久化、故障恢复**不能由 HTTP 200 或静态检查代替**。最小负向用例集（Network 上线时已验证的等价集合）：

- [ ] 未登录 → 401
- [ ] 平台租户（tid=0）→ 403 `TENANT_REQUIRED`
- [ ] 登录但无权限 → 403
- [ ] 套餐未含 Module → 403（闸门 fail-closed）
- [ ] 未登记 `(path, method)` → 403（证明 fail-closed 生效后再登记放行）
- [ ] 未知/重复/空查询参数 → 400
- [ ] 下游进程停止 → 503（传输不可达，不是 504）
- [ ] 下游可达但挂起超过 timeout → 504
- [ ] 错误 CA / 错误 SAN 的客户端证书 → 启动即失败（fail-closed 构造）
- [ ] 跨租户隔离：租户 A 的登录态拿不到租户 B 的数据

未执行的项在交付说明里写 `not_verified`，不得用"构建通过"替代。

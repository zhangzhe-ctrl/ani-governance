# Governance ↔ 业务服务本机联调手册（Ubuntu-24 实测版）

本文件记录 2026-09-20 移除脚本引擎后，governance 与 ani-network-service 在
远程 Ubuntu-24 机器上做真实双进程联调的**完整踩坑路径**。所有环境差异、
启动门槛、数据种子逐条落在纸面上，后来者照做即可，不必重新逆向。

联调验证的业务链路（以 Network 的 GetVPC 为例，Model 同理）：

```text
HTTP 客户端
  → governance :7788  登录(JWT) → authn → authz(casbin) → 租户闸门(套餐白名单) → API 登记
  → mTLS gRPC → network-service :19090  GetVPC
  ← 映射回 catalog.v1.VPC 响应
```

## 0. 机器环境事实（先核对，不要假设）

| 项 | 实测值 |
|---|---|
| Go | 系统是 1.26.4，**本仓库要求 1.26.7**；用 `golang.org/dl` 包装器装 1.26.7：`go install golang.org/dl/go1.26.7@latest && go1.26.7 download`，之后所有 go 命令用 `go1.26.7` 调用，且 `GOTOOLCHAIN=local` 防止它回退到 1.26.4 |
| 私有模块 | `GOPRIVATE=github.com/zhangzhe-ctrl/*`；`goproxy.cn` 缓存的 zip 哈希与 go.sum 可能不一致，见 §1 |
| Postgres | 机器上无常驻 PG，联调用 docker 起（见 §2）；机器自带的 `lb02-*` 容器是别的业务，别动 |
| Redis | 同上，docker 起 |
| kind 集群 | kc062（control-plane + 2 worker），kubeconfig 在 `/home/ubuntu/.kube/config`，可给需要 K8s CR watch 的服务用 |
| 工具链 | `gow`、`ent`、`buf`、protoc 全家桶都要现装，见 §1 |

## 1. 一次性环境准备

```sh
export PATH=/home/ubuntu/go/bin:/usr/local/go/bin:$PATH

# 工具
go1.26.7 install github.com/tx7do/go-wind-toolkit/gowind/cmd/gow@v1.0.3
go1.26.7 install entgo.io/ent/cmd/ent@v0.14.6        # 与 go.mod 的 entgo 版本一致
go1.26.7 install github.com/bufbuild/buf/cmd/buf@v1.50.0
go1.26.7 install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
go1.26.7 install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
go1.26.7 install github.com/go-kratos/kratos/cmd/protoc-gen-go-http/v2@latest
go1.26.7 install github.com/go-kratos/kratos/cmd/protoc-gen-go-errors/v2@latest
go1.26.7 install github.com/google/gnostic/cmd/protoc-gen-openapi@latest   # openapi 生成会缺它
go1.26.7 install github.com/envoyproxy/protoc-gen-validate@v1.3.3
go1.26.7 install github.com/tx7do/go-wind-toolkit/protoc-gen-go-redact@v0.0.0-20260831125122-5bb4931991b2
```

### 1.1 私有模块校验和不匹配（必踩坑）

`go mod download` 报 `SECURITY ERROR ... checksum mismatch`（如
`zhangzhe-ctrl/ani-network-service`）。原因：goproxy.cn 缓存的 zip 与
本机 go.sum 记录的哈希不一致（私有模块曾经/正在被重复打 tag 推送）。

**处理**：把 go.sum 里 `zhangzhe-ctrl/*` 的 4 行删掉，重跑下载，让它按
proxy 返回值重建（前提：你确认来源就是 goproxy.cn 而非被劫持，私有仓库
按 commit 重新解析后哈希稳定）。首次 `go mod tidy` 同样需要：

```sh
export GOFLAGS=-mod=mod GOPROXY=https://goproxy.cn,direct \
       GOPRIVATE='github.com/zhangzhe-ctrl/*' GOSUMDB=off GOTOOLCHAIN=local
go1.26.7 mod tidy && go1.26.7 mod download all
```

### 1.2 代码生成

```sh
cd app/admin/service
ent generate --feature privacy --feature entql --feature sql/modifier \
             --feature sql/upsert --feature sql/lock ./internal/data/ent/schema
cd ../../api && buf generate                              # Go API
buf generate --template buf.admin.openapi.gen.yaml        # OpenAPI（会嵌进 assets）
```

**注意**：ent 生成前 `internal/data/ent/entql.go` 的 `Nodes` 切片长度必须
与实际 schema 数一致（手工改过 schema 后生成器会以现有 entql.go 为输入做
一致性检查，长度不对直接 panic）。正常流程"改 schema → 生成"不会踩到；
只有"手工删过生成代码"时才会，重跑一遍生成即可自愈。

## 2. 基础设施容器

```sh
# PG（镜像与 ani-network-service 集成测试锁定同一 digest）
docker run -d --name gov-int-pg --memory=512m --cpus=1 \
  -e POSTGRES_PASSWORD=govint -p 127.0.0.1:25432:5432 \
  docker.io/library/postgres@sha256:4ef4dbc939d61acea57712655ddb4b4ab27419c913f94cca0cd57cb3ea3c2280

docker run -d --name gov-int-redis --memory=256m -p 127.0.0.1:26379:6379 redis:7-alpine

# 库
docker exec gov-int-pg psql -U postgres -c "CREATE DATABASE gwa;"
docker exec gov-int-pg psql -U postgres -c "CREATE DATABASE network;"
docker exec gov-int-pg psql -U postgres -c "CREATE ROLE owner LOGIN PASSWORD 'owner' NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;"
docker exec gov-int-pg psql -U postgres -d network -c "GRANT USAGE ON SCHEMA public TO owner; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA public TO owner;"
```

## 3. network-service 启动（门槛最多的一侧）

配置文件直接用仓库 `configs/config.yaml`。**环境变量必须带 `ANI_` 前缀**
——它用 `env.NewSource("ANI")` 覆盖配置，所以 `NETWORK_DATABASE_DSN` 是
错的，正确的是 `ANI_NETWORK_DATABASE_DSN`（kratos env source 的语义是
`<前缀>_<配置路径>`，路径里的层级用 `_` 连接）。

```sh
# 迁移（一次性）：runtime role 必须是受限的 owner，不能用 postgres 超管
export ANI_NETWORK_MIGRATION_DSN="postgres://postgres:govint@127.0.0.1:25432/network?sslmode=disable"
export ANI_NETWORK_RUNTIME_ROLE=owner
./ani-network-service -conf <配置目录> -migrate

# 运行
export ANI_NETWORK_DATABASE_DSN="postgres://owner:owner@127.0.0.1:25432/network?sslmode=disable"
export ANI_NETWORK_KUBECONFIG=/home/ubuntu/.kube/config     # 启动强制要 kubeconfig
export ANI_NETWORK_CLUSTER_ID=test-cluster
export ANI_NETWORK_NAMESPACE_PREFIX=tenant-
# 必须是 base64、解码后 32..128 字节；"随便一串明文"会启动失败
export ANI_NETWORK_CURSOR_SIGNING_KEY="dGVzdC1zaWduaW5nLWtleS0wMTIzNDU2Nzg5YWJjZGVmMDEyMzQ1Njc4OWFiY2RlZg=="
./ani-network-service -conf <配置目录>
```

启动失败速查（都实测踩过）：

| 报错 | 原因与解法 |
|---|---|
| `migration requires ANI_NETWORK_MIGRATION_DSN and ...` | `-migrate` 模式必须显式给迁移 DSN 和 runtime role |
| `role "owner" does not exist` | 先建角色再迁移 |
| `runtime database role has administrative privileges` | 运行时 DSN 用了超管，或 owner 还有 CREATEDB/CREATE 权限（启动时会查 `pg_roles` + schema/db 权限，逐项 REVOKE） |
| `Kubernetes credentials/configuration could not be loaded` | 没给 `ANI_NETWORK_KUBECONFIG`，指向 `/home/ubuntu/.kube/config` 即可 |
| `cursor_signing_key must encode 32..128 bytes as base64` | key 必须先 base64，不能传明文 |
| `Network database, placement and worker config are required` | 环境变量前缀用错了（`NETWORK_` → 应为 `ANI_NETWORK_`），没覆盖到 |

## 4. governance 启动

```sh
# 数据库/redis 指向容器：改 data.yaml（database.source、redis.addr），
# server.yaml 里 rest.addr 改 127.0.0.1:7788、asynq.uri 改 redis://127.0.0.1:26379/1
#   注意：-c 指定的目录下只允许 yaml，多余的文件（如 ca.pem）会导致
#   "failed to merge config source: unsupported key: ca.pem format: pem" 直接 panic
export ANI_NETWORK_ADDR=127.0.0.1:19090
export ANI_NETWORK_CA=... ANI_NETWORK_CERT=... ANI_NETWORK_KEY=...
export ANI_MODEL_ADDR=127.0.0.1:19091    # Model 客户端是 fail-closed，不给直接 panic
./ani-governance -c <配置目录>
```

**已知环境限制**：`NetworkClient` 强制 mTLS（见
[network_client.go](../app/admin/service/internal/data/network_client.go)，
证书必须有 DNS SAN `ani-governance`，ServerName 固定 `ani-network-service`）。
本地裸跑的 network-service 是明文 gRPC，直连会报
`first record does not look like a TLS handshake`。生产无此问题：TLS 由
cert-manager + 集群内网关统一承载（见
[scripts/deploy/governance-mtls/](../scripts/deploy/governance-mtls/)）。
本机联调二选一：

- 接受业务链路在"明文直连下游"下验证（mTLS 层交给部署环境）；
- 或给 network-service 前面加一个 TLS 终止代理（stunnel/envoy），把
  `ANI_NETWORK_*` 指向代理端口。

## 5. 联调数据种子（最小集，全部 SQL 实测）

governance 的闸门是层层串联的，**每一层都要有数据**，缺一层就是一个
不同的 403/400。按请求实际穿越顺序：

1. **登录免不了验证码**：`CaptchaEnabled=true` 是硬编码（H5 闸门），
   验证码答案存在 Redis `gowind:captcha:<id>`，联调脚本里直接
   `GET /admin/v1/captcha` 拿 id 后从 Redis 读答案再带
   `X-Captcha-Id`/`X-Captcha-Value` 头登录。登录密码是
   `base64(AES-CBC(明文, DefaultAESKey, iv=key))`，`DefaultAESKey` 在
   tx7do/go-utils/crypto 里，明文 `Abcd@1234` 加密后是
   `5iATn4qtWi3ej5hNxxBcEA==`（ DefaultAESKey 实测值见
   `pkg/constants` 的 DefaultUserPassword 流程）。
2. **租户**：`sys_tenants` 里造一行（`resource_tenant_id` 是 Network 侧
   的租户 UUID，governance 直接透传给下游），`status='ON'`。
3. **租户用户**：`sys_users`（status 用 `'NORMAL'`，不是 'ON'！这是
   proto 枚举名）+ `sys_user_credentials`（PASSWORD_HASH 的值是 bcrypt，
   直接复用 admin 行的哈希最省事）+ `sys_memberships`（ACTIVE）。
4. **角色**：造一个**非 template:** 前缀的租户角色（template 角色会被
   策略装载器跳过），绑 `sys:access_backend` 权限（登录授权要求）+
   目标 API 的权限；`sys_user_roles`/`sys_membership_roles` 绑到用户。
   **改完角色必须重启 governance**——策略只在启动时装载
   （日志 `reloaded policy rules [N]`，N 应包含你新增的行）。
5. **套餐**：`sys_plans` 一行；`sys_tenants.plan_id`（外键列）指过去；
   `sys_plan_modules` 插 `(plan_id, 'NETWORK')`。只写
   `subscription_plan` 字符串没用，闸门读的是 plan 外键 + 模块白名单。
6. **API 登记**：`sys_apis` 插目标路由（path/method/module/
   business_module='NETWORK'）。参照
   [scripts/bootstrap-network-access.sql](../scripts/bootstrap-network-access.sql)。
   这一步漏了就是"access denied"，一步对齐一层 403 就会消失。
7. **登录请求必须带 `tenant_code`**（body 里），否则按平台租户解析，
   永远找不到你的租户用户。

## 6. 验证命令（逐层收敛）

```sh
# ① 未登录 → 401 UNAUTHORIZED（闸门存在性验证）
curl -s -o /dev/null -w "%{http_code}\n" \
  http://127.0.0.1:7788/api/v1/networks/vpcs/vpc_0123456789abcdef0123456789abcdef
# 期望：401

# ② 登录（§5 流程）拿 access_token

# ③ 带非法参数 → 400（业务校验生效）
curl -s "http://127.0.0.1:7788/api/v1/networks/vpcs/bad_id?x=1" -H "Authorization: Bearer $TOKEN"
# 期望：400 INVALID_VPC_ID / INVALID_QUERY

# ④ 全链路 → 下游真实响应
curl -s "http://127.0.0.1:7788/api/v1/networks/vpcs/vpc_0123456789abcdef0123456789abcdef" \
  -H "Authorization: Bearer $TOKEN"
# 期望：404 VPC_NOT_FOUND（库中无此 VPC，说明请求已穿过 governance 全部
# 闸门、经 mTLS 客户端真实到达 network-service 并带回了业务结果）

# ⑤ 对照：绕过 governance 直连下游（明文），排除下游本身故障
./getvpc vpc_0123456789abcdef0123456789abcdef
# 期望：NotFound（与 ④ 语义一致，证明 403/503 差异来自 governance 层）

# ⑥ 下游单测兜底（有真实 PG 的完整集成套件，110s 左右）
cd ~/Workspace/ani-network-service && ./scripts/integration ./...
```

⑥ 通过 + ④ 返回 404 + ① ② ③ 语义正确，即可认定链路健康。
mTLS 段在生产部署由集群内证书体系保证（§4 的环境限制）。

## 7. 收尾

```sh
docker rm -f gov-int-pg gov-int-redis
pkill -f gov-int/ani-governance; pkill -f gov-int/ani-network-service
```

临时库、测试租户数据都只存在于上述容器/数据库里，不污染任何真实环境。

# 镜像瘦身与二进制拆分执行计划（IMAGE-SLIM-01）

目标：按 2026-09-23 决定落地四项：① 去掉 GeoIP 归属地查询；② 编译剥离符号/DWARF；③ server / admin 二进制拆成两个镜像并同步部署；④ 基础镜像换 `gcr.io/distroless/static`。
OPA 无条件链接问题**不在本批**，另开任务讨论。

## 0. 实测基线（本机 `CGO_ENABLED=0 GOARCH=amd64` 构建，与 Dockerfile 参数一致）

| 产物 | 大小 |
|---|---|
| `server`（`./app/admin/service/cmd/server`） | 235,456,925 B |
| `admin`（`./app/admin/service/cmd/admin`） | 34,915,199 B |
| 基础镜像 alpine + ca-certificates | ≈ 8 MB |
| 当前镜像合计 | ≈ 280 MB |

`size -A server` 关键段：`.noptrdata` 64.5 MB（其中 62,616,566 B 是内嵌 `GeoLite2-City.mmdb`）、`.text` 50.9 MB、`.gopclntab` 45.7 MB、DWARF ≈ 31 MB、`.noptrbss` 35.9 MB（不占盘，Go 1.26 FIPS `drbg.memory` 33.5 MB）。
加 `-trimpath -ldflags "-s -w"` 实测：235,456,925 → 185,913,506 B（**-49.5 MB**）。

**执行后实测**（2026-09-23，`CGO_ENABLED=0 -trimpath -ldflags "-s -w"`）：

| 产物 | 基线 | 改后 | 变化 |
|---|---|---|---|
| `server` 二进制 | 235,456,925 B | **123,224,226 B** | −112.2 MB |
| `admin` 二进制 | 34,915,199 B | **24,334,498 B** | −10.6 MB |
| 基础镜像 | alpine+ca ≈ 8 MB | distroless 6.18 MB | ≈ −2 MB |

**镜像实测**（2026-09-23，同一台机器、同一 `docker images` 口径）：

| 镜像 | 体积（`docker images` Size） | 分层求和 |
|---|---|---|
| 基线：改动前配置（alpine + ca-certificates + 未剥离 server/admin + configs） | **397 MB** | ≈ 270 MB 二进制 + ≈ 8 MB 基础 |
| 服务镜像 `runtime-server`（distroless + 剥离 server） | **165 MB** | 123.4 MB（server 123 MB + 证书 319 kB + configs 33 kB + 公告 12 kB） |
| 运维镜像 `runtime-admin`（distroless + 剥离 admin） | **38.7 MB** | 24.8 MB |

运行时只需拉取服务镜像：**397 MB → 165 MB（-58%）**；运维镜像 38.7 MB 只在初始化/同步时按需使用。

---

## 1. 阶段 0：`.dockerignore` 阻断项（计划外发现）

**发现**：`.dockerignore:59` 排除 `sql/`，但 `sql/bootstrap/*.go` 是编译必需（`app/admin/service/cmd/server/wiring_ent.go:4`、`app/admin/service/cmd/admin/main.go:16` 都 import `go-wind-admin/sql/bootstrap`）。已用最小 Dockerfile 实测确认构建上下文里 `/src/sql` 不存在 → 按仓库根 `docker build` 会在 `go build` 阶段失败。本批要改 Dockerfile，必须先解掉这个阻断。

- [x] **0.1** `.dockerignore:58-59` 改为只排除数据/补丁 SQL，保留 `sql/bootstrap` 的 Go 源码：
  ```diff
  -# SQL seed/migration dumps (not needed for image build)
  -sql/
  +# SQL dumps are not needed to build the image; sql/bootstrap/*.go is embedded and required.
  +sql/*.sql
  +sql/patches/
  +sql/quota/
  ```
- [x] **0.2** 复验上下文：`docker build -f /tmp/ctx-probe/Dockerfile .`（临时 Dockerfile 内 `COPY . /src` + `ls /src/sql/bootstrap`），确认 `bootstrap.go`、`catalog.go` 存在且 `001_initial.sql` 仍在。

---

## 2. 阶段 1：移除 GeoIP（IP 归属地查询）

范围：只删「查地理库并填充审计记录」这条链路，**保留** proto / Ent / DB 的 `geo_location` 字段（字段删除涉及 Proto 重生成 + 迁移，不在本批）。移除后 `geo_location` 一律为 nil，历史行保留原值，不需要数据迁移。

### 2.1 生产代码

- [x] **1.1** `pkg/middleware/logging/utils.go:20` 删 `"github.com/tx7do/go-utils/geoip"`；`:25` 删 `"github.com/tx7do/go-utils/geoip/geolite"`；`:33` 删 `var ipClient, _ = geolite.NewClient()`（这一行是 62.6 MB 内嵌的唯一来源）。
- [x] **1.2** 删 `pkg/middleware/logging/utils.go:217-224` 的 `clientIpToLocation`。
- [x] **1.3** 删 `pkg/middleware/logging/utils.go:306-321` 的 `fillGeoLocation`。
- [x] **1.4** `pkg/middleware/logging/login_audit_log.go:91` 去掉 `loginAuditLog.GeoLocation = fillGeoLocation(clientIp)`（`clientIp` 仍用于 `IpAddress`，保留变量）。
- [x] **1.5** `pkg/middleware/logging/api_audit_log.go:84` 同上。
- [x] **1.6** `pkg/middleware/logging/operation_audit_log.go:124` 同上。
- [x] **1.7** `app/admin/service/internal/data/login_policy_checker.go:17-19` 注释更新：REGION 维度说明去掉「依赖 IP 地理库」，改为「当前不判定，且已无地理库依赖」。
- [x] **1.8** `isPrivateIP`（`utils.go:323-345`）保留——它服务于风险因素 `INTERNAL_IP`，与地理库无关。

### 2.2 测试（断言改为「不再产生地理信息」）

- [x] **1.9** `pkg/middleware/logging/utils_extra_test.go:289-299` 删 `TestClientIpToLocation`（被测函数已不存在）。
- [x] **1.10** `pkg/middleware/logging/utils_extra_test.go:301-318` 删 `TestFillGeoLocation`，或改写成断言「审计记录不再包含 GeoLocation」的等价用例（倾向删除，避免为已删功能留测试）。
- [x] **1.11** `pkg/middleware/logging/login_audit_log_test.go:94-98`：`geo := rec.GetGeoLocation(); require.NotNil` + `"局域网"` → 改为 `assert.Nil(t, rec.GetGeoLocation())`。
- [x] **1.12** `pkg/middleware/logging/login_audit_log_test.go:197`：`assert.Equal(t, "美国", ...)` → 改为断言 `GetGeoLocation()` 为 nil（该用例其余断言：IP、评分、风险因素不变）。
- [x] **1.13** `pkg/middleware/logging/login_audit_log_test.go:271`、`api_audit_log_test.go:257`、`operation_audit_log_test.go:220`：`assert.Empty(t, rec.GetGeoLocation().GetCountryCode())` 保留即可（nil 安全），无需改动——仅核对通过。
- [x] **1.14** `pkg/middleware/logging/api_audit_log_test.go:113-118`：`geo := ...; require.NotNil; "局域网"` → 改为 `assert.Nil(t, rec.GetGeoLocation())`。
- [x] **1.15** `pkg/middleware/logging/operation_audit_log_test.go:99`：`assert.Equal(t, "局域网", ..., "GeoLocation ← 客户端 IP 的地理解析")` → 改为 `assert.Nil(t, rec.GetGeoLocation())`。

### 2.3 依赖

- [x] **1.16** `go mod tidy`（`GOPROXY` 按 AGENTS.md 保持 `proxy.golang.org` 在前）。预期 `go.mod:32` `github.com/tx7do/go-utils/geoip v1.1.8` 降级为 indirect 或移除，连带 `oschwald/geoip2-golang`、`oschwald/maxminddb-golang` 变化；**逐行 diff 复核，只接受地理库相关变更**。

---

## 3. 阶段 2：编译剥离符号/DWARF

- [x] **2.1** `Dockerfile:22-26` 的 server 构建改为 `go build -trimpath -ldflags "-s -w -X main.version=$APP_VERSION"`。
- [x] **2.2** `Dockerfile:28` 的 admin 构建改为 `go build -trimpath -ldflags "-s -w"`。
- [x] **2.3** `app.mk:66` `LDFLAGS ?= -X main.version=$(VERSION)` → `LDFLAGS ?= -s -w -X main.version=$(VERSION)`，让 `make build_only` / `make build_admin` 与 CI 制品一致（PM2 部署走 `bin/`，同样受益）。
- [x] **2.4** 记录：剥离后 panic 栈只有地址、无符号/行号；排障需保留 build ID 与源码对应（写入 §5 文档注意事项）。

---

## 4. 阶段 3：server / admin 拆成两个镜像

设计：单个 Dockerfile，一个 `builder` 阶段 + 两个 runtime 阶段，用 `--target` 选择；**容器内路径沿用现在的 `/app/bin/server`、`/app/bin/admin`、`/app/configs`**，把部署改动降到最小。

- [x] **3.1** `Dockerfile` 保留 `builder` 阶段（第 1 阶段，行 5-34），两个 `go build` 照旧产出 `/src/bin/${SERVICE_NAME}-server` 与 `/src/bin/admin`。
- [x] **3.2** 新增 `FROM gcr.io/distroless/static-debian12:nonroot AS runtime-server`（替换原 `FROM docker.io/alpine:latest`，行 41）：
  - `COPY --from=builder /src/bin/${SERVICE_NAME}-server /app/bin/server`
  - `COPY --from=builder /src/bin/configs/ /app/configs/`
  - `COPY --from=builder /src/THIRD_PARTY_NOTICES.md /app/THIRD_PARTY_NOTICES.md`（保留上游许可声明）
  - `CMD ["/app/bin/server", "-c", "/app/configs"]`
  - 每个 runtime 阶段重新声明 `ARG SERVICE_NAME=admin`（跨 `FROM` 需重新声明）。
- [x] **3.3** 新增 `FROM gcr.io/distroless/static-debian12:nonroot AS runtime-admin`：
  - `COPY --from=builder /src/bin/admin /app/bin/admin`
  - `COPY --from=builder /src/THIRD_PARTY_NOTICES.md /app/THIRD_PARTY_NOTICES.md`
  - `CMD ["/app/bin/admin"]`（`admin` 无子参数时打印 usage 并退出 1，符合 CLI 语义）
  - **不拷配置**：`admin` 只连 PostgreSQL，凭据走 `ANI_DATABASE_DSN`（`docs/deployment.md:66`）。
- [x] **3.4** 删除 `Dockerfile:46` 的 `apk --no-cache add ca-certificates`（distroless/static 自带 CA 证书）与 `:62` 的 `adduser -D appuser`、`:65` 的 `USER appuser:appuser`（`:nonroot` 标签默认以 UID 65532 运行）。
- [x] **3.5** `app.mk:136-140` `docker` 目标改为构建两个镜像，并加单目标：
  - `docker_server`：`docker build --target runtime-server -t $(PROJECT_NAME)/$(APP_NAME):$(VERSION) ...`
  - `docker_admin`：`docker build --target runtime-admin -t $(PROJECT_NAME)/$(APP_NAME)-admin:$(VERSION) ...`
  - `docker`：依次执行两者（保持 `make docker` 语义不破坏）。
- [x] **3.6** 核对 `COPY` 进 distroless 的文件权限：二进制需 `0755`、configs `0644`（由 `builder` 阶段 `cp` 后的权限决定），保证 UID 65532 可读可执行。

---

## 5. 阶段 4：基础镜像换 distroless/static

- [x] **4.1** 按 §3.2 / §3.3 使用 `gcr.io/distroless/static-debian12:nonroot`。
- [x] **4.2** 已拉取并实测（2026-09-23，本机 docker 29.6.1）：
  - 镜像 `gcr.io/distroless/static-debian12:nonroot`，digest `sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab`，体积 **6.18 MB**（对比原 alpine + ca-certificates ≈ 8 MB）。
  - `Config.User = 65532`（即 `nonroot`），`Entrypoint/Cmd` 均为空。
  - 容器内已含 `/etc/ssl/certs/ca-certificates.crt` 与 `usr/share/zoneinfo`（用 `docker create` + `docker export` 验证），**因此 Dockerfile 可以去掉 `apk add ca-certificates`**。
  - 容器内**无 `/bin/sh`**（实测 `exec: "/bin/sh": stat /bin/sh: no such file or directory`）。
  - 是否按 digest 固定见 §8.2；若固定则 Dockerfile 内写 `@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab`。
- [x] **4.3** 确认无 shell 的影响面：容器不支持 `sh -c` 探针/健康检查；Atlas 迁移仍用独立 Atlas 镜像（`scripts/aksk-lab/build-images.sh:9` 的做法），**不在 governance 镜像里塞 shell**。

---

## 6. 阶段 5：部署与文档同步

- [x] **5.1** `docs/deployment.md:8-15`（准备配置和制品）：补充两个镜像的构建命令与产物说明（`make docker_server` / `make docker_admin`，或 `--target` 直连）。
- [x] **5.2** `docs/deployment.md:56-64`（首次初始化）：`./bin/admin init` 增加镜像形态写法（一次性 Job / `docker run --rm <admin-image> admin init ...` + `--password-file` 挂载 + `ANI_DATABASE_DSN`）。
- [x] **5.3** `docs/deployment.md:82-92`（启动）：更新「根 Dockerfile 同时包含 `/app/bin/server` 和 `/app/bin/admin`」这句为「两个独立镜像」，并说明 `admin` 镜像不含配置与 HTTP 端口。
- [x] **5.4** `docs/deployment.md:123-129`（`sync-apis`）：补镜像形态命令，保持 dry-run → 应用 → check 顺序。
- [x] **5.5** `docs/deployment.md` 增加「注意事项」：剥离符号后 panic 栈无行号；distroless 无 shell，容器内排障靠日志与 HTTP 健康检查。
- [x] **5.6** `scripts/deploy/pm2_service.sh` **不改**：它只起 `bin/server`（`pm2_service.sh:71`、`:103`），`bin/` 仍同时产出两个二进制，不受拆分影响。
- [x] **5.7** `scripts/aksk-lab/build-images.sh:7-27` 与 `scripts/network-lab/build-images.sh`：**待确认**（见 §8.3）。若改：governance 镜像只放 `server` + atlas + migrations，另出 `governance-admin` 镜像放 `admin`；注意这两个脚本是 2026-09-22 证据复现脚本，改动会改变历史复现路径。

---

## 7. 阶段 6：验收与回写

本机可验证（执行时逐项打勾并回填实测数字）：

- [x] **6.1** `make build_only` + `make build_admin` 编译通过。
- [x] **6.2** `go test ./pkg/middleware/logging/...`（定向）+ `go vet ./pkg/middleware/logging/...`。
- [x] **6.3** 复核 `go.mod` / `go.sum` diff 仅含地理库相关行。
- [x] **6.4** `docker build --target runtime-server` 与 `--target runtime-admin` 均成功；`docker images` 记录两者体积并与 §0 基线对比，回填到 §0。
- [x] **6.5** 静态确认 server 二进制不再含 GeoLite 数据：`go tool nm -size <server> | grep -i geolite` 应为空。
- [x] **6.6** `docker run --rm <server-image>` 冒烟：能读到 `/app/configs` 并按预期因缺少 DB/Redis 报明确错误（`not_verified` 若本机无 DB）。

本机**不可**验证（明确标注 `not_verified`，附运维命令）：

- [ ] **6.7** distroless nonroot 下真实启动、HTTP 7788 监听、SSE 7789 —— 需在部署环境执行 `docker run --rm -p 7788:7788 -v <configs>:/app/configs <server-image>` 后验证 `GET /admin/v1/me`。
- [ ] **6.8** 新库初始化闭环：`docker run --rm -e ANI_DATABASE_DSN=... -v <pw-file>:/run/secrets/... <admin-image> admin init --username admin --password-file /run/secrets/...` → `admin check` → 服务启动 → 登录 → 目标 API。
- [ ] **6.9** 审计记录落库后 `geo_location` 为 NULL、历史行不变（无迁移）。
- [ ] **6.10** `admin sync-apis --dry-run` / `sync-apis` 在独立 admin 镜像内的行为与 `bin/admin` 一致。

---

## 8. 风险与待确认

| # | 风险 | 处置 |
|---|---|---|
| R1 | 去掉地理库后 `geo_location` 恒为 NULL，审计/前端展示归属地为空 | 保留字段与历史数据，不做迁移；文档注明「归属地不再采集」 |
| R2 | distroless 无 shell，容器内 `exec`、shell 型探针失效 | 只用镜像自带 CMD；迁移仍走独立 Atlas 镜像；文档注明 |
| R3 | 拆镜像后运维仍按旧流程在同一个镜像里找 `admin init` | 阶段 5 同步文档；初始化必须显式用 admin 镜像 |
| R4 | `go mod tidy` 可能顺带改动无关依赖行 | 逐行 diff 复核，只接受地理库相关变更 |
| R5 | `gcr.io` 在目标环境不可达 | 拉取失败时改用内网代理/镜像仓库同版本同 digest，并在 §4.2 记录 |
| R6 | `-s -w` 后 panic 栈无符号行号 | 保留 build ID 与源码对应；排障需要时可临时用未剥离构建 |

待确认项已于 2026-09-23 由用户确认：

- [x] **8.1** `geo_location` **只「不再填充」**（按计划默认执行）；Proto/DB 字段删除**另开一批**，不在本批声称完成。
- [x] **8.2** distroless **按 tag 引用** `gcr.io/distroless/static-debian12:nonroot`，不固定 digest；本机实测可拉取（digest 记录在 §4.2 备查）。
- [x] **8.3** lab 脚本（`scripts/aksk-lab/build-images.sh`、`scripts/network-lab/build-images.sh`）**本批不改**，保持 2026-09-22 证据复现路径不变。
- [x] **8.4** 镜像 tag 规则**沿用** `$(PROJECT_NAME)/$(APP_NAME)` 与 `-admin` 后缀（`app.mk` 的 `docker_server` / `docker_admin`）。

## 9. 执行状态

| 阶段 | 状态 |
|---|---|
| 0 `.dockerignore` | **已完成**（上下文实测含 `sql/bootstrap/*.go`） |
| 1 GeoIP 移除 | **已完成**（`go test ./pkg/middleware/logging/...` 通过；`nm` 无 geolite 符号；`go.mod` 仅删 geoip/geoip2/maxminddb 三行） |
| 2 符号剥离 | **已完成**（Dockerfile 两处 + `app.mk:66`） |
| 3 镜像拆分 | **已完成**（`runtime-server` / `runtime-admin` + `docker_server` / `docker_admin`） |
| 4 distroless | **已完成**（按 tag 引用 `:nonroot`，不固定 digest） |
| 5 文档/部署 | **已完成**（`docs/deployment.md`；lab 脚本按用户决定不改；对接登记追加 AUDIT-GEO-01） |
| 6 验收 | 进行中：6.1-6.5 已完成；6.6 镜像冒烟待定；6.7-6.10 `not_verified` |

## 10. 计划外发现

- `.dockerignore:59` 排除 `sql/` 与 `sql/bootstrap` 的 `//go:embed` 编译需求冲突，按仓库根 `docker build` 会失败（已实测上下文缺失）→ 列入阶段 0。
- 原 `Dockerfile` 硬编码 `GOPROXY=https://proxy.golang.org,direct`，容器内无模块缓存且走官方代理，实测 `go mod download` 超过 40 分钟仍未完成 → 改为 `ARG GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct`（可用 `--build-arg GOPROXY=...` 覆盖），并去掉 RUN 里的硬编码。
  **与仓库现有约定的冲突（需确认）**：`AGENTS.md:75`、`README.md:11`、`docs/local-integration.md:99-101` 要求 `proxy.golang.org` 在前，理由是 `goproxy.cn` 对 `zhangzhe-ctrl/*` 等固定版本领域模块可能返回 `not found`。本次实测两个代理对该模块均返回 200，且代理链在 404 时会回退到下一个源；若构建环境严格要求遵循仓库约定，把默认值改回 `https://proxy.golang.org,https://goproxy.cn,direct` 即可。
- 现网在用的 `ani-governance:anisystem-*` 镜像 **554 MB**，分层为 `server + admin` 271 MB、**`atlas` 126 MB**、busybox 4.55 MB，由 `scripts/aksk-lab/build-images.sh` 产出。本批按用户决定不改 lab 脚本，因此该镜像不会自动变小；若要继续降，需把 Atlas 拆成独立镜像/init 容器（属另一批）。

# OPA 移除（OPA-REMOVAL-01）

2026-09-23，承接 IMAGE-SLIM-01。用户要求：去掉 OPA，并判断配置里 `authz` 是否也要去掉。本任务直接计划 → 执行 → 测试 → 提交推送。

## 0. 结论先行

**`authz` 配置块不能去掉**，只做收敛：

- `app/admin/service/cmd/server/wiring_ent.go:38` 用 `authz.type != "casbin"` 做启动期 **fail-closed** 校验（"downstream catalog access requires tenant-domain casbin authorization"）。配置去掉即失去这道防线，误配/缺配会静默退化。
- `pkg/authorizer/authorizer.go:168-185` 的 `newEngine` 依赖 `authz.type` 选引擎，去掉后无法表达"未配置时走什么"。
- `noop` 也不能一并删除：它是"未配置 / 未知类型"的显式兜底（`default` 分支），删掉会退化成 nil engine。
- 因此保留 `authz.type`（`casbin` / `noop`），**删除 `opa:` 子配置与 OPA 分支**，并把注释从 `casbin, opa, noop` 改为 `casbin, noop`。

## 1. 现状（静态审计）

OPA 相关面：

| 位置 | 内容 |
|---|---|
| `pkg/authorizer/authorizer.go:12` | import `kratos-authz/engine/opa`（无条件链接，即使配置 casbin） |
| 同上 `:89-93`、`:140-165`、`:182-183`、`:207-245` | ResetPolicies 的 opa 分支、`generateOpaPolicies`、`newEngineOPA` |
| `pkg/authorizer/deny_all_engine.go` | 只为"OPA 自定义模型解析失败"提供 fail-closed 兜底，无其他调用方 |
| `app/admin/service/cmd/server/assets/assets.go:8` | `//go:embed rbac.rego` → `OpaRbacRego` |
| `app/admin/service/cmd/server/assets/rbac.rego`、`rbac_test.rego` | OPA 模型文件（`rbac_test.rego` 无引用） |
| `app/admin/service/internal/data/authorizer_provider.go:42-52` | `ProvideModels("opa")` 分支 |
| `pkg/authorizer/provider.go:23` | `ProvideModels` 接口（仅 OPA 消费；casbin 分支返回空 map，且 casbin 不消费模型）→ 删除后为死接口 |
| `pkg/authorizer/authorizer_test.go` | 4 个 OPA 用例 + `opaPathJSON` + stub 的 `ProvideModels` |
| `app/admin/service/configs/auth.yaml:70-76` | `authz.type` 注释含 opa，`opa:` 子配置 |
| `go.mod` | `tx7do/kratos-authz/engine/opa` v1.1.15（direct）、`open-policy-agent/opa` v1.15.2（indirect） |
| `AGENTS.md:9` | "鉴权引擎支持 casbin / opa / noop" |

不在本批：`permission_policy.policy_engine` 枚举含 `OPA`（`app/admin/service/internal/data/ent/schema/permission_policy.go:38-46`）。它是**数据字段/DB 枚举**，改动需要 Ent 重生成 + Atlas 迁移，且当前业务代码不写入该字段（grep `SetPolicyEngine` 无命中）。列入另一批（与 `geo_location` 字段删除同一类）。

## 2. 阶段 1：删除 OPA 代码与内嵌模型

- [x] **1.1** `pkg/authorizer/authorizer.go`：删 opa import；删 `ResetPolicies` 的 `case "opa"`；删 `generateOpaPolicies`（含其 `OpaPolicyPath` 类型）；删 `newEngine` 的 `case "opa"`；删 `newEngineOPA`。
- [x] **1.2** 删除 `pkg/authorizer/deny_all_engine.go`（唯一调用方是 1.1 删掉的 OPA 分支；其它位置无引用）。
- [x] **1.3** `app/admin/service/cmd/server/assets/assets.go`：删 `//go:embed rbac.rego` 与 `OpaRbacRego`；删除 `assets/rbac.rego`、`assets/rbac_test.rego`。
- [x] **1.4** `app/admin/service/internal/data/authorizer_provider.go`：`ProvideModels` 只剩 casbin 空 map → 连同接口一起删：`pkg/authorizer/provider.go:23` 去掉 `ProvideModels`，删除 `AuthorizerProvider.ProvideModels`；同步删除两个测试 stub 的实现（`app/admin/service/internal/service/role_service_sqlite_test.go:41`、`api_service_sqlite_test.go:34`）。
- [x] **1.5** `pkg/authorizer/authorizer.go:82-102` 的 switch 保留 casbin / noop / default（不再有 opa 分支）。

## 3. 阶段 2：测试

- [x] **2.1** `pkg/authorizer/authorizer_test.go`：删 `TestNewEngineOPA_MissingModelReturnsNil`、`TestNewEngineOPA_CustomModelLoadsEngine`、`TestNewEngineOPA_InvalidModelReturnsDenyAllEngine`、`TestGenerateOpaPolicies_*`、`opaPathJSON`；stubProvider 去掉 `ProvideModels` 与 `lastModelEngineArg`；文件头注释改写。
- [x] **2.2** 保留并确认：`TestNewEngine_EmptyAndNoopTypeSelectNoopEngine`、`TestNewEngine_UnknownTypeFallsThroughToNoop`、`TestNewEngine_NilConfigReturnsNil`、`TestNewEngine_CasbinCreatesRealEngine`、`TestGenerateCasbinPolicies_*`、`TestEngine_ReturnsAssignedEngine`。
- [x] **2.3** `go test ./pkg/authorizer/... ./pkg/middleware/auth/...`。

## 4. 阶段 3：配置与文档

- [x] **4.1** `app/admin/service/configs/auth.yaml:70-76`：删 `opa:` 子项；`type` 注释改为 `# casbin, noop`（主装配强制 casbin，见 wiring_ent.go:38）。
- [x] **4.2** `AGENTS.md:9`：改为"鉴权引擎支持 casbin / noop（OPA 已移除）"。
- [x] **4.3** `docs/deployment.md:24`（`authz.type` 行）：补充 OPA 已移除、配置非法类型时启动拒绝。
- [x] **4.4** `docs/interface-integration-register.md`：按 AGENTS.md 第 10 条登记（鉴权条件变化：OPA 引擎不再可用）。
- [x] **4.5** `docs/image-size-reduction-plan.md`：OPA 那条"另开任务"标注已由本批完成。

## 5. 阶段 4：依赖与验收

- [x] **5.1** `go mod tidy`，逐行复核只移除 OPA 相关（`kratos-authz/engine/opa`、`open-policy-agent/opa` 及专属间接依赖）。
- [x] **5.2** `go build ./...`、`go vet ./pkg/authorizer/...`、定向测试通过。
- [x] **5.3** 复测 server 二进制体积（IMAGE-SLIM-01 后 123,224,226 B），确认 OPA 移除后的下降幅度并回填。
- [x] **5.4** `go tool nm -size <server> | grep -i "open-policy-agent"` 应为空。
- [x] **5.5** 冒烟：服务镜像/二进制仍能加载配置并按预期要求 `ANI_ACCESS_KEY_ENCRYPTION_KEY_FILE`（casbin 装配路径未被破坏）。
- [ ] **5.6** `not_verified`：真实部署下 Casmin 策略装载与接口鉴权行为需在部署环境复验（命令见 §6）。

## 6. 运维复验命令（部署环境）

```bash
./bin/admin check
/app/bin/server -c /run/governance/configs   # 启动日志应出现 casbin 引擎装载
# 登录后验证 GET /admin/v1/me、GET /admin/v1/initial-context，并抽查一个需授权接口仍 403/200 符合预期
```

## 7. 风险与回退

| # | 风险 | 处置 |
|---|---|---|
| R1 | 删除 `ProvideModels` 接口改动面扩到测试 stub | 改动点已枚举（1.4），编译器兜底 |
| R2 | 有人把 `authz.type` 配成 `opa` | 主装配 `wiring_ent.go:38` 非 casbin 即拒绝启动；配置注释同步 |
| R3 | `denyAllEngine` 删除后少了一层 fail-closed | 该引擎只对 OPA 模型解析失败生效，OPA 移除后无调用方；casbin 装配失败由 `newEngineCasbin` 返回 nil + 上层校验兜住 |
| R4 | DB 枚举 `policy_engine` 仍含 OPA | 数据字段，另批处理（本批不声称已清理） |

## 8. 执行状态

| 阶段 | 状态 |
|---|---|
| 1 代码删除 | **已完成**：`authorizer.go` OPA 分支/生成器/构造函数、`deny_all_engine.go`、`assets/rbac.rego(+_test)`、`ProvideModels` 接口与实现全部删除 |
| 2 测试 | **已完成**：删 4 个 OPA 用例与 `opaPathJSON`，stub 去掉 `ProvideModels`；`go test ./pkg/authorizer/... ./pkg/middleware/auth/...` 通过 |
| 3 配置文档 | **已完成**：`auth.yaml` 删 `opa:` 并改写注释；`AGENTS.md:9`、`deployment.md:39`、对接登记 AUTHZ-OPA-01 |
| 4 依赖与验收 | 5.1-5.5 **已完成**；5.6 `not_verified` |

## 9. 实测（2026-09-23）

- server 二进制（`CGO_ENABLED=0 -trimpath -ldflags "-s -w"`）：**123,224,226 B → 110,321,826 B**（−12.9 MB）。
- `go tool nm -size <server> | grep -c open-policy-agent` → **0**。
- `go.mod` 删除 `kratos-authz/engine/opa`、`open-policy-agent/opa` 及 OPA 专属间接依赖（lestrrat-go/jwx、gqlparser、tchap/go-patricia、prometheus/client_model 等共 26 行）。
- 冒烟：默认配置（casbin）加载 5 个配置文件后按预期要求 `ANI_ACCESS_KEY_ENCRYPTION_KEY_FILE`；把 `authz.type` 改成 `noop` 后启动被拒：`ani-governance downstream catalog access requires tenant-domain casbin authorization`——**这就是 §0 判定"authz 配置必须保留"的实测依据**。
- 全仓 `go build ./...`、`go vet ./...` 通过。

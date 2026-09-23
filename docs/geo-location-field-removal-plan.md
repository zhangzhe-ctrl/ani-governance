# 字段清理：`geo_location` 删除 + `policy_engine` 去掉 OPA（FIELD-CLEANUP-01）

2026-09-23，承接 IMAGE-SLIM-01（停止采集）与 OPA-REMOVAL-01（去掉引擎）。本批把两处**数据字段**收敛掉。本任务直接计划 → 执行 → 测试 → 提交推送。

## 0. 背景与依据

- IMAGE-SLIM-01 已停止填充 `geo_location`（新记录为 NULL），但 Proto 消息、Ent 字段、DB 列都还在。
- OPA-REMOVAL-01 已移除 OPA 引擎，但 `sys_permission_policies.policy_engine` 枚举仍含 `OPA`。
- 两者都标过"另批"，本批执行。

## 1. 现状（静态审计）

| 面 | 位置 |
|---|---|
| Proto 消息 | `api/protos/audit/service/v1/geo_location.proto` |
| Proto 引用 | `api_audit_log.proto:15,66`、`operation_audit_log.proto:14,127`、`login_audit_log.proto:13,104`（均为 `optional GeoLocation geo_location`） |
| 生成码 | `api/gen/go/audit/service/v1/geo_location.pb.go`、`geo_location.pb.validate.go` 及各 audit log 消息字段 |
| Ent schema | `api_audit_log.go:52`、`data_access_audit_log.go:50`、`login_audit_log.go:50`、`operation_audit_log.go:126` 的 `field.JSON("geo_location", &auditV1.GeoLocation{})` |
| Ent 生成码 | `ent/mutation.go`、`*_create.go`、`*_update.go`、`entql.go`、`where.go` 等（由 `make ent` 重建，不手改） |
| DB 列 | 初始迁移 `migrations/20260921134442_initial.sql` 中 4 处 `"geo_location" jsonb NULL` + 列注释 |
| 业务代码 | `login_audit_log_repo.go:168`、`api_audit_log_repo.go:164`、`operation_audit_log_repo.go:167` 的 `SetGeoLocation(...)` |
| 测试 | `pkg/middleware/logging/{api,login,operation}_audit_log_test.go` 的 `GetGeoLocation` 断言 |
| 枚举 | `ent/schema/permission_policy.go:38-46`（`Cel/Casbin/Opa/Sql`）；DB 侧是 `character varying NOT NULL DEFAULT 'CASBIN'` + `idx_perm_policy_engine`，**没有 CHECK 约束** → 去掉枚举值不需要改列类型 |

关键判断：`policy_engine` 在 Postgres 里是 varchar 无 CHECK，删枚举值只是代码/生成码收敛，**不产生迁移**。

## 2. 阶段 1：Proto 与 OpenAPI

- [x] **2.1** 删 `api/protos/audit/service/v1/geo_location.proto`。
- [x] **2.2** 三个 audit log proto 删 `import "audit/service/v1/geo_location.proto";` 与 `optional GeoLocation geo_location = N [...]` 字段（保留字段号不复用）。
- [x] **2.3** `make api`（`cd api && buf generate`）重新生成 Go 代码；确认 `api/gen/go/audit/service/v1/geo_location*.pb.go` 被删除。
- [x] **2.4** `make openapi` 重新生成 `app/admin/service/cmd/server/assets/openapi.yaml`（它是随二进制发布的 API 目录来源，`admin sync-apis` 依赖它）。
- [x] **2.5** 逐文件复核 diff：只应出现 geo_location 相关删除；如出现无关漂移，用 `scripts/post-generate-clean.sh` 处理并记录。

## 3. 阶段 2：Ent schema 与迁移

- [x] **3.1** 删 4 处 `field.JSON("geo_location", &auditV1.GeoLocation{})...` 字段定义。
- [x] **3.2** `ent/schema/permission_policy.go` 的 `NamedValues` 去掉 `"Opa", "OPA",`（保留 Cel/Casbin/Sql）。
- [x] **3.3** `make ent`（ent generate + `go run ./cmd/schema > schema.sql`），确认 `PolicyEngineOpa` 常量、生成码中的 geo_location 全部消失。
- [x] **3.4** 生成迁移：`ANI_ATLAS_DEV_DSN` 指向**独立可清空的开发库**，跑 `./scripts/atlas.sh migrate diff drop_geo_location`；产出 `migrations/<时间戳>_drop_geo_location.sql`。
- [x] **3.5** 复核迁移内容：只应含 4 条 `ALTER TABLE ... DROP COLUMN "geo_location"`，不含 `policy_engine` 变更，不含 DROP TABLE。
- [x] **3.6** 在 dev 库验证迁移可执行：`./scripts/atlas.sh migrate apply`（对 dev 库）与 `migrate status` 干净。
- [x] **3.7** `atlas.sum` 随 diff 更新，一并提交。

## 4. 阶段 3：业务代码、测试与文档

- [x] **4.1** 删 `login_audit_log_repo.go:168`、`api_audit_log_repo.go:164`、`operation_audit_log_repo.go:167` 的 `SetGeoLocation(req.Data.GeoLocation)`。
- [x] **4.2** 删三个 middleware 测试里 `GetGeoLocation()` 的断言（字段已不存在）。
- [x] **4.3** `docs/deployment.md`：新增本批的升级说明（Atlas 迁移 → 重启 → 验收），并写明**数据影响**：历史审计行的 `geo_location` 列随 DROP 一并丢弃，需要留存先备份；`policy_engine` 只是代码枚举收敛，DB 侧无变更。
- [x] **4.4** `docs/interface-integration-register.md`：按 AGENTS.md 第 10 条登记（审计查询响应不再含 `geo_location`，属报文变更）。
- [x] **4.5** `docs/image-size-reduction-plan.md`、`docs/opa-removal-plan.md` 里"另批"的这两项标注为由本批完成。

## 5. 阶段 4：验收

- [x] **5.1** `go build ./...`、`go vet ./...`、`make build_only`、`make build_admin`。
- [x] **5.2** `go test ./pkg/middleware/logging/... ./app/admin/service/internal/data/...`（后者含 sqlite 用例，能覆盖 Ent 生成码）。
- [x] **5.3** 确认仓库里再无 `geo_location` / `GeoLocation` / `PolicyEngineOpa`（除历史迁移文件与证据目录）。
- [x] **5.4** dev 库迁移 apply 通过（见 3.6）。
- [ ] **5.5** `not_verified`：真实库执行迁移、回滚演练、审计查询接口在部署环境的表现（命令见 §6）。

## 6. 运维执行与回滚

```bash
./scripts/atlas.sh migrate status
./scripts/atlas.sh migrate apply --dry-run
./scripts/atlas.sh migrate apply        # 4 张表 DROP COLUMN geo_location
```

回滚：本迁移只删列，**列内数据不可由迁移恢复**。执行前必须备份目标库（`scripts/backup/pg_backup.sh`）；需要回滚时用备份恢复，不要尝试"加回空列"来假装没删过。

## 7. 风险

| # | 风险 | 处置 |
|---|---|---|
| R1 | DROP COLUMN 使历史审计归属地数据不可恢复 | 执行前备份；文档写明不可回滚到"有数据"状态 |
| R2 | 全量 `buf generate` / `ent generate` 带来无关 diff | 逐文件复核；必要时 `scripts/post-generate-clean.sh` |
| R3 | openapi.yaml 变化影响 `admin sync-apis` | 本批无路由增删，只删消息字段；升级后按文档先 `--dry-run` 预览 |
| R4 | dev 库用共享 Postgres 实例 | 只用独立新建的临时库，不用业务库；用完 drop |

## 8. 执行状态

| 阶段 | 状态 |
|---|---|
| 1 Proto/OpenAPI | **已完成**：proto 消息 + 3 处引用删除，`make api` / `make openapi` 重新生成；openapi.yaml 只少 37 行（全为 geo 相关） |
| 2 Ent/迁移 | **已完成**：4 处 schema 字段 + OPA 枚举值删除，`make ent` 重新生成；迁移 `20260923143455_drop_geo_location.sql`（手写 + `migrate hash` + 独立开发库 apply 验证） |
| 3 代码/文档 | **已完成**：3 处 `SetGeoLocation`、测试断言删除；deployment.md 升级与数据影响、对接登记 AUDIT-GEO-02 / PERM-ENGINE-01 |
| 4 验收 | 5.1-5.4 **已完成**；5.5 `not_verified` |

## 9. 执行注记与实测（2026-09-23）

**生成顺序有坑**：先删 Proto 会让 Ent 生成失败（`ent generate` 要编译包，而生成码仍引用 `auditpb.GeoLocation`）。正确顺序是：先改 Ent schema → `make ent`（此时 proto 还在）→ 再删 proto → `make api` / `make openapi`。

**生成噪声处理**：`make api` 会全量重生成，本机 `protoc-gen-go` 是 v1.36.12 而仓库既有产物是 v1.36.11，产生 120+ 个文件的版本头漂移。做法是 `go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11` 后重跑，再 `git checkout` 掉非 audit 目录的漂移文件，并删除 6 个仓库原本不存在的空壳 `*.pb.validate.go`（quota / quota_lab / i_quota）。最终 `api/` 的 diff 只剩本次改动。

**迁移为何手写**：`atlas migrate diff` 在本机失败于 dev 库规范化
`schema.sql:3298: pq: there is no unique constraint matching given keys for referenced table "sys_quota_operations"`——这是 quota 批次遗留的 schema 问题（与本次改动无关），会让**任何**后续 `migrate diff` 都做不了。因此本批改为手写 4 条 `DROP COLUMN` + `atlas migrate hash` 校验，并在独立开发库（kind 内新建的 `atlas_dev_geo`，用完已 drop）执行：既有 3 个迁移 apply 成功，新迁移 `migrate apply` 4 条语句通过，`migrate status` 到达 `20260923143455`，查询 `information_schema.columns` 确认 4 张表已无 `geo_location`。**建议另开一批修复 `sys_quota_operations` 的外键/唯一约束问题，恢复 `migrate diff` 可用。**

**验收结果**：`go build ./...`、`go vet ./...` 通过；`go test ./pkg/middleware/logging/... ./pkg/authorizer/... ./app/admin/service/internal/data/...` 全 ok；仓库内（除历史初始迁移）已无 `geo_location` / `GeoLocation` / `PolicyEngineOpa` 引用。

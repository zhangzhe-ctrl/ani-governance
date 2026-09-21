# governance 文件/存储能力下线计划

目标：从 governance 整体删除文件管理功能，存储能力后续由其他服务实现。

已确认的删除范围（2026-09-21）：

1. 头像与备份 —— 删
2. `Module_FILE` 枚举 —— 删
3. Api 表与权限 —— 删

行号已逐条核对。执行时请按阶段顺序推进，每完成一项勾选。

---

## 执行状态（2026-09-21 执行）

| 阶段 | 状态 | 说明 |
|---|---|---|
| 0 硬约束 | 已满足 | 0.1/0.4 由 proto 与 bootstrap SQL 同批删除满足 |
| 1 文件管理 | 已完成 | proto/gen/service/repo/ent 实体/wiring/路由 全删；openapi 重生成后路由消失 |
| 2 MinIO/oss/头像/备份 + avatar 列 | 代码已完成；DB 迁移 not_verified | pkg/oss 整包、备份、头像 RPC、ent avatar 字段全删；列 drop 待 Atlas |
| 3 `Module_FILE` 枚举 | 代码已完成；DB 数据清理 not_verified | 枚举列为 `character varying`，仅清数据行，无类型变更 |
| 6 bootstrap 种子 SQL | 已完成 | 20 个 file/avatar 元组 + FileManagement 菜单种子已删 |
| 4 Api 表清理 | 补丁已备，未执行（本机无 DB） | `sql/patches/20260921_drop_file_storage.sql` |
| 5 验收 | 编译 + 定向测试 PASS；运行时验收 not_verified | 见文末 |

---

## 阶段 0：三条硬约束（先记住，否则白干）

- [ ] **0.1** `SyncApis` 会复活路由：启动时自动从内嵌 `openapi.yaml` 播种（`internal/service/api_service.go:61`）。proto 与 openapi 必须同步删。
- [ ] **0.2** `SyncApis` **不会删除**已消失的路由（契约：保留 ID、启停状态和权限关联，删除的只提示复核）。Api 表残留必须手工清理。
- [ ] **0.3** 服务禁止启动迁移（`migrate: false`）。drop table 与枚举变更走 `scripts/atlas.sh` 单独执行。
- [ ] **0.4** **bootstrap 种子 SQL 与 openapi 必须原子同删**（2026-09-21 拉取 `bin/admin init` 后发现）：`sql/bootstrap/001_initial.sql` 经 `//go:embed` 打入 `bin/admin`，其内部 `required(method,path)` 冻结清单会在 `admin init` 时校验"全部 API 必须已种入 `sys_apis`"，缺一则 `RAISE ... rollback`。阶段 1.9 删 openapi 路由时，本 SQL 的冻结清单与菜单种子（阶段 6）必须同批删除，否则 `admin init` 直接回滚。`admin check`（bootstrap.go:82）自带清单仅 4 条核心路由、不含 file/avatar，不受影响。

---

## 阶段 1：文件管理（第一层）

`storageV1` 全仓仅 5 处引用，file 表无其他消费者，删除面干净。

- [ ] **1.1** 删源领域 proto：`api/protos/storage/service/v1/{file,file_transfer,file_error}.proto` 及 `api/gen/go/storage/**`
- [ ] **1.2** 删 BFF proto：`api/protos/admin/service/v1/{i_file,i_file_transfer}.proto` 及对应生成码（含 `i_file_http.pb.go`、`i_file_transfer_http.pb.go`）
- [ ] **1.3** 删 service：`internal/service/file_service.go`、`internal/service/file_transfer_service.go`
- [ ] **1.4** 删 repo 与测试：`internal/data/file_repo.go`、`internal/data/file_repo_sqlite_test.go`
- [ ] **1.5** 删 ent schema 与生成码：`internal/data/ent/schema/file.go` 及 `ent/file*.go`、`ent/file/` 整套
- [ ] **1.6** 删手写 HTTP 装配：`internal/server/i_file_transfer_http.pb.go`
- [ ] **1.7** 删 wiring：`cmd/server/wiring_ent.go:138`（fileRepo）、`:190-191`（fileService、fileTransferService）
- [ ] **1.8** 删路由注册：`internal/server/rest_server.go:247`、`rest_server.go:252`
- [ ] **1.9** 删内嵌 openapi 4 条路由：`cmd/server/assets/openapi.yaml:1350`（download）、`:1411`（upload）、`:1448`（files）、`:1541`（files/{id}）

---

## 阶段 2：MinIO / `pkg/oss`（第二层）

头像 + 备份 + 文件三者都删 → `oss.MinIOClient` 归零消费者，整包可删。

### 2.A 头像

- [ ] **2.1** 删 RPC：`internal/service/user_profile_service.go:133-155`（DeleteAvatar）、`:157-215`（UploadAvatar）
- [ ] **2.2** 删 proto：`api/protos/identity/service/v1/user_profile.proto` 的 `UploadAvatarRequest` / `UploadAvatarResponse`；`api/protos/admin/service/v1/i_user_profile.proto:36`（POST `/admin/v1/me/avatar`）、`:43`（DELETE `/admin/v1/me/avatar`）
- [ ] **2.3** 删测试：`internal/service/user_profile_service_sqlite_test.go:250-267` 及相关用例

### 2.B 备份

- [ ] **2.4** 删备份任务：`pkg/task/backup.go`、`pkg/task/backup_test.go`；`internal/service/task_service.go:45-46`（`backupBucket` 常量）、`:613-615`（上传调用）；`internal/server/asynq_server.go:38`（`BackupTaskType` 订阅）
- [ ] **2.5** 删备份 repo：`internal/data/backup_repo.go`、`internal/data/backup_repo_sqlite_test.go`；wiring 中的 `backupRepo`；`task_service.go` 构造参数与 `task_service_sqlite_test.go:164`

### 2.C 存储基础设施

- [ ] **2.6** 删 `pkg/oss` 整包：`minio.go`、`utils.go` 及全部测试（含 `minio_live_test.go`）
- [ ] **2.7** 删客户端构造与配置：`internal/data/data.go:43`（`NewMinIoClient`）；`cmd/server/wiring_ent.go:67`；`app/admin/service/configs/oss.yaml`
- [ ] **2.8** 修 wiring 入参：`wiring_ent.go:167`（UserProfileService 去 minioClient）、`:192`（TaskService 去 minioClient）

### 保留项（不要误删）

- `pkg/netutil` —— `authentication_service.go:610,975`、`access_key_service.go:166`、`mfa_service.go:275,296,309`、`session_meta.go:46,53` 仍在用
- `pkg/crypto` —— payload 加密在用
- `scripts/backup/pg_backup.sh` —— 数据库运维备份脚本，**不属于本次删除范围**

### 2.D 头像列与 ent 字段（删，已确认 2026-09-21）

用户已拍板：连 `sys_users.avatar` 列一并删除。该列在 `ent/schema/user.go:71-74` 有 `field.String("avatar")`，且与 Atlas 迁移 `migrations/20260921134442_initial.sql:2868` 对应；非生成代码里仅头像 RPC（`DeleteAvatar`/`UploadAvatar`，阶段 2.1 已删）读写它，`User` proto 的 `avatar` 字段（field 23）不在此列删除范围（见 2.D.4）。

- [ ] **2.9** 删 ent schema 字段：`internal/data/ent/schema/user.go:71-74` 的 `field.String("avatar").Comment("头像").Optional().Nillable()`。
- [ ] **2.10** 重生成 ent：`gow ent admin`，清除全部 `FieldAvatar` / `GetAvatar` / `SetAvatar` / `ClearAvatar` 生成引用（`ent/user/{where,create,update,mutation}.go`、`ent/migrate/schema.go` 等随更）。
- [ ] **2.11** 迁移 drop 列：新增 Atlas 迁移 `ALTER TABLE "sys_users" DROP COLUMN "avatar";`，经 `scripts/atlas.sh` 执行；回滚 = `ALTER TABLE "sys_users" ADD COLUMN "avatar" varchar NULL;`。已有库升级/恢复/验证方式单列说明。
- [ ] **2.12** 一致性：确认 `migrations/20260921134442_initial.sql:2868` 列定义随新迁移移除（或在新迁移 drop）；`sql/bootstrap/001_initial.sql` 经核查**无 avatar 列**（命中皆为 `/admin/v1/me/avatar` 路由种子），无需改。
- [ ] **2.13** 验收：`make build_only` 通过；重生成后全仓 grep `user.FieldAvatar` / `.GetAvatar()` 无残留（阶段 2.1/2.3 已删头像 RPC，不应再有引用）。

> ⚠️ **proto `User.avatar` 字段（field 23）保留**，不在此列删除范围。删 DB 列 + ent 字段后，该 proto 字段在所有 `GetUser`/`ListUser` 响应中恒为空，前端不再能拿到头像 URL——符合"删除头像功能"语义，且不破坏现有契约。若后续要彻底从 proto 移除（影响更大，需重生成 proto 与全部引用），作为独立后续项，不纳入本计划。

---

## 阶段 3：`Module_FILE` 枚举

- [ ] **3.1** 删 proto 枚举值：`api/protos/identity/service/v1/module.proto:8` 起的 `enum Module` 中 `Module_FILE`
- [ ] **3.2** 删 ent 枚举值：`internal/data/ent/schema/menu.go`、`ent/schema/plan_module.go`、`ent/schema/api.go`；生成码 `ent/menu/menu.go:176`、`ent/planmodule/planmodule.go:95`、`ent/api/api.go:124` 随重生成更新
- [ ] **3.3** 删映射：`internal/data/tenant_access_checker.go:156-157`、`:189-190`；`internal/data/plan_module_repo.go:341-342`；`pkg/constants/module_mapping.go:30-31`；`pkg/constants/default_data.go:322-323`
- [ ] **3.4** 删/改测试：`pkg/constants/component_module_test.go:36`、`module_mapping_test.go:37,38,79`、`internal/service/plan_module_service_sqlite_test.go:134,149`、`internal/data/plan_module_repo_sqlite_test.go:232`、`internal/data/tenant_access_checker_sqlite_test.go:115,147,191`
- [ ] **3.5** 迁移：drop 数据库枚举值；清理 `plan_module` 表 `module='FILE'` 行、`api` 表 `business_module='FILE'` 行
- [ ] **3.6** 删测试种子：`pkg/constants/default_data.go:832-838` 的 `FileManagement` 菜单（`Component: "app/system/file/index.vue"`、`Path: "files"`）确认存在，一并删；同文件 `:322-323` 的 `component→module` 映射（`app/file/*` → `Module_FILE`）随 3.3 删。注：`DefaultMenus` 仅被 `pkg/constants/component_module_test.go:60` 的 `TestDefaultMenusModuleBackfillInvariant` 不变式测试引用，**不是生产种子**；真正生效的菜单种子是 `sql/bootstrap/001_initial.sql:99`，归入阶段 6.1。

> ⚠️ 删枚举值会影响套餐白名单：任何把 FILE 列入白名单的套餐需同步改，否则 `admin_portal_service.go:226` 的 `ListModulesByPlanId` 拿不到有效模块集合 → 该租户菜单被整体清空。

---

## 阶段 4：Api 表与权限

顺序固定，**不能只依赖 SyncApis**。

- [ ] **4.1** 确认阶段 1-3 完成，并重新生成 `openapi.yaml`（4 条 file 路由已消失）
- [ ] **4.2** 重启服务，让 `SyncApis` 跑一遍，记录其删除提示
- [ ] **4.3** 手工清理 `api` 表：`path` 匹配 `/admin/v1/files%` 或 `/admin/v1/file/%` 的行
- [ ] **4.4** 手工清理 `permission_api` 关联行（权限 → 接口）
- [ ] **4.5** 清理 `api` 表其余 `business_module='FILE'` 行（若有）
- [ ] **4.6** 复查 `SyncApis` 不再提示这些路由

建议在维护窗口执行，先 `admin sync-apis --dry-run` 预览。

---

## 阶段 6：bootstrap 初始化种子 SQL（fresh init 必改）

新增的 `bin/admin init`（commit 2619764/39008e4）执行 `//go:embed` 的 `sql/bootstrap/001_initial.sql`；该 SQL 内含一份**冻结的 API 授权清单**，init 时先由 `SyncAPIs` 把 openapi 路由种入 `sys_apis`，再用 `IF EXISTS(...)` 校验清单全部存在，缺一则 `RAISE ... rollback`（见阶段 0.4）。因此**阶段 1.9 删 openapi 路由时，本文件必须同批删对应条目**。

`InitialSQL` 是数据种子非 schema 迁移（bootstrap.go:14）；改动对**已存在库不回溯**（init 幂等，已完成的库返回 `UNCHANGED`）。故本阶段只修 fresh init；存量库仍走阶段 4 手工清理 `sys_apis`/`sys_permission_apis`。

- [ ] **6.1** 删菜单种子：第 99 行 `FileManagement`（`app/system/file/index.vue`、`path:"files"`、`module:"SYSTEM"`、`name:"FileManagement"`）。
- [ ] **6.2** 删冻结校验清单（`required(method,path)` 块，第 124–321 行）中的 file/avatar 元组：`131`(DEL files/{id})、`135`(DEL me/avatar)、`173`(GET file/download)、`174`(GET files)、`175`(GET files/{id})、`243`(POST file/upload)、`244`(POST files)、`252`(POST me/avatar)、`300`(PUT file/upload)、`301`(PUT files/{id})。
- [ ] **6.3** 删 `sys_permission_apis` 授权块（第 322 行起）中的 file/avatar 元组：`331`(DEL files/{id})、`335`(DEL me/avatar)、`373`(GET file/download)、`374`(GET files)、`375`(GET files/{id})、`444`(POST files)、`452`(POST me/avatar)、`501`(PUT files/{id})、`524`(DEL me/avatar)、`541`(POST me/avatar)。
- [ ] **6.4** 重新编译 `bin/admin`（go:embed 重新打入 SQL）；对干净库跑真实 `admin init` 验证不回滚（无 `--dry-run`，不能只靠预检）。
- [ ] **6.5** 与阶段 1.9 同批提交：openapi 路由、proto、本 SQL 三者删除必须原子，避免任一先行导致 `admin init` 失败。

> ⚠️ 注：阶段 4.3 的清理 pattern 需同时覆盖复数 `/admin/v1/files%` 与单数 `/admin/v1/file/%`（含 `/file/download`、`/file/upload`），否则存量库 `sys_apis`/`sys_permission_apis` 残留。

---

## 阶段 5：验收

- [ ] **5.1** `make build_only` 编译通过
- [ ] **5.2** 定向跑受影响包测试：user_profile / task / plan_module / tenant_access_checker
- [ ] **5.3** 重启后 `/admin/v1/files*`、`/admin/v1/file/*` 返回 404
- [ ] **5.4** Api 表无对应残留行，`SyncApis` 无提示
- [ ] **5.5** 确认 `files` 表已 drop、枚举值已移除
- [ ] **5.6** 回滚方案：阶段 1-2 为代码删除（git 回滚）；阶段 3-4 涉及线上数据与枚举，需准备回滚迁移

---

## 待确认项（已清零）

1. ~~`sys_users.avatar` 列~~ —— **已确认删除（2026-09-21）**。新增阶段 2.D（2.9–2.13）：删 ent 字段 + Atlas drop 列 + 保留 proto `User.avatar` 字段（恒空）。
2. ~~阶段 3.6 菜单种子~~ —— 已核查：确认存在 `FileManagement` 菜单（`default_data.go:832-838`），阶段 3.6 改为"确认删除"。
3. ~~阶段 2.5 的 `backupRepo` 是否共用~~ —— 已核查：不共用（仅备份导出），阶段 2.5 可安全删，风险表中对应项已关闭。

---

## 已知风险

| 风险 | 说明 |
|---|---|
| 套餐白名单联动 | 阶段 3 删枚举后，白名单含 FILE 的租户可能整体空菜单（P0 坑，见 `docs/onboarding-invite-audit.md`） |
| 权限残留静默 | 阶段 4 的 `permission_api` 残留不会报错，但会让权限点指向不存在的接口 |
| 备份 repo 复用 | 已核查：`backupRepo` 仅用于备份导出（`ExportCoreTables`），Task 的 List/Get 走 `taskRepo`，不共用。阶段 2.5 删除安全，本风险关闭 |
| openapi 未同步 | 阶段 1.9 漏做 → `SyncApis` 把已删路由重新播种，产生永不打通的僵尸条目 |
| bootstrap 冻结清单不同步 | **新发现（2026-09-21）**：阶段 1.9 删 openapi 但漏改 `sql/bootstrap/001_initial.sql` 的 `required` 冻结清单（第 124–321 行）→ `admin init` 校验缺条目直接 `RAISE rollback`，全新库无法初始化。已补阶段 6，要求与 1.9 原子同删 |
| 存量库不回溯 | `InitialSQL` 改动只影响 fresh init；已存在库仍依赖阶段 4 手工清理 `sys_apis`/`sys_permission_apis`，不能假设改了 SQL 即生效 |

---

## 执行记录（2026-09-21）

### 已落地改动
- **删除**：`api/protos/storage/**`、`api/protos/admin/service/v1/i_file*.proto` 及生成码；`pkg/oss/**`（含测试、live 测试）；`pkg/task/backup{,_test}.go`；`internal/data/{file_repo,backup_repo}*.go`；`internal/data/ent/schema/file.go` 及 `ent/file*` 生成文件；`internal/server/i_file_transfer_http.pb.go`；`internal/service/{file,file_transfer}_service.go`；`configs/oss.yaml`。
- **修改**：`wiring_ent.go`、`rest_server.go`、`asynq_server.go`、`task_service.go`、`user_profile_service.go`、`data.go`、`tenant_usage_repo.go`、`user_repo.go`、`module_mapping.go`、`default_data.go`、`tenant_access_checker.go`、`plan_module_repo.go`；ent schema `user/menu/plan_module/api`；`module.proto`、`user.proto`、`i_user_profile.proto`；`openapi.yaml`（重生成，-516 行）；`sql/bootstrap/001_initial.sql`；`go.mod`/`go.sum`（去 minio 依赖）。
- **新增**：`sql/patches/20260921_drop_file_storage.sql`（存量库数据清理，幂等、原子）。

### 计划外发现（均已处理）
1. **`tenant_usage_repo.go` 额外消费 File 实体**（计划未列）：租户存储用量统计 `File.Query().Sum(FieldSize)` 与租户清理 `tx.File.Delete()`。已改为存储用量恒 0、移除 File 删除；其 SQLite 测试改用用户行验证"跨租户保留"。
2. **`user_repo.go` 两处 `SetNillableAvatar`**（Create/Update 路径）—— 计划 2.D 隐含，已删。
3. **计划 2.2 文件指向有误**：`UploadAvatarRequest/Response` 实际在 `api/protos/identity/service/v1/user.proto`，不在 `user_profile.proto`。
4. **`TestServiceTagToBusinessModuleReverseMapping` 是拉取代码中的既有失败**（反向期望表漏 `MfaService`/`PlanModuleService`），非本次引入；已补正使其与实际映射一致。
5. **生成器副作用**：`buf generate` 会为 network/catalog 产出两个 origin 未跟踪的 `*.validate.go`；经确认非必需，已移除。`protoc-gen-go` 本地 v1.36.12 高于提交产物 v1.36.11，重生成产生 ~98 个纯版本注释 diff —— 已还原这些仅版本变化的文件以聚焦改动；20 个 `*_http.pb.go` 的 handler 重编号是删 proto 的连带结果，保留。

### not_verified（本机无 atlas / 无 DB / 无运行实例）
- 阶段 2.11 / 3.5：schema 迁移（`DROP TABLE "files"`；`ALTER TABLE "sys_users" DROP COLUMN "avatar"`）未执行。
- 阶段 4：`sys_apis` / `sys_permission_apis` / `sys_menus` / `sys_plan_modules` 清理未执行（补丁已备）。
- 阶段 5.3 / 5.4 / 5.5：运行时 `/admin/v1/files*` 与 `/admin/v1/file/*` 返回 404、`SyncApis` 无提示、库结构确认未执行。
- 阶段 6.4：`admin init` 对干净库不回滚未验证。
- 邮件邀请 / 平台管理员开通链路（`docs/onboarding-invite-audit.md`）不在本次删除范围。

### 运维执行命令（在具备 atlas / PostgreSQL 的环境）
```bash
# 1) schema 迁移（Atlas；会同时生成迁移文件并更新 atlas.sum）
./scripts/atlas.sh migrate diff drop_file_storage --env governance
./scripts/atlas.sh migrate apply --env governance

# 2) 存量库数据清理（幂等，可重复执行）
psql -v ON_ERROR_STOP=1 -f sql/patches/20260921_drop_file_storage.sql

# 3) 重新编译 admin（go:embed 打入更新后的种子 SQL）
make build_admin

# 4) 对干净库验证初始化不回滚
ANI_DATABASE_DSN='postgres://...' ./bin/admin init --username admin --password-file /path/to/pw
```

---

## 说明

本计划基于静态源码审计编写，未做运行时验证。执行中若发现行号漂移或遗漏依赖，请以实际代码为准并回写本文件。

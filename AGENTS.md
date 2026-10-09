# AGENTS.md — ANI Governance 执行规则

本仓独立维护 ANI 治理后端。开发步骤以 [docs/development.md](docs/development.md) 为入口；当前任务的明确授权决定修改和执行范围。来源与许可见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

## 身份、布局与工作方法

根目录是 Go 模块根，模块名暂为 `go-wind-admin`，服务身份为 `ani-governance`。模块名及所有技术引用必须一起保留，直到单独的改名任务。不要为品牌整理修改 import、go_package、Proto 包名、运行标识或本地包目录。

Go 基准来自 `go.mod`。`gow` 是本仓维护副本，来源 v1.0.3 不等于应安装上游二进制。用 `make gow` 构建，使用本仓的 api/ent/run/version；不运行未接管的 Wire、project/new、migrate 等脚手架命令。工具版本以 Makefile、活跃模板和工具锁为准，不用 latest 补空缺。

读取顺序：当前任务 → 本文件 → 相关合同与真实源码 → 对应开发指南章节。保留无关工作区修改；一次任务不加载全部历史迁移回执。未执行的事情明确写未执行，不能以文件存在、静态检查或 HTTP 200 代替验收。

主装配为 Kratos + Ent，当前数据主路径为 PostgreSQL；GORM 备用装配、Zanzibar 占位、OPA 已移除。库内可能存在 noop 类型，但主服务配置必须是 Casbin，不能为让启动成功降级。生成、构建、运行、数据库和部署分别遵守本次授权，未授权的主机安装/全局配置/业务库操作不做。

## 不可省略的实现合同

1. 不手改 `api/gen/go/` 和 Ent 生成实现；Ent schema 输入在 `app/admin/service/internal/data/ent/schema/`。Ent 目录也有手写守卫和测试，不能整目录当生成物删除。`wiring_ent.go` 手写，不使用 Wire。
2. 源领域 Proto 定义消息与 RPC，不带 HTTP 注解；admin BFF 引用消息并声明路由。沿用已接受路径，`/admin/v1` 不是所有 ANI 新接口的强制前缀。
3. CRUD 请求保持 `{ data: {...} }`，部分更新用 `update_mask`。Service 校验 nil 请求/数据，通过 `auth.FromContext` 获取可信操作人；不信任入站租户与操作人字段。
4. 文本搜索使用 contains，ID 不进模糊搜索。精确 ID 查询保留明确标识条件；分页、过滤按当前 `pagination.PagingRequest` 合同。
5. 不吞错，使用相应 Proto 错误码并保留原始上下文。日志不泄露密码、密钥、令牌或跨租户私有信息。
6. 本地 `go-crud/entgo` Repository 泛型次序为 Query、Select、Create、CreateBulk、Update、UpdateOne、Delete、Predicate、DTO、Entity，共 10 项；以当前 `api_repo.go` 为范例，不按上游最新版猜测。
7. DTO 映射注册 `copierutil.NewTimeStringConverterPair()` 与 `NewTimeTimestamppbConverterPair()`；枚举注册相应 `NewConverterPair()`。`ListWithPaging` 同时传 builder 和 Clone。
8. FieldMask 与关联更新参照 `RoleRepo.Update`：未请求修改和显式清空不同，按关联字段是否在 mask 中决定更新；关联 mask 从通用标量 mask 分离，主表/关联在同一事务，验证清空、重加和跨租户拒绝。
9. `tenant_id` 不等于隔离。读取、写入、删除、关联、异步、缓存、消息和下游请求均检查租户边界；关系约束与负向测试不能省。
10. `wiring_ent.go` 按基础设施 → Repo → 认证授权 → Service → Server 单向装配，只构造与传参。持有 cleanup 的资源成功后立即登记，失败和退出逆序释放；`make register` 不代替额外依赖的手工装配。

功能对接和接口风格持续登记在 [docs/interface-integration-register.md](docs/interface-integration-register.md)。每次新接入先追加接口、报文、鉴权条件和问题，复用已有编号；只在用户指定批次/格式后统一调整风格，不能因排查就改接口。即时修复单独记录；整体登记只在用户确认后结项。

## 数据与生成边界

`data.database.migrate` 必须 false。结构迁移通过 `migrations/` 和 Atlas 显式执行；初始化用 `bin/admin init` 与 `sql/bootstrap/001_initial.sql`，不把 `pkg/constants/default_data.go` 旧测试数据当部署种子。不得重复首次播种覆盖已有业务数据。

API 目录先 `admin sync-apis --dry-run` 再显式同步，保留 ID、启停状态和权限关系。新增 Proto 不等于 API 已登记，目录同步不等于自动授权；新增 `(path, method)` 需权限和套餐关系，租户闸门仍 fail-closed。CLI/SQL 修改后的策略刷新按部署说明执行。

`make api` / 本地 `gow api` 是完整 API 入口；不在正式输出根裸跑 Buf，再靠 `git checkout` 或删除多余生成物凑结果。保留开始快照、暂存产物、写回前工作树的保护；本地 redact 从当前源码重建，PATH 工具检查身份，不只看存在。

`migration/` 不等于数据库 `migrations/`。前者的 PGV 范围清单、工具锁和活跃测试清单仍被使用，不整目录搬删；历史记录、许可与来源归档不改写。改接管源码按最小补丁与定向回归维护，不每次重做 T00～T15。

## 验证与交付

按风险选择编译、定向测试、工具集成和真实链路；工具命令从根目录运行。Make 委托调整先跑 `make check-repo-entrypoints`，但命令桩不代替真实生成/构建。现有 `make test` 不准备依赖资源，父包测试不自动覆盖子包 Ent 守卫。

数据库测试只用明确授权的独占库，helper 可能建表和 TRUNCATE。使用真实退出码和具名结果；缓存是缓存，skip 是未执行，不写成新鲜通过。失败保留，不重试至绿、不扩 known-defects、不删断言、不全局关闭 vet。

当前 R6 历史样本恢复按所有者裁决延期、未验证，不再寻找已清理实验文件。运行、部署与跨仓联调另验。缺陷及范围外事项保留，不用无关优化拖延已限定任务。

只提交本次授权文件；文档更新不自动授权部署、修改密码、操作业务库或改变仓库可见性。推送/PR/合并各按明确授权处理，CI 状态如实记录，不为回填自身 SHA 反复追加台账提交。

## 测试、自动化与文件放置

遵循 [仓库维护规范](docs/contributing/repository-hygiene.md)，不在此重复完整文档。

1. Go 用例与同包 helper 放 `*_test.go`；跨包共享设施仅在 `app/admin/service/tests/testutil`，正式依赖图不得引用。
2. 不在生产包新增 `ForTest` 构造器、测试控制路由或平行模拟资源系统；测试调用真实构造与行为。
3. CI 实现放 `scripts/ci`，实验放 `scripts/experiments/<topic>` 并显式指定环境；业务断言留在语言测试。
4. 临时探针、日志、源码包、回执和截图不得随业务提交；只保留必要合同、决策与来源索引。
5. 删除测试需说明被测对象与保留或退役边界；不能以缺环境 skip、扩大豁免或反复重试冒充通过。
6. 生成配置、许可、正式业务与数据库迁移不能按历史目录名误删；清理仅处理本 run 自有资源，不改模块名或共享环境。

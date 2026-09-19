# AGENTS.md — ANI Governance 后端开发指南

本仓库独立维护 ANI 后端，不再跟随或合并 go-wind-admin 主线。来源与许可见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

## 范围与布局

- 仓库根目录就是 Go 模块根目录，模块路径保持 `go-wind-admin`，运行服务名为 `ani-governance`。
- Go 工具链基准为 **1.26.7**，gow 固定 **v1.0.3**；命令从仓库根目录执行。
- 主装配为 Kratos + Ent，当前 ANI 集成使用 PostgreSQL。`gorm_backend` 是未完成的备用装配，不能作为已支持的部署方式。
- 现有登录、身份、权限与套餐代码按已接受的 ANI 合同演进；目录调整不代表域职责迁移或生产切换完成。

```text
api/protos/                         手写源领域 Proto 与 HTTP BFF
api/gen/go/                         生成的 Go API
app/admin/service/cmd/server/        入口、手写 wiring_*.go、嵌入 OpenAPI
app/admin/service/configs/           配置样例
app/admin/service/internal/data/     Repository、领域客户端、Ent schema/生成代码
app/admin/service/internal/service/  业务服务
app/admin/service/internal/server/   HTTP、Asynq、SSE 装配
pkg/                                认证、授权、租户、审计、脚本等实现
tools/register/                     标准 CRUD 装配登记工具
scripts/                            生成、部署、备份及定向验收
third_party/tx7do/                   依赖源码备份区域
```

## 工作流

先读相关合同与真实源码，保留无关工作区改动。涉及多仓库时先确认 owner 和调用合同，不把下游领域业务复制成本仓写入职责。

依赖消费、生成、构建、测试和运行的位置遵守当前任务要求；若指定远程执行，本地仅编辑与提交。不要为只读检查安装依赖或启动服务。

安装固定工具：`go install github.com/tx7do/go-wind-toolkit/gowind/cmd/gow@v1.0.3`。运行与生成优先 gow，未覆盖的任务使用根 Makefile：

| 任务 | 根目录命令 |
|---|---|
| 运行 | `gow run admin` |
| Ent 生成 | `gow ent admin` |
| Proto / Go API | `gow api` |
| OpenAPI | `make openapi` |
| 标准 CRUD 登记 | `make register ENTITY=product` |
| 仅编译已有源码 | `make build_only` |
| 编译并生成 API/OpenAPI | `make build` |
| 全量测试（任务需要时） | `make test` |
| 静态检查 | `make lint` |

`make build` 会生成文件。其他生成工具以 Makefile 和专项脚本的实际版本为准；发布复现需记录工具版本，不能仅凭 go.mod 固定就声称整个生成链已锁定。Model/Network 的 `scripts/generate-*-slice.sh` 按各自版本、范围与执行环境使用。

验证对应改动风险：受影响入口编译、定向测试、必要的真实链路检查。隔离、鉴权、持久化、故障恢复不能由 HTTP 200 或静态检查代替；未执行的验收写 `not_verified`。文档和目录调整不自动要求全量测试。

## 实现规则

1. 不手改 `api/gen/go/` 和 Ent 生成实现；`app/admin/service/internal/data/ent/schema/` 是可编辑输入。`wiring_ent.go` 手写维护，不使用 Wire、不运行 `gow wire`。
2. 源领域 Proto 定义消息与 RPC，不带 HTTP 注解；admin BFF 引用消息并声明路由。路径遵守已接受合同，`/admin/v1` 不是 ANI 新接口的强制前缀。
3. CRUD 请求保持 `{ data: {...} }`，部分更新使用 `update_mask`。Service 校验 nil 请求/数据，通过 `auth.FromContext` 取得可信操作人；不信任请求传来的租户和操作人身份。
4. 文本搜索使用 contains，ID 不进入模糊搜索。精确 ID 读取仍使用明确标识条件；分页和过滤按当前 `pagination.PagingRequest` 合同。
5. 不吞错；使用相应 Proto 错误码，保留原始错误上下文，日志不得泄露密钥、令牌或跨租户私有信息。
6. `go-crud/entgo` 的 Repository 泛型顺序为 Query、Select、Create、CreateBulk、Update、UpdateOne、Delete、Predicate、DTO、Entity，共 **10 个参数**。按当前 `api_repo.go` 实现复制。
7. DTO 映射注册 `copierutil.NewTimeStringConverterPair()` 与 `NewTimeTimestamppbConverterPair()`；枚举注册对应 `NewConverterPair()`。`ListWithPaging` 同时传 builder 和 Clone。
8. FieldMask 与关联表更新参照当前 [RoleRepo.Update](app/admin/service/internal/data/role_repo.go)：区分未请求修改关联与显式清空关联，按关联字段是否出现在 mask 中决定是否更新；关联字段从通用标量更新 mask 中分离。主表与关联修改保持同一事务，验证清空、重加和跨租户拒绝。
9. `tenant_id` 字段不等于隔离。新资源检查读取、写入、关联、异步任务、缓存和消息路径；租户关系还需租户一致性约束与负向验证。
10. `wiring_ent.go` 按基础设施 → Repo → 认证授权 → Service → Server 单向装配；资源创建后登记 cleanup，失败和退出逆序释放。`make register` 仅覆盖标准构造函数，额外依赖手工补齐。

## 数据与运行边界

配置样例位于 `app/admin/service/configs/`，HTTP 默认 `7788`，可选 SSE `7789`。按环境配置数据库、Redis、密钥、领域地址和 mTLS 材料，不能把开发样例当生产参数。

开发配置 `migrate: true` 会在启动时执行 Ent `Schema.Create`，不是生产升级/回滚证据。schema 改动要说明已有数据库的升级、恢复和验证方式。

系统默认数据来自 `pkg/constants/default_data.go` 与各 Service 的启动守卫；多处只在表为空时播种。`sql/` 是演示数据；`scripts/bootstrap-*-access.sql` 是专项接入脚本，按前提使用。

已有 Api 表不会因新增 Proto 自动补齐。新增接口明确登记 `(path, method)`、权限和套餐关系，否则租户闸门 fail-closed。`SyncApis` 清空后全量重建，不是无损增量登记；需保留已有 ID 与关联并验证升级授权链。

Go 依赖仍由 `go.mod` / `go.sum` 管理。固定版本的 Model/Network API 模块需要匹配的既有模块交付与 file-GOPROXY 配置；先核对交付及消费方式。`third_party/tx7do/` 备份的版本、覆盖范围与校验信息以实际清单为准；源码备份不自动切换模块解析，也不等于已完成离线构建。依赖升级与漏洞修复由本仓独立维护。

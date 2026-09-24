# GOV-ACC-V12-01：BFF、身份与权限证据

本文件记录暂停前的执行结果；恢复后的增量证据见 [resume/README.md](resume/README.md)。以下当时的 `not_verified` 保留历史语义，不能用后续结果改写原始记录。

执行环境为 `ssh fedora`，独立任务 `gov-acc-v12-01-20260923/gov-bff`；Go `1.26.7`，`GOWORK=off GOMAXPROCS=2 GOFLAGS=-p=2`，独立缓存与 `gov-bff.lock`。消费正式上游模块 `github.com/zhangzhe-ctrl/ani-accelerator-service v0.0.0-20260923100416-9f9712198488`，无 replace、file proxy、上游 internal 导入或复制 Proto。

## 已实际验证

- `bff-joint-content-final.log`：20 个真实 HTTP BFF（12 管理、7 租户、1 自助额度）通过。生产 JWT 校验、Redis session/token cache、PostgreSQL tenant/module checker、持久角色权限 provider、Casbin middleware、生成 HTTP 路由、BFF 与真实 Accelerator mTLS helper 均进入链路。JWT session 为测试发行；不声称密码登录端点通过。
- 对应 20 个委托 RPC：12 个 Admin RPC，ListTenantProfiles、GetProfile、ResolveGpuRequest、CheckFit、GetCapacity、ListGpuUsages、GetGpuUsage、ListGpuBindings。Preview 实际执行 Resolve + CheckFit；自助额度由 Governance 权威账户读取。第 21 个 SyncGpuUsage 由独立主链测试验证，不由 HTTP 代理；ObserveRelease 没有 Governance BFF。
- 管理写以原 actor 与原始幂等键重放已由真实 Admin RPC 落库的 5 个请求；既有写入首次执行由 Accelerator seed 完成。逐路内容断言包括对象 ID、profile 版本、OPEN 状态、device/node、admin binding 中真实物理/Pod/证据字段；同 key 异内容为 IDEMPOTENCY_CONFLICT 409，旧版本新 key 为 VERSION_CONFLICT 409。两页真实 usage 无重复，重复第一页稳定；跨 tenant、filter、action 与篡改 cursor 拒绝；page_size=200 成功、201→400。不把这些断言声称为所有 actor/grant cursor 变体已验证。
- 使用主链原 CREATE 引用查询 `ENDED` usage，并返回仍然非空的实际 live binding。字段断言排除物理设备 ID、Pod UID/namespace、证据路径、baseline digest、snapshot 与 tenant ID；另有非空结构映射单元测试。
- 无/坏 JWT→401，租户访问管理面→403，平台 tenant 0 访问租户面→403，另一真实 tenant UUID 无下游精确 grant→403，另一租户查询原 CREATE ref/bindings→404，未知 profile/ref→404。每个请求均注入伪造公网身份 header，BFF 从可信 Principal 与持久 UUID 映射重建委托。
- 角色权限 OFF、效果 DENY、角色 OFF、权限 OFF、API OFF 在刷新持久策略后均→403；套餐 ACCELERATOR 模块删除后立即→403，TENANT 模块自助额度仍→200。恢复权限和模块后再次可用。
- 空生产 registry 的预览返回 `NOT_ENABLED`；CPU/network/storage `NOT_CHECKED`，没有生产 owner/action 注册，不声称生产执行 ENFORCED。
- Preview 真实 Resolve/Fit/账户读取返回 12288 MiB，前后 operation、charge、account 三表全部租户行的规范摘要不变。满容量/UNKNOWN 的两种 BFF 预览分支尚待配合主链的新容量 fixture 验证。
- `bff-auth-network-regression-02.log`：既有 auth middleware、authorizer、constants、Permission/RolePermission/TenantAccess、Authentication、Network client/service 与新增 Accelerator tests 通过。此常规测试中的外部集成环境缺失会 skip；真实 BFF 的独立运行如上，不用 skip 充当通过。
- `bff-server-build.log`：正式 server composition root 与 transport package 编译通过。
- `formal-gov-process.json/log`：随后实际运行普通 Governance 二进制（非 quota_lab、非测试二进制），独立受限 PG 库、真实 Redis/JWT，正式配额目录两 GPU code 均 NOT_ENABLED，自助账户 tenant=1；quota_lab/fault/control 路径 404，SIGTERM 退出码 0。此轮未连接普通 Accelerator A 进程，完整 A→A 联调仍 not_verified。
- `bff-generation-drift.log` 与 `bff-generated-before.sha256`：固定 Buf 1.60.0 定向再生成，7 个目标产物逐字一致；Buf 二进制 SHA256 为 `27c8272bff9403ed04bb90875f9d831facf11778c5f9994046dd2cae55fb05d1`。
- `bff-lab-regression-02.log`：8 个 `TestSimPG_*` 在独立 `gov_acc_lab` 与每用例独立 owner/provider PG 库通过。包括证书负向、失 ACK 幂等、provider commit 后恢复、部分创建/清理、部分退款/通知重试、旧创建重放、永久合同错误解除阻断与并发删除。运行身份逐例断言非 superuser、非数据库 owner、无 CREATE DATABASE/ROLE、无 schema CREATE/TEMP；fixture 管理身份只显式建库、迁移、授予 DML、销毁测试库。独立 quota_lab CA 不复用 Accelerator CA。

## 失败与修复保留

- `bff-joint-attempt-02.log`：真实 role_permission OFF 后仍为 HTTP 200。修复策略加载仅消费启用 role/API/permission 与 ON+ALLOW 关联；上面的真实撤销矩阵和非 GPU 回归覆盖修复。
- `bff-joint-final.log`：跨租户缺失 ledger ref 透出 Ent not-found 500。原 ref resolver 修复为受控 QuotaErrNotFound；最终返回 404。
- `bff-auth-network-regression.log`：非 GPU SQLite fixture 错误创建含 PostgreSQL 专有约束的新 GPU 表。fixture 已排除两张 PG 专有表，生产约束没有修改；GPU 表仍由真实 PG 独立验收。
- `bff-lab-regression-01.log`：历史模拟器测试固定 ResourceTenantID 不等于新权威租户映射，且未知原 operation 旧断言为 NotFound。现测试从真实持久 tenant UUID 贯穿 ledger/owner；未知原 operation 被 owner-scoped locator 统一拒绝 403。未放宽生产账本身份校验。

## 重放入口

在 Fedora 已显式准备独立 PG/Redis/Acc helper 后，以任务私有 `GOMODCACHE`、`GOCACHE` 与 `gov-bff.lock` 运行 `GOV_ACC_TASK_ROOT=... bash scripts/accelerator-acceptance/bff-joint.sh`。脚本要求真实原 CREATE/ref、非空绑定与独立 fixture admin DSN，缺依赖即失败。

quota_lab 用 `prepare-lab-certs.sh <taskroot>/task/lab` 显式生成独立证书；`lab-regression.sh` 读取该目录 0600 `gov-dsn` / `admin-dsn`。模拟器 `OpenOwner/OpenProvider` 不再隐式建表，DDL 保存在 `simulator/testdata/*-schema.sql`；旧 `scripts/quota-lab/run.py migrate` 已追加显式迁移。这些脚本不代表生产部署验收。

## 边界

软件联调使用隔离 PostgreSQL、Redis、真实 mTLS 与持久化测试 owner/observer；`hardware_observed=false`。真实 GPU、Volcano 调度、HAMi/Volcano 分配、生产 owner/action、密码登录全流程及生产部署不属于本文件 PASS。没有新增万能业务 create、没有公开 Sync/ObserveRelease、没有默认向角色/套餐发放 Accelerator 权限。

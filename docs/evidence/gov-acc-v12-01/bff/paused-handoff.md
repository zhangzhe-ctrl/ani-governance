# BFF 子任务安全暂停交接

用户要求暂停后未启动后续 A 联调、verify、生成或新测试；没有提交或推送。

## 已结束命令及退出码

工作目录均为 Fedora `/home/chabking/gov-acc-v12-01-20260923/gov-bff`，固定 `GOWORK=off GOMAXPROCS=2 GOFLAGS=-p=2`，独占 `cache/gov-bff-mod`、`cache/gov-bff-build` 与 `locks/gov-bff.lock`。命令退出码来自本轮工具执行回执，不将未记录的开始时间补写为猜测值；正式进程 JSON 自带真实开始/结束 UTC。

| 命令 | exit | 证据及范围 |
|---|---:|---|
| `GOV_ACC_TASK_ROOT=… bash scripts/accelerator-acceptance/bff-joint.sh` | 0 | `bff-joint-content-final.log`，B 层真实 HTTP/PG/Redis/Acc mTLS，20 BFF、内容/权限/快照/分页/脱敏 |
| `go test ./pkg/middleware/auth ./pkg/authorizer ./pkg/constants ./app/admin/service/internal/data ./app/admin/service/internal/service -run 'Test(Network\|Permission\|RolePermission\|TenantAccess\|ModuleMapping\|ServiceTag\|Server_\|Auth\|Composed\|Option\|SetRequest\|Signature\|VPCSignature\|Principal\|SignedHTTP\|RoleRejection\|NewEngine\|Engine_\|Generate\|ResetPolicies\|NewAuthorizer\|Accelerator)' -count=1`（正则使用分支竖线，不含 Markdown 转义符） | 0 | `bff-auth-network-regression-02.log`，C 层定向回归，外部 BFF 测试单独执行如上 |
| `go test ./app/admin/service/cmd/server ./app/admin/service/internal/server -run '^$'` | 0 | `bff-server-build.log`，编译证据 |
| `GOV_ACC_TASK_ROOT=… bash scripts/accelerator-acceptance/lab-regression.sh` | 0 | `bff-lab-regression-02.log`，8 个真实 PG/mTLS 旧 lab 测试、非 owner 无 DDL/TEMP 运行角色 |
| `BUF=…/gov-bff-tools/buf bash scripts/generate-accelerator-slice.sh` + `sha256sum -c …/evidence/bff-generated-before.sha256` | 0 | `bff-generation-drift.log`，7 产物无差异 |
| `go build -o …/task/formal-gov/ani-governance ./app/admin/service/cmd/server` | 0 | 随后该普通二进制真实运行，非 build tag/helper |
| `python3 scripts/accelerator-acceptance/formal-gov.py /home/chabking/gov-acc-v12-01-20260923` | 0 | `formal-gov-process.json/log`，A 层 Gov 本侧，process exit=0；无 Acc A 下游链 |

测试时 API 固定模块为 `v0.0.0-20260923100416-9f9712198488`；本分支仍为未提交工作树。本文件不认证随后 Accelerator 新候选模块，也不替代协调者的最终两仓 source manifest/exact-SHA 门禁。

## 矩阵组件状态

这些是本子任务负责的组件断言；跨组件 ID 的整项结论必须与协调者和 Acc 证据合并。

| ID | 本侧 status | 具体证据／尚缺条件 |
|---|---|---|
| BASE-03 | pass | 正常固定正式模块获取、无 replace/internal/复制 Proto；最终升级候选需重验 |
| BASE-04 | pass | BFF 固定生成无差异、正式 server 编译、普通 Gov 实际进程、quota_lab 测试编译运行 |
| AUTH-01 | pass | C 层真实 TCP mTLS 客户端错 CA/DNS/URI/缺证书负向 + B 层实际 Acc 委托正链；Acc 接收端负向另见其矩阵 |
| AUTH-02 | pass | 真 JWT/cache/Principal、持久 tenant UUID、外部伪造 header、平台 tenant 0 及跨租户拒绝 |
| AUTH-03 | not_verified | 已逐个正向调用 20 委托 RPC，并实测另一 tenant 缺 grant 拒绝；全部 action/grant 负向和 Sync 撤权独立性须合并 Acc/主链证据 |
| AUTH-04 | not_verified | BFF原 CREATE/usage/binding 缺失和不可见统一404；DELETE/FK/扫描仍属主链/data |
| AUTH-05 | pass | 实际 module入库/API同步dry-run事务回滚/权限登记/无授权角色拒绝/五类撤销刷新/模块关闭 |
| AUTH-06 | not_verified | lab无证书、错CA、错owner DNS拒绝通过；同owner/多SAN完整矩阵由主链负责 |
| AUTH-07 | pass | BFF仅user Principal、APIKey拒绝，既有Network/签名Key/Auth定向回归通过；非密码登录证明 |
| API-01 | pass | 12管理路由实际内容断言、原幂等写重放、409幂等与版本冲突 |
| API-02 | not_verified | 全部租户路由实际内容与原ref读取通过；未知owner filter负向有上游合同代码，暂停前未补独立HTTP断言 |
| API-03 | not_verified | 真Resolve/Fit/账户、12288MiB、三表全行摘要不变、NOT_ENABLED通过；FULL/UNKNOWN BFF分支未跑 |
| API-04 | pass | SaveObservation非空原ref绑定、ENDED仍非空；admin可见敏感字段，租户DTO去除；无硬件实采宣称 |
| API-05 | not_verified | page1/200/201、两页稳定、跨tenant/filter/action与篡改cursor通过；独立actor/grant-scope游标变体需Acc证据合并 |
| API-06 | pass | 实际400/401/403/404/409，C层全部合同status/reason映射与原始敏感错误抑制；传输错误真实TLS负向 |
| API-07 | not_verified | 自助Principal tenant1忽略恶意tenant_id=9102；降额over_limit/available和旧admin账户权限由data/主链补证 |
| BOUND-01 | pass | 普通Gov真实进程目录NOT_ENABLED、lab/fault/control404；正式注册表无owner |
| BOUND-03 | pass | B helper实际mTLS与非空软件观察，來源清晰；完整manifest仍由协调者冻结 |
| BOUND-04 | pass | lab8真实运行 + 非GPU Network/鉴权回归，模拟器仅tag构建 |
| SQLC-05 | not_verified | 本侧lab8/Network/auth回归共用新账本已通过；旧fixture升级/回滚/dump恢复由data证据负责 |
| OWNER/LIVE/DEPLOY | not_verified | 无真实业务owner、硬件与生产发布；保持固定边界 |

## 暂停状态与恢复输入

- 本子任务所有 test/go build/formal Gov 进程均已结束；普通 Gov 最后一次 SIGTERM exit=0。未启动 Acc A 长驻或第二次 A→A 联调。
- 自有容器 `gov-acc-v12-01-bff-redis` 已先执行 `redis-cli SAVE` 返回 OK，再 `podman stop --time 10`；inspect 状态为 `exited`、running=false。容器未删除，RDB 留在其可写层，以保留测试 session。
- 任务 PG 容器与 Acc B 服务归协调者/数据代理处理，本子任务未停止或删除共享任务资源。
- Fedora `task/formal-gov/`：普通二进制、0600 runtime/admin DSN、0600 session.json/JWT材料、config、加密key和私有原始日志均保留。它们不复制到本地，不进入Git。
- Fedora `task/lab/`：受限Gov DSN、仅fixture管理员DSN、独立CA/三方证书；每用例临时owner/provider DB与角色由测试cleanup结束，lab Gov库保留。
- Fedora `joint-b/`：原CREATE/ref、Acc证书/seed/helper、Gov/owner原始链由协调者保留。不能把A接到该B helper后宣称正式A。

用户恢复授权后，先启动自有 Redis 容器 `podman start gov-acc-v12-01-bff-redis`，由协调者恢复对应 PG/Acc B 输入；再按上表独占锁重跑必要 BFF。A完整链待 Acc agent提供普通bin的独立 `ready.json` 后运行 `python3 scripts/accelerator-acceptance/formal-gov.py <taskroot> <formal-acc-ready.json>`。必须先固定最终候选 module并同步最新源码，不能复用旧模块PASS认证新版本。

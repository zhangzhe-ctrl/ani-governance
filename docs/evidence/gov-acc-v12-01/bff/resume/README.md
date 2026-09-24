# BFF 恢复后的组件验收

本目录保留 2026-09-24 恢复授权后的实际执行证据，不覆盖暂停前日志和失败记录。执行位置仍为 Fedora `gov-acc-v12-01-20260923/gov-bff`，任务私有 Go cache 与 `gov-bff.lock`，`GOWORK=off GOMAXPROCS=2 GOFLAGS=-p=2`。本轮消费 Accelerator 正式模块 `v0.0.0-20260923100416-9f9712198488`；该轮不能认证后续候选版本。

## 已结束命令

| 命令 | exit | 实际边界 |
|---|---:|---|
| `GOV_ACC_TASK_ROOT=… bash scripts/accelerator-acceptance/lab-regression.sh` | 0 | `lab.log`、`lab.started`、`lab.finished`、`lab.exit`。8 个真实 PG/mTLS 模拟器回归；增加同 owner 多 DNS SAN 成功、跨 owner 多 SAN 拒绝，拒绝后占用保持 1、合法原 ref 退款后占用为 0。使用独立 CA、显式建库迁移、受限运行角色。 |
| `go test -c -o …/gov-bff-service.test ./app/admin/service/internal/service` | 0 | `bff-build.log`。新增 HTTP 断言编译通过；此项本身不是接口运行证据。 |
| `python3 scripts/accelerator-acceptance/formal-gov.py <taskroot> <taskroot>/joint-a/formal-resume/ready.json` | 0 | `formal-gov-acc-process.json` 与 `formal-gov-process.log`，普通 Gov → 普通 Acc 真实进程链。 |
| `GOV_ACC_BFF_EXPECT_FIT=WAIT_FOR_CAPACITY bash scripts/accelerator-acceptance/bff-joint.sh` | 0 | `bff-WAIT_FOR_CAPACITY.json/log`。主链真实满容量窗口调用，全 20 BFF 与新增边界断言通过。 |
| `GOV_ACC_BFF_EXPECT_FIT=FIT_UNKNOWN bash scripts/accelerator-acceptance/bff-joint.sh` | 0 | `bff-FIT_UNKNOWN.json/log`。主链真实 freshness 超时窗口调用，同一完整 BFF 断言通过。 |
| 测试 SQL 集中后 `lab-regression.sh` 和 `GOV_ACC_BFF_EXPECT_FIT=FIT_UNKNOWN bff-joint.sh` | 0 / 0 | `sql-extraction-{lab,bff}.log/exit`，分别 7.728s / 0.834s；真实开始/结束见 `sql-extraction.started/finished`。 |

正式 A 链实际使用独立受限 PostgreSQL、真实 Redis/JWT、合法 CA/DNS/唯一 Governance URI 证书。Gov 正式目录两 GPU code 均为 `NOT_ENABLED`，自助额度的可信 tenant 为 1，lab/fault/control 路径均 404；真实 mTLS 管理 ListClusters 成功，PublishProfile 被 `CONSUMPTION_NOT_VERIFIED` 412 拒绝，profile 数为 0，整个供给组前后相同且 `CLOSED/NOT_VERIFIED`。JSON 保存开始/结束时间及两个普通二进制 SHA256，Gov SIGTERM 退出码 0。测试 session 是显式 fixture 发行，不声称登录端点验收；没有硬件采集或生产 owner。

## 组件状态

| ID | status | 恢复后证据与范围 |
|---|---|---|
| AUTH-06 | pass | `lab.log` 的真实证书负向与同 owner / 跨 owner 多 SAN 断言；对应生产 owner 解析路径，测试 owner 仅在测试装配注册。 |
| BOUND-01 | pass | 普通 Gov + 普通 Acc + 真实 PG 的 A 链与无测试路由；正式 owner readiness 为 NOT_ENABLED。 |
| BOUND-04 | pass | 8 个旧 lab 回归使用新账本真实通过；暂停前非 GPU Network/鉴权回归仍保留独立来源。 |
| API-01 | pass | 两容量窗口各完整重跑 12 管理 BFF；逐路内容、原幂等键重放和 409 冲突断言。 |
| API-02 | pass | 全租户 HTTP 路由、原 CREATE/ref、可信 tenant；新增未知 owner filter → 400、未知 owner detail → 404。 |
| API-03 | pass | 两真实容量窗口分别 WAIT_FOR_CAPACITY / FIT_UNKNOWN，额度检查 sufficient=true；12288 MiB、非法 replicas=17 → 400、三表摘要不变、NOT_ENABLED 和三个 NOT_CHECKED。满容量事实和 freshness 由主链构造，非 BFF mock。 |
| API-04 | pass | 原 CREATE 的 ENDED usage 仍有非空绑定；admin 物理字段可见、租户 DTO 脱敏。 |
| API-05 | pass within BFF scope | page 1/200/201、两页无重复与稳定第一页、跨 tenant/actor 请求拒绝、filter/action/tamper 拒绝。另一 actor 未被授予下游 grant，HTTP 403 可能先在委托层拒绝；精确 cursor actor/cluster/action/method 绑定由 Acc 单元与 PG 组件提供。 |
| API-07 | pass within BFF scope | 自助接口忽略请求 tenant_id=9102，读取 Principal tenant1；旧 admin quota 接口平台授权 → 200，账户与自助完全相同；即使给 tenant API 权限仍被平台身份 guard → 404，无 JWT → 401。降额 limit4/occupied6/available0/over_limit=true 的真实投影由 data `resume/plan-compatibility-05.log` 单独证明。 |

AUTH-03 的全部 20 动作缺 grant 与 API-05 的完整游标作用域负向由 Accelerator `docs/evidence/gov-acc-v12-01/acc/candidate-v7/verify.log` 提供，其组件汇总为相邻 `acc/resume-results.md`。额度降额视图由 Governance data 组件提供。整项结论必须合并各来源，不能把其中一侧单独扩展为全链 PASS。后续最终模块与 exact-SHA 复验记录将单独补充。

SQL 源现集中于 BFF `testdata/accelerator_ledger_snapshot.sql` 与 lab `testdata/integration-queries.sql`；只读摘要用单条 SQL 获取同一语句快照，lab 账本查询使用 tenant 参数。随机 scratch DB/role 的标识经 pgx Identifier 引用后仅替换测试 DDL 模板，运行服务不执行建库或迁移。

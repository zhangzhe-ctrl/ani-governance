# 发布模块 1d32dd9 的 BFF 组件复验

执行窗口为 `started` 至 `finished`（2026-09-24 03:11:46Z–03:14:31Z），Fedora `gov-bff` 独占副本、缓存及锁，`GOWORK=off GOMAXPROCS=2 GOFLAGS=-p=2`。`module.json` 记录通过 `https://proxy.golang.org,direct` 正常获取的 `v0.0.0-20260924030150-1d32dd9a9173`，来源 Git SHA 为 `1d32dd9a9173b8869fa0ef2ae64e20b88f2ca0a3`；`module-verify.log` 为全部模块校验通过。没有兄弟 replace、复制 Proto 或 file proxy。

| 命令 | exit | 证据与实际范围 |
|---|---:|---|
| `go test -tags gpu_joint -c … ./app/admin/service/tests/gpucontract` | 0 | `policy-compile.log/exit`，包含修复后 policy 精确 PermissionDenied + DELEGATION_DENIED 消息断言。编译不证明场景运行。 |
| `GOV_ACC_FORMAL_SESSION_FILE=… GOV_ACC_JOINT_REDIS_ADDR=127.0.0.1:19382 go test ./app/admin/service/internal/service -run '^TestAcceleratorFormalSessionRefresh$' -count=1 -v` | 0 | `formal-session-refresh.log`，显式测试 session 刷新；没有密码登录声明。 |
| `go build -o …/task/formal-gov/ani-governance ./app/admin/service/cmd/server`，随后 `python3 scripts/accelerator-acceptance/formal-gov.py <taskroot> <taskroot>/joint-a/formal-resume/ready.json` | 0 | `formal-build.log`、`formal.exit`、`formal-gov-acc-process.json/log`，真实普通 Gov→普通 Acc A 链、独立受限 PG、真实 Redis/mTLS。 |
| `GOV_ACC_TASK_ROOT=… bash scripts/accelerator-acceptance/lab-regression.sh` | 0 | `lab.log/exit`，8 个真实 PG/mTLS 旧 lab 回归，10.836 秒，包括同 owner / 跨 owner 多 SAN。 |
| `go test -c … ./app/admin/service/internal/service` | 0 | `service-compile.log/exit`，最终容量窗口 BFF 测试预编译。 |

A 链在 03:13:51Z–03:13:52Z 实际验证：两 GPU code 都为 NOT_ENABLED；自助可信 tenant=1；lab/fault/control 路径为 404；真实 mTLS 管理读取成功；Publish 到达消费证明门槛并返回 CONSUMPTION_NOT_VERIFIED 412，profile 数仍为 0，供给组全部内容不变且 CLOSED/NOT_VERIFIED。Gov 普通二进制 SHA256 为 `392e5c81c9cb96f835c90ea0b77cbdf989bdb5e6630958c70801f83289296114`，Acc 普通二进制为 `8010247de88904934dfbb326dbca06f5421d20bf1a3d9f3e4c07a6bb8a5ca8b6`。Gov SIGTERM 退出码为 0。

`source.sha256` / `source-verify.log` 校验该副本的 Go、SQL、Proto、脚本、YAML 与 JSON 输入；它是本组件受测文件摘要，最终两仓提交/source manifest 和 CI 仍由协调者登记。最终模块的 FULL/UNKNOWN BFF 窗口由主联合测试另行串行运行，其回执不能由这里的预编译推断。暂停前和 9f 模块下的失败/复跑记录均保留，未覆盖为当前结果。

这些结果支持本组件 BASE-03/04、BOUND-01/04 与 A 层合同边界。它们不证明真实 GPU、正式 owner、密码登录或生产部署完成。

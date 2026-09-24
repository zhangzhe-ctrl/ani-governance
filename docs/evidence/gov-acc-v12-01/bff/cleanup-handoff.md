# GOV-ACC-V12-01 任务清理交接

状态：**prepared / not_executed**。2026-09-24 Fedora 实际只读清点；脚本语法与默认只读模式退出 0。此记录不表示已备份、已停止或已清理。主任务要求等待 Governance 首次提交与 CI 通过后，再由主任务调用。

脚本：`scripts/accelerator-acceptance/cleanup-task.py`。远端已放在 `/home/chabking/gov-acc-v12-01-20260923/gov-bff/scripts/accelerator-acceptance/cleanup-task.py`。只操作固定任务根，不接受任意根目录参数；默认只读，只有 `--execute` 进入变更路径。未改生产源码，未提交。

## 实际资源清单

任务根：`/home/chabking/gov-acc-v12-01-20260923`，执行用户 `chabking`，主机 `fedora`。

| 引擎 / 容器 | 容器 ID 前缀 | 回环端口 | 已确认匿名卷 |
|---|---|---|---|
| Docker `gov-acc-v12-01-acc` | `224573e9b689` | 25432 | `ed70eb4db176adea821800913a7f444d61cb438942c52f71fe4780b2b75eff85` |
| Podman `gov-acc-v12-01-gov-pg` | `78b915ff131b` | 25433 | `47201750b540fb41e61687daf3393edbf34268795b0f6ee16cfaed5b84bb64f0` |
| Podman `gov-acc-v12-01-bff-redis` | `46ff43452c46` | 19382 | `ee05f849baf3e79681ce736ec510930eb1c0717c362036c29ca2fac38b7da79c` |

Docker 卷含 `com.docker.volume.anonymous` 标记；两个 Podman 卷的 `Anonymous=true`。实际 `ps -a --filter volume=...` 均只有表中对应容器。脚本固定完整容器 ID，并在执行时复核匿名标记、唯一引用及完整 mount 集合。不会执行 prune，不删除镜像，不影响其他容器或卷。

最终只读模式仍发现以下四个应用进程；主联合 Gov/owner 测试进程已经自然退出：

| 用途 | PID | `/proc` start_ticks | 停止方式 |
|---|---|---|---|
| A 正式进程的测试 wrapper | 1085376 | 25735284 | 创建 `joint-a/formal-resume/stop` |
| A provider source helper | 1085520 | 25735893 | 由 wrapper 正常关闭 |
| A 普通 Accelerator | 1085537 | 25735910 | 由 wrapper 正常关闭 |
| B `acc-contract-final.test` | 1101036 | 25788646 | 验证 exe 与 start_ticks 后，仅向此 PID 发送 SIGINT |

A 的临时目录准确为 `/tmp/TestFormalProductionConsumptionGate2132644118`；wrapper 正常退出负责删除其 `t.TempDir`。脚本预先保留两个运行日志，但不复制其证书/私钥。目录若未消失，脚本拒绝宣告完成，需检查，绝不通配删除 `/tmp/Test*`。任何未知任务进程、PID 复用、45 秒内未停止都会失败关闭；cleanup 本身不使用 pkill 或强杀。既有 A wrapper 的 `startFormalCommand` 在 SIGINT 后等待 5 秒，超时会对自己启动的准确子进程 Kill，这是既有 helper 内部停止行为，不能宣称整个停止链绝不强杀。

实际数据库（执行时再次枚举全部非 template DB，包含 postgres）：

- Acc：`acc_joint_b`、`acc_legacy`、`acc_upgrade_final`、`accelerator`、`accelerator_formal`、`postgres`。
- Gov：`gov_acc_data`、`gov_acc_data_resume`、`gov_acc_dev`、`gov_acc_failed`、`gov_acc_fault`、`gov_acc_formal`、`gov_acc_joint_b`、`gov_acc_lab`、`gov_acc_mutation`、`gov_acc_restore`、`gov_acc_test_owner`、`gov_acc_upgrade_resume`、`postgres`。

任务私有缓存实测合计约 39 GiB：`cache/{acc,gov-data,gov-bff,gov-main,gov-fault}-{build,mod}` 共十个目录。仅清理这些准确目录；共享 Go 缓存、工具安装目录、Git bundles、源树和版本锁保留。

私有文件清单由脚本常量明确列出：任务根三个 Acc DSN；A/B DSN 与私有配置；B 证书、私钥、cursor key、control token；`task/{joint-b,formal-gov,lab,gov-fault,gov-data-resume,gov-upgrade-resume}` 中的已确认 DSN、正式 Gov session/access key/config；独立 lab PKI。`joint-b/policy-recovery-<UUID>` 只删除两个固定叶子 `acc-no-grants.json`、`governance-config.backup.json`，保留策略快照与日志。路径与文件类型均验证，不递归删除整个 joint 目录。

## 执行顺序与失败边界

1. 默认模式打印不含凭据的 metadata；执行模式先拿全部已有任务锁的非阻塞排他锁。测试还在运行时拒绝进入停止流程。
2. 验证仅剩上述已知 helper；验证容器完整 ID、匿名卷及唯一引用。创建 `recovery/final-<UTC>`（0700）。现有 `.dump` 全部保留，收紧到 0600 并记录摘要。
3. 正常停止 A wrapper/子进程及准确 B PID，再次确认没有 Gov、owner、同步或测试进程。各 PG 必须没有其他 client backend。
4. 两个 PG 均执行完整 `pg_dumpall -U postgres`，然后每个非模板数据库执行 `pg_dump -Fc`；每个 custom dump 用容器内匹配版本 `pg_restore --list` 检查。Redis `SAVE` 后保存 `/data/dump.rdb`。
5. 新备份文件全为 0600，生成并验证 SHA256SUMS、fsync 文件，写 `BACKUP_COMPLETE`。此脚本只验证 dump 退出码、TOC 和摘要，不冒充恢复重放测试；已有恢复验收证据仍单独引用。
6. 再次确认进程、客户端、容器和卷身份，依次 stop/rm 三个准确容器；逐个复核卷已经无容器引用后删除三个准确匿名卷。
7. 删除上述私有文件与十个缓存目录，保留全部源树、证据、旧备份、新最终备份和版本锁；最后写 `CLEANUP_COMPLETE`。

所有变更步骤失败即中止。不存在 catch-and-continue 删除逻辑。若部分容器已删除后失败，保留新备份及现场，按具体失败手工恢复/继续；脚本不承诺跨任意半完成状态自动重跑。不要替换固定 ID 来跳过检查。

## 调用命令

先检查只读输出：

```sh
ssh fedora 'python3 /home/chabking/gov-acc-v12-01-20260923/gov-bff/scripts/accelerator-acceptance/cleanup-task.py'
```

主任务确认所有验收与首次发布 CI 完成后执行；目前没有执行这条命令：

```sh
ssh fedora 'python3 /home/chabking/gov-acc-v12-01-20260923/gov-bff/scripts/accelerator-acceptance/cleanup-task.py --execute'
```

如需将执行日志纳入证据，只保存脚本的脱敏输出，不打印或提交恢复文件内容。新最终备份位置只在 Fedora `recovery/final-<UTC>`；每次成功执行的真实路径与退出码由主任务追加登记。

## 保留与恢复边界

既有备份：`evidence/acc/**/populated-recovery.dump`、`evidence/acc/v1-populated-before-upgrade.dump`、`evidence/gov-data/{old-populated,mutation-base,new-populated}.dump`、`evidence/pause/{gov-joint-b,test-owner}.dump`、`task/formal-gov/identity-snapshot.dump`。清点时其中九份为 0644；执行阶段会收紧为 0600，其原始内容保持不变。准备阶段未修改权限。

新 `cluster.sql` 包含角色密码 hash，custom dumps 包含业务身份与数据，Redis RDB 包含测试会话。它们是受保护的恢复资料，不是脱敏证据，禁止提交仓库。现场临时证书、DSN、session 文件删除，不等于所有凭据信息被销毁；恢复备份、受限旧日志及原任务源码仍按各自保留边界存在。

恢复需在新的隔离容器/数据库进行：先按 `containers.json` 固定镜像与版本，验证 SHA256SUMS；完整集群恢复可使用对应 `cluster.sql`，选择性恢复使用 custom dump 和经过审阅的角色/权限装配。两种方案不得未经审查叠加到同一 populated DB。重新签发任务 CA/mTLS 证书、生成新 DSN/session/control token，不恢复旧测试会话用于业务访问。按既有迁移/受限角色验证和 worker 恢复脚本复验，再恢复服务。此交接未执行新最终备份的恢复。

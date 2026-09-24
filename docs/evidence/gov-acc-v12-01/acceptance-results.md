# GOV-ACC-V12-01 逐项验收结果（交付收尾）

本表保留原 84 项和追加 11 项，共 95 个 ID，不修改 Accelerator 仓库
`docs/plans/governance-accelerator-v1.2-acceptance.md` 的要求。核对时间为
2026-09-24；82项软件必验已通过，收尾与CI例外见[交付记录](release/closeout.md)。

`pass` 只表示本行列出的要求在所列受测版本上已有相应断言；不代表最终模块、
发布或硬件通过。`not_verified` 表示本行仍有明确子句或交付核验未闭合，同行已通过
的组件不会被抹去。历史失败保留并注明后续修复，不把代码存在或编译通过当作运行通过。
最终源文件、模块版本、版本对和新增门禁回执由发布协调者继续更新。

本表包括 82 项软件必验、4 项 SHIP 交付条件和 9 项固定未验证范围。
原始 95 个 ID 全部保留；当前统计和未闭合项见文末。ID 集合/链接校验仅检查文档，
不替代运行验收。最终模块下联合普通测试和 race 均已取得 exit 0 原始回执。

## 版本与证据索引

- Gov 起点 `0fbe1a69e49cc23dc7a1696b62f68c34a7c6a48a`；本批测试包含未提交改动，
  不能把起点 SHA 标成最终实现。正式实现提交为 `bd9ad1a33bbe8ce28f4faeb19bfc9ee494ae8a84`；完整树锁、1535项最终联合源码匹配及实际发布见 [release](release/publication.json)。
- 最终 Gov 已用独立缓存、`GOWORK=off` 正常下载并消费
  `v0.0.0-20260924030150-1d32dd9a9173`，Acc 对应完整 SHA 为
  `1d32dd9a9173b8869fa0ef2ae64e20b88f2ca0a3`。模块 sum、来源和版本见
  [模块回执](bff/final-1d32dd9/module.json)、[校验](bff/final-1d32dd9/module-verify.log)及 J。
  旧 `9f9712198488` 模块的日志只保留历史证据，不作为最终版本复验。
- Acc candidate-v7 完整 `make verify` exit 0；之后基于
  `c2fb7410cc2a6f583d8d253224b65861416b5ecc` 的 replicas 注释修正也完整验证 exit 0。
  仅两行注释变化，去 source-info 的 descriptor 一致。已发布 Acc 的 clean audit 与
  精确 SHA CI success 见 [发布审计](release/acc-release-audit.log)、[CI 回执](release/acc-ci-success.json)。
- A 普通 Gov→普通 Acc 在最终模块下复验的二进制 hash、时间和 SIGTERM 0 见 F；
  Acc 普通二进制与最终提交差异仅上述不改变 descriptor/runtime 的 Proto 注释。没有生产 owner 或硬件。
- 下列命令均在授权 Fedora 任务隔离目录、私有 cache/lock 和真实受限 PostgreSQL 上执行。
  通用环境为 `GOWORK=off GOMAXPROCS=2 GOFLAGS=-p=2`。DSN、私钥和令牌不在本文。

| 代号 | 命令/回执和证据位置 | 证据能力 |
|---|---|---|
| A | Acc `docs/evidence/gov-acc-v12-01/acc/candidate-v7/verify.log`、`verify.exit=0`、`source.sha256`；增量 `acc/replicas-comment/{verify.log,verify.exit,generation-result.json,changed-source.sha256}` | `make verify`：sqlc、生成、全测试、race、tenant mutation、dump restore、漏洞/secret；全部 22 RPC 实际 mTLS，正式进程边界及 provider 回放。路径相对固定 Acc 仓库，不冒充 Gov 日志 |
| A-map | Acc `docs/evidence/gov-acc-v12-01/acc/resume-results.md`、`README.md`、`upgrade-final.log` | 组件映射、旧库升级和历史失败来源；以 A 原始日志为准 |
| B | 最终模块J内[满容量BFF](joint-resume/20260924T031542Z/bff-WAIT_FOR_CAPACITY.log)、[UNKNOWN BFF](joint-resume/20260924T031542Z/bff-FIT_UNKNOWN.log)与相邻json；J-race内[满容量](joint-resume/20260924T031813Z/bff-WAIT_FOR_CAPACITY.log)、[UNKNOWN](joint-resume/20260924T031813Z/bff-FIT_UNKNOWN.log)；历史[BFF恢复报告](bff/resume/README.md) | `bff-joint.sh`实际成功返回父测试，J/J-race整体exit0；真实JWT/Redis/PG/生成HTTP/mTLS，20 BFF内容、权限、游标和非空脱敏断言。最后module覆盖由这两轮承担 |
| B-old | [首次 BFF 报告](bff/README.md)、[内容断言](bff/bff-joint-content-final.log)、[鉴权与 Network 回归](bff/bff-auth-network-regression-02.log) | 修复前失败及修复后结果；保留历史时间边界 |
| F | [最终模块普通进程报告](bff/final-1d32dd9/README.md)、[A→A 回执](bff/final-1d32dd9/formal-gov-acc-process.json)、[日志](bff/final-1d32dd9/formal-gov-process.log) | `formal-gov.py … joint-a/formal-resume/ready.json`，普通 Gov/Acc，真实 Publish 412、profiles=0、group 未改变；NOT_ENABLED、测试路由404、Gov SIGTERM=0 |
| L | [最终模块 lab](bff/final-1d32dd9/lab.log)、[exit=0](bff/final-1d32dd9/lab.exit)；历史 [lab 恢复](bff/resume/lab.log) | `lab-regression.sh`，8 个真实 PG/mTLS 历史模拟器回归；同 owner 多 DNS SAN/不同 owner 多 SAN |
| D | [数据层映射](data-layer.md)、[最终模块 verify-gpu-05](data/resume/verify-gpu-05.log)、[meta](data/resume/verify-gpu-05.meta)、[源码清单](data/resume/verify-gpu-05-source.sha256)、[比对](data/resume/verify-gpu-05-source-check.log) | `make verify-gpu` exit 0，03:09:30Z–03:17:52Z；PG 11.038s、data race 58.214s、service race 1.177s、lab8 8.235s；schema/sqlc/格式/构建/受影响测试/0 reachable漏洞/0 secret。完整交付树由 BASE-01 另锁 |
| D-old | [verify-gpu-03](data/resume/verify-gpu-03.log)、[meta](data/resume/verify-gpu-03.meta)、[04](data/resume/verify-gpu-04.meta) | 03 整体 exit 2（secret精确例外未完成），04 修复后 exit 0，05 最终模块 exit 0；保留失败，不改写历史 |
| D-fix | [secret 定向复跑](data/resume/secrets-05.log)、[空 findings](data/resume/secrets-05.json)、[套餐兼容](data/resume/plan-compatibility-05.log)、[时区](data/resume/plan-timezone-01.log)、[目录大分页](data/resume/catalog-pagination-01.log) | 定向修复 PASS，随后 D 完整入口通过 |
| D-neg | [GPU负向/套餐锁](data/resume/gpu-negative-policy-02.log)、[meta](data/resume/gpu-negative-policy-02.meta)、[DELETE负向](data/resume/delete-negative-02.log)、[meta](data/resume/delete-negative-02.meta)、[due竞争](data/resume/claim-due-recheck-01.log)、[meta](data/resume/claim-due-recheck-01.meta) | 真实受限PG ordinary/race通过，最终 D 再覆盖：11退款失败向量逐次核对余额/charges/receipt不变、7 DELETE失败向量全库快照不变、套餐实际持锁/数据库now到期、领取等待锁后重新核对due |
| D-sync | [同步租约/审计](data/resume/sync-lease-audit-01.log)、[meta](data/resume/sync-lease-audit-01.meta) | `go test -tags quota_pg … -run 'TestQuotaGpuSyncLeaseRetryAndBlock|TestAuditDriver'`，真实 lease 过期/接管、due-time、旧 revision/generation、block/恢复、tenant CAS；exit 0 |
| D-up | [旧库升级](data/resume/old-upgrade-migrations.log)、[51 表摘要](data/resume/upgrade-digest-assert.log)、[旧账本重放](data/resume/old-upgrade-replay.log) | 原 populated 备份另库升级，51 旧表不变，新增两目录/两空表；原 operation/receipt 重放与后续累计释放 |
| D-restore | [restore 差异](data/restore-digests.diff)、[恢复继续](data/restore-continuation.log)、[失败迁移](data/failed-migration.log)、[回滚断言](data/failed-migration-rollback-assert.log) | populated dump 另库、逐表摘要一致和 worker 收敛；注入失败为预期非零，不是交付失败 |
| P | [进程故障说明](process/README.md)、[普通](process/final.log)、[race](process/race.log)、相邻 exit=0/时间/source manifest | 两个 `TestQuotaProcess*`：5 类事务 × before/after COMMIT 的 10 次实际 SIGKILL；12 轮双 OS 进程取消/领取和两次独立重放 |
| U | [能力/摘要/ACK/终态测试](bff/resume/capability-final.log)、相邻 exit=0 | `TestGpuCapabilityRequiresSameOwnerActions` 和 GPU canonical/向量/ACK/projection 单元测试，不替代真实 RPC/账本 |
| J | [最终模块联合日志](joint-resume/20260924T031542Z/test.log)、[命令](joint-resume/20260924T031542Z/command)、[exit=0](joint-resume/20260924T031542Z/exit_code)、[模块](joint-resume/20260924T031542Z/accelerator-module.json)、[源码](joint-resume/20260924T031542Z/runtime-source.sha256)、[结果](joint-resume/20260924T031542Z/joint-contract-result.json) | `go test -tags gpu_joint … -run '^TestJoint(SoftwareContract\|MixedCharges\|SyncFailureRecovery\|DispatchFailuresAndStop\|ResolvedSnapshotRace\|PolicyRevocationRecovery)$'` 六项 PASS 140.884s；双Gov进程12并发、独立ownerPG、混合charge、known-zero/UNKNOWN与真实BFF，精确时间见相邻 started_at/finished_at |
| J-race | [最终联合race日志](joint-resume/20260924T031813Z/test.log)、[命令](joint-resume/20260924T031813Z/command)、[exit=0](joint-resume/20260924T031813Z/exit_code)、[源码](joint-resume/20260924T031813Z/runtime-source.sha256) | J相同六项显式 `go test -race -tags gpu_joint … -count=1`，全部PASS 161.297s；相邻时间/版本/结果文件完整保留，独立于normal结论 |
| W | [dispatch故障/尾项](joint-resume/dispatch-fault-tail.log)，最终模块 J 再跑 | 4种非法ACK保留charge/阻断；首条真实RPC在途时尾项保持QUEUED、attempt0且无lease，释放后各调用一次；真实PG阻塞Stop不产生后续owner调用，重启收敛 |
| S | [同步故障](joint-resume/sync-fault-conflict.log)，最终模块 J 再跑 | 真实 mTLS Unavailable/deadline、真实 Acc 同 revision 冲突；保留 payload/账本并分类重试/block，独立 dispatch/refund、新 revision 恢复 |
| Q | [政策撤销](joint-resume/policy-recovery-02.log)，最终模块 J 再跑 | 原委托Resolve允许，零grant配置拒绝；tenant OFF/expired/quota0、DELETE未ACK时真实owner mTLS退款仍成功；零用户grant下旧操作DECLARED→ENDED |
| R | [Resolve快照竞争](joint-resume/snapshot-race-first.log)，最终模块 J 再跑 | 两真实委托Resolve经测试代理返回两个合法、语义digest相同但字节不同的plan；胜者完整canonical唯一持久，败者及后续重放不覆盖、不再Resolve；不声称修改真实硬件规格 |
| O | [实际PG中断回执](joint-resume/acc-pg-outage.json)、[日志](joint-resume/acc-pg-outage-03.log) | 同一普通Acc PID，实际任务PG停止/恢复；ready 200→503→200、health保持200、group规范摘要不变；与 W 数据库锁阻塞、S 下游RPC中断分别记录 |
| G | [owner指南](../../contracts/gpu-owner-integration-guide.md)、[运行恢复手册](../../operations/governance-accelerator-v1.2.md)、[发布模块文档校验](docs-owner/document-validation-published-acc.json) | Fedora 50个相对链接、4固定摘要、Unicode/int64示例通过；最低Acc精确SHA/模块和Proto/runtime replicas1..16一致；Gov最低是包含该能力/指南的交付提交，避免自引用SHA |

J/W/S/Q/R/O 已归档本仓并读取原始断言，旧模块/失败尝试仍保留。
组件结果不自动带来最终交付树与 SHA 的通过结论。所有 A/B/C 输入均
`hardware_observed=false`；A 无消费证明，B 有明确测试外部证据端口与独立测试 owner，
C 为生产 reader 构造来源回放。

## 输入、目录、权限与 BFF

| ID | status | 已有命令/证据、断言与未闭合边界 |
|---|---|---|
| BASE-01 | pass | [完整树锁](release/source-lock.json)：本地不可变暂存tree与Fedora独立git import tree同为c8dc3123eba699c099b586f8a73e7100910379fb；完整逐文件manifest含AGENTS/合同/向量/文档。生产/查询/schema/module与make05一致，2个测试/runner增量由最终J/J-race通过；后生成回执不自引用，最终提交发布另记 |
| BASE-02 | pass | A、D、P、B 的环境/角色/证书与 pause/resume 记录；Fedora 私有副本/锁/cache、回环端口和独立 A/B/owner 库。运行类工作没有回退本地 |
| BASE-03 | pass | F/J/D均以GOWORK=off、私有cache消费正式模块v0.0.0-20260924030150-1d32dd9a9173；module.json记录完整来源SHA及module/mod sum，go mod verify通过；无兄弟replace/internal/file-GOPROXY，最终模块真实A/B联调已复验 |
| BASE-04 | pass | A固定Buf/Go/sqlc与两次生成无diff；B定向Buf/OpenAPI无漂移，D最终05完整schema/sqlc再生成/格式/普通和lab/admin build通过；J/J-race覆新增测试和runner，生成未扩至无关领域。最终完整交付树锁另见BASE-01 |
| CAT-01 | pass | D-up 显式升级/重复迁移核对及 D 目录测试；两正式 code/unit/CONCURRENT、旧 gpu.count LAB；新目录不擅自授予套餐额度或执行能力 |
| CAT-02 | pass | U 的完整/缺 DELETE/异 owner CREATE/DELETE 注册表四分支；D/B/F 目录投影。旧 enum/tag 保留，能力按同 owner 受控 action/code 派生，目录数据不能启用 |
| CAT-03 | pass | F 普通构建两 code NOT_ENABLED；J 新 create-disabled 在缺 resolver/adapter 时 503、无新受理；U 能力负向。管理 BFF 可读不能推出 owner 已启用 |
| CAT-04 | pass | U/Acc A 的 mode、F=1/256/1024、粒度/溢出/replicas17/devices2/双计负向；J 共享原 charge=replicas×6144，整卡向量由组件覆盖，不声明真实 GPU 运行 |
| AUTH-01 | pass | A 全 22 RPC 真实 mTLS、错服务/CA/无证书；B/F 真 Gov→Acc，Gov client 配置/错误 server identity 测试在 D/B-old。无 insecure 回退 |
| AUTH-02 | pass | B 每次带伪造公网身份 header仍从 JWT Principal/持久 UUID 重建 context；tenant0 租户入口拒绝，另一真实租户拒绝；A typed/metadata 不匹配负向 |
| AUTH-03 | pass | A合法Gov URI下逐20动作无grant均精确DELEGATION_DENIED、平台/tenant作用域及typed metadata校验；Q/J/J-race原Resolve获准后切至独立零grant配置，原公开Resolve被拒，tenant OFF/到期/额度0时原操作仍DECLARED→ENDED。Sync走Gov服务身份，不借原用户当前grant；Observe独立owner身份 |
| AUTH-04 | pass | B 他租户原 ref/binding 404；D/P known foreign operation/charge、scoped cancel/ACK/CAS/复合 FK；A tenant usage/binding 及分页负向。不是只测试未知随机 UUID |
| AUTH-05 | pass | B-old 持久 role permission OFF/DENY、role/API/permission OFF、模块删除后真实 403，刷新恢复后可用；D schema module CHECK、API 目录/权限测试。测试显式授予不等于默认给全部角色授权 |
| AUTH-06 | pass | L 真 mTLS 错 CA/无证书/错 owner；同 owner 多 DNS SAN 成功，不同 owner 多 SAN 拒绝且余额不变；F 生产没有测试 owner map。与 Acc 唯一 URI SAN 分开验收 |
| AUTH-07 | pass | U/B-old `TestAcceleratorPrincipalRequiresExplicitScopeAndJWT`、现有 AK/SK whitelist 与 auth/Network 定向回归；GPU 首期只收用户 JWT。未声称已完成 GPU AK/SK 正向接入 |
| API-01 | pass | B 两容量窗口均完整 12 管理 HTTP；内容/ID/版本断言、5 写请求原幂等重放、同 key 异内容和旧版本 409。首次管理建目录由真实 Acc 管理 RPC 完成，BFF 不冒充首次播种 |
| API-02 | pass | B 全租户 HTTP、具体 version、usage/binding 从本库原 CREATE/ref定位；未知 owner filter400/detail404、已知他租户404。任意 owner 字符串不扩大支持范围 |
| API-03 | pass | J最终normal满容量8设备/88范围转换→WAIT_FOR_CAPACITY，真实freshness到期→FIT_UNKNOWN；J内实际JWT/HTTP BFF两窗口额度sufficient、12288MiB、非法shape400、前后三表摘要不变、NOT_ENABLED/NOT_CHECKED，J-race独立复跑。设备/转换数是该轮fixture，不当硬件容量 |
| API-04 | pass | J 实际 SaveObservation 创建敏感非空 binding；B 管理字段可见、租户删除物理/Pod/证据/plan等；ENDED 仍有非空live binding。并非空列表或真实硬件采样证明 |
| API-05 | pass | B page1/200/201、两页无重复/第一页稳定、filter/action/tamper及tenant负向；A actor/cluster/action/method/filter/tenant精确 cursor绑定组件与PG稳定keyset。B另一actor403可能先在grant层拒绝，不冒充该变体HTTP游标分支 |
| API-06 | pass | B/F实际400/401/403/404/409/412；U `TestAcceleratorErrorMappingKeepsReasonAndDropsRawContent`逐grpcCode含503/504/Unknown，S真实deadline/unavailable。HTTP超时映射与真实RPC故障为不同组件证据，没有伪造HTTP504全链 |
| API-07 | pass | B Principal tenant1忽略tenant_id=9102、无JWT401；旧admin平台200/租户即使授权限也404。D-fix真实降额limit4/occupied6/available0/over_limit=true，不借租户传参跨读 |

## 摘要、受理与投递

| ID | status | 已有命令/证据、断言与未闭合边界 |
|---|---|---|
| CAN-01 | pass | U逐固定baseline/spec/plan/projection摘要；新增直接protojson hash不同的负向；G引用相同固定向量并在Fedora校验。不是两个临时同源实现相互比较 |
| CAN-02 | pass | U Unicode/转义/null/int64上界、float/overflow拒绝、受管key重排与重复；Acc A和U对应F=1/256/1024、6144MiB、F10负向 |
| CAN-03 | pass | J同key改变业务内容409、snapshot/canonical源测试；U plan字段语义变异/非语义published和KeyValue顺序不变；原request身份不包含动态Resolve/新资源UUID。动态不同Resolve快照竞争另见CREATE-03 |
| CREATE-01 | pass | J真实Resolve→PG原operation/charge/账户→事务后worker→独立ownerPG持久ACK；D多向量失败回滚，P Occupy COMMIT前kill无半写。没有内存fake冒充账本 |
| CREATE-02 | pass | J当前授权先校验，create-disabled使用无adapter/无resolver仍重放原IDs、无权限404、新请求503；D到期后的原幂等重放；原plan/charges从库恢复。旧plan不重新Resolve的证据不是实际改供给F的演练 |
| CREATE-03 | pass | J/J-race双Gov OS进程12同key请求只产生原operation/resource/一次完整charge，异内容409；R双进程各经真实委托Resolve后返回合法不同字节快照（语义digest相同），原canonical逐字等于唯一胜者，败者及历史重放保持原plan/完整GPU+storage向量，Resolve调用总数仍2 |
| CREATE-04 | pass | D多向量不足全部回滚/真实sqlc降额竞争；D-neg实际持tenant行锁改套餐，Occupy等待提交后按新limit拒绝，原占额不变；独立SQL确认expires_at等于数据库now、随后新受理拒绝，未借本机时钟；J未授权历史重放拒绝。不是声称两个不同事务的时钟仍相等 |
| CREATE-05 | pass | U完整GPU子集/业务总集合按code/id、漏项/重复/错单位；J mixed删除移除storage后不调owner、恢复原行后完整发送。合法单模式只有一个GPU code，未构造整卡+共享双计 |
| CREATE-06 | pass | J真实生产 `BuildGpuOwnerAttachments`、owner核验原ref/canonical；U结构化actor和plan/hash/business digest/metering校验。用户输入不成为可信charge/plan来源 |
| CREATE-07 | pass | W真实owner非法operation/resource/accepted/json四分支均UNKNOWN+blocked、未退款；显式修复后原ID重投ACKED。J持久提交丢ACK同ID/内容重试；U额外重复key/别名/尾随JSON拒绝 |
| CREATE-08 | pass | J known-zero和真实freshness UNKNOWN均原子受理再安全取消；额度有余、plan合法、测试owner装配可用，未做库存预留/二次扣额。B两个窗口同步验证真实Preview |

## 删除、释放与投影

| ID | status | 已有命令/证据、断言与未闭合边界 |
|---|---|---|
| DELETE-01 | pass | J/D/P QUEUED attempt0取消、原全集合退款、DELETE acceptance同事务，返回CANCELED_BEFORE_DISPATCH且无dispatch/owner假ACK；COMMIT故障无半写 |
| DELETE-02 | pass | P 12轮独立双OS进程barrier领取/取消，两个合法结局均出现，余额/attempt/DELETE重放核验；normal和race均exit0 |
| DELETE-03 | pass | J已attempt/ACKED实际走QUEUED_FOR_OWNER；D CancelUnsent已发送负向、GPU原plan/全部charges，未新占额/即时退款。UNKNOWN由丢ACK与W保留账本场景覆盖 |
| DELETE-04 | pass | J mixed真实移除原storage行→ORIGINAL_CHARGES_INVALID、ownerDB无命令；健康操作不饿死；恢复同一原charge→完整DELETE持久ACK。GPU+storage两个合法维度，不是First |
| DELETE-05 | pass | J同key重放、本地CANCELED_UNSENT后新DELETE key无重退；真实DELETE先于CREATE到ownerPG墓碑，迟到CREATE不执行。异内容冲突由D幂等与附件校验组件承担 |
| DELETE-06 | pass | D-neg真实PG受理错owner、未知resource、已知他tenant resource、缺GPU/空全集合、原units错误、冻结向量损坏七分支，operations/charges/accounts/acceptances/receipts完整快照不变；原CREATE关联与tenant隔离由D/B补足；P本地/owner DELETE提交前后SIGKILL无半acceptance/孤立dispatch |
| RELEASE-01 | pass | J/L真实mTLS ReportQuotaRelease；DNS owner和原charge定位租户；协议无可信tenant字段，错身份拒绝。D同owner/原operation/charge关系校验不使用调用者tenant |
| RELEASE-02 | pass | J实际owner mTLS无DELETE全GPU回调拒绝；Q合法全量通知先于DELETE ACK成功；D-neg 11分支含partial/excess/重复id或code/错charge或code/DELETE误作原CREATE/未知及已知其他CREATE，各次余额/charges/receipt不变。当前合法GPU子集只有1种code，不编造整卡+共享双计；缺GPU项的纯非GPU累计仍允许且不结束GPU，原集合缺项由DELETE-06/CREATE-05拒绝 |
| RELEASE-03 | pass | D累计乱序2→1 delta0/同事件异内容冲突；J mixed真实事件及新事件重复累计5/5/10幂等；D-neg真实PG分别合法storage后非法GPU、合法GPU后非法storage，整个向量/receipt全部回滚，每例重新核对账户及原charges快照 |
| RELEASE-04 | pass | P真实Release COMMIT前kill/后丢响应两次重放；J Gov真实退款提交后owner在outbox ACK前SIGKILL，fresh owner原事件重发只退一次；L通知暂不可用重试 |
| RELEASE-05 | pass | J持合法owner证书主动无DELETE全量GPU通知被生产校验拒绝、余额保留；D同负向；W错误ACK不退款。删除ACK/NotFound/timeout没有自动退款分支，fixture关闭事实与硬件事实分别标注 |
| RELEASE-06 | pass | Q/J/J-race对原GPU操作tenant OFF/expired/quota0、新建disabled、用户grant为0时，owner真实mTLS全量累计退款成功且DELETE尚未ACK；S同步失败独立于退款；J持久closed-never-executed与非空live观察分列，空Pod范围不能冒充ObserveRelease已解除 |
| SYNC-01 | pass | J原CREATE提交后nosync进程被kill、尚无sync行，重启派生DECLARED；删掉终态sync行后逐字重建原payload/hash；P同步CAS提交故障验证持久原子性 |
| SYNC-02 | pass | J旧CREATE在后续释放后重扫成ENDED；D有界全局候选/原tenant再读与跨tenant负向。轮次回绕不只扫created_at增量；源码与实际旧行回收联合证明 |
| SYNC-03 | pass | U空集合/部分GPU/非GPU/DELETE非终态派生；J mixed GPU已全退但storage未退已ENDED、原time/ref/plan确定性重建，local cancel为另一合法rev2分支 |
| SYNC-04 | pass | A真实Sync终态先到/晚1/重复2不复活、异create与同revision异内容冲突；S真实Acc冲突被Gov保留并block。两revision使用原CREATE时间，不取重试时钟 |
| SYNC-05 | pass | D-sync两个领取竞争/实际lease到期/接管、旧gen/rev和未领取ACK全部拒绝；J双Gov进程；S真实deadline后原payload重试，不变业务attempt/charge。数据层和网络层分列，不冒充所有时序同一测试 |
| SYNC-06 | pass | S真实Unavailable/deadline、FailedPrecondition和真实USAGE_PROJECTION_CONFLICT分类；原payload/hash保留，健康同步/dispatch/退款独立，后来rev2恢复。D-sync持久block/due-time/CAS补足 |
| SYNC-07 | pass | J审核fixture经真实SaveObservation写非空binding，再真实Gov Sync关联；ENDED仍保留live binding。B非空敏感DTO映射通过，A内部provider关联回归独立。hardware_observed=false |

## 数据库、进程、provider 和边界

| ID | status | 已有命令/证据、断言与未闭合边界 |
|---|---|---|
| DB-01 | pass | D-up旧populated库显式Atlas迁移+原幂等/receipt继续；A真实v1升级。D/A运行角色TEMP/DDL拒绝，普通服务启动无迁移/播种；不以新空库bootstrap替代升级 |
| DB-02 | pass | D真实PG tenant必填、JSON null/missing/foreign ref、复合FK和sync revision；A对应tenant/ref约束。D-up 51旧表/Acc十旧关系数据保持 |
| DB-03 | pass | P 10个实际SIGKILL窗口覆盖Occupy、本地cancel、owner DELETE、Release和Sync ACK CAS，fresh进程重放无半写双扣退；A Sync发送/COMMIT kill补接收侧。源码故障点仅_test.go |
| DB-04 | pass | D-restore逐表count/规范JSON digest一致、restored terminal worker继续，无清理重seed；A populated dump独立库13关系逐字一致及恢复后repo/contract测试 |
| DB-05 | pass | D-restore/A显式注入失败migration回滚，原表/余额保持；G/data-layer明确schema2/模块不兼容旧二进制、禁止自动DOWN带业务新表，保留备份恢复入口 |
| PROC-01 | pass | J独立Gov×2、Acc B和owner OS进程，三个独立真实PG权限/配置；F两个普通二进制A链。测试owner不进入正式composition |
| PROC-02 | pass | W/J/J-race实际首RPC被阻塞时尾项始终QUEUED/attempt0/leaseNULL，证明改为单条临RPC领取后无预领取尾部lease老化，解除后两条各发1次；D-neg候选等待真实tenant锁时due被推后，锁后拒绝领取；D/P双worker/旧generation晚回写负向。A延迟Inspect/Verify/OPEN/Resolve及R证明网络无PG持锁 |
| PROC-03 | pass | O同一正式Acc进程实际PG停止/恢复，ready200→503→200、health200、目录摘要一致；S真实下游中断→重试恢复；F Gov SIGTERM退出0，W在真实PG阻塞中Stop取消且不再调owner、新worker恢复，J/P多进程故障后持久恢复。不同组件的中断类型不混称同一实验 |
| PROC-04 | pass | A全affected race，P真实OS故障race；D最终模块data race58.214s/service1.177s；J-race最终六联合场景161.297s全部PASS含policy/snapshot/fault/mixed/主链，normal独立140.884s。保留历史失败和修复记录，最终完整源码锁由BASE-01另验 |
| PROVIDER-01 | pass | A `TestProviderControlledSourcesPersistRecoverAndRelease`：生产TLS HTTP和Unix gRPC reader实际执行，来源/样本为构造软件fixture；不是硬件采样 |
| PROVIDER-02 | pass | A同测试真实PG持久：binding先到、三源去重、局部冲突、stale/失联/缺源UNKNOWN、受控范围正向解除后容量恢复；J真实freshness UNKNOWN另证 |
| PROVIDER-03 | pass | A错身份/空范围/受控实际解除；J owner直接Observe非空live范围STILL_PRESENT、空scope拒绝；G明确局部scope/未知在途写不能证明完整业务关闭，Gov无Observe自动退款。真实owner完整历史由OWNER-02另验 |
| BOUND-01 | pass | F普通Gov lab/fault/control路径404、GPU目录NOT_ENABLED；正式registry/map空，U缺执行能力在新占额前失败。测试business listener仅_test.go，未添加生产OpenAPI接口 |
| BOUND-02 | pass | A正式普通Acc+合法TLS provider无消费证明，Publish真实CONSUMPTION_NOT_VERIFIED、不落profile、不变VERIFIED；OPEN PROFILE_NOT_ACTIVE/Resolve NOT_FOUND如实记录。F真实Gov BFF到A复核group未变 |
| BOUND-03 | pass | A/B不同DB/CA配置；B真实管理RPC加明确测试外部证据与观察helper，J SaveObservation叠加来源和ownerPG有据。原空helper未用于非空脱敏，未称纯生产硬件验收 |
| BOUND-04 | pass | L8项新pgx/sqlc账本实验回归；B-old Network/auth受影响测试，D正式/lab构建；测试入口编译隔离，无Inference/其他兄弟业务实现修改 |

## 文档、发布与固定未验证范围

| ID | status | 已有命令/证据、断言与未闭合边界 |
|---|---|---|
| DOC-01 | pass | G按最终公开Proto/配置覆盖职责、字段、单owner/新owner准入、受控装配、关闭/退款/恢复/向量；最低Acc1d32完整SHA及正式module已锁，Gov最低指含指南/能力的交付提交、精确SHA外置交付记录避免自引用。正式owner未实现不混写为本批pass |
| DOC-02 | pass | G无虚构生产业务RPC或“改名即接入”，URI/DNS分开；Fedora固定摘要/Unicode/MiB算式验证；真实业务接缝明确待owner实现 |
| DOC-03 | pass | G最终Gov运行恢复手册与接口登记/生成OpenAPI/本结果表互链；显式迁移/noDDL、停机顺序、独立projection/永久block无公开恢复API、原charge修复、另库恢复/不自动DOWN均按源码写明；明确旧Acc R0手写SQL记载的历史性，以本批sqlc整改证据和运维增补为现状，不倒改历史 |
| SHIP-01 | pass | Acc完整verify/受影响复验及最终SBOM/clean审计通过，1d32精确CI成功；Gov最终模块make05、J/J-race、普通A/BFF/lab通过，release/gov-source-parity.json与source-lock.json绑定受测源码；固定工具0可达漏洞、0未获准secret，SBOM已生成。历史失败保留，非可达依赖告警范围见data-layer |
| SHIP-02 | pass | 两仓准确origin均核对；Acc1d32、Gov bd9ad1a均普通非强推到授权任务分支，git ls-remote精确SHA一致；Gov Git bundle在Fedora还原的commit/tree一致，见release/publication.json。没有main合并、tag或生产部署；后续纯证据提交另查精确CI |
| SHIP-03 | not_verified | Acc1d32精确SHA真实CI已成功；Gov bd9ad1a实际Actions于03:40:16Z仍In progress。用户随后明确“ci没过就算了，直接走后面流程吧”，接受不等CI继续收尾；[决定与证据](release/ci-disposition.md)。不推测CI失败、不以Fedora门禁替代GitHub通过，后续证据提交也不等CI |
| SHIP-04 | pass | release/cleanup-result.json与cleanup.log记录exit0；最终19DB custom dump、2 cluster dump和Redis备份0600/TOC/hash通过后，准确任务进程/3容器/3匿名卷/临时凭据/十个私有cache已清理；独立检查进程/监听/已审阅私有路径残留为0，旧新恢复备份和证据保留。用户接受不等待CI后执行，不碰共享资源 |
| OWNER-01 | not_verified | 本批没有真实Inference/其他owner业务API、正式adapter和真实持久执行；独立PG测试owner不是该实现 |
| OWNER-02 | not_verified | 无真实工作负载renderer防绕过、历史/替换Pod追踪、在途写封闭和GPU清理；测试墓碑/通知仅软件合同 |
| OWNER-03 | not_verified | 仍只接受ani-inference合同标识；第二owner身份/公钥选择/查询隔离/正式装配未实现 |
| LIVE-01 | not_verified | 没有F>1真实plan→Pod→分配→运行显存单位证明；回放/签名fixture不升级结论 |
| LIVE-02 | not_verified | 没有同父卡两个独立真实模型加载和推理 |
| LIVE-03 | not_verified | J known-zero是构造观察范围，未验证真实K满新增Pending和安全预算 |
| LIVE-04 | not_verified | 没有真实整卡排他、量化尾差和物理释放后容量恢复 |
| LIVE-05 | not_verified | 没有真实owner/硬件限制/全量实际释放与Gov账本闭环；ENDED仍可有live binding |
| DEPLOY-01 | not_verified | 未部署共享集群/生产，未切用户流量，未将非测试真实供给伪造为OPEN |

## 数据层追加 11 项

| ID | status | 已有命令/证据、断言与未闭合边界 |
|---|---|---|
| RLS-01 | pass | A/D真实catalog flags/policy断言和显式迁移；owned表relrowsecurity=false、relforcerowsecurity=false、无隔离policy；无启动迁移 |
| RLS-02 | pass | A/D/P/L真实nonowner、NOSUPERUSER、NOBYPASSRLS，无DDL/TEMP；实际拒绝写测试，不用owner测试充当隔离证据，无tenant GUC/SET ROLE旁路 |
| TENANT-01 | pass | A/D所需租户/ref非NULL、tenant复合FK/唯一键、JSON与关系列一致；实际missing/null/foreign-reference负向，不能靠NULL绕CHECK |
| TENANT-02 | pass | A/D/P/B已知他租户UUID、同业务键、关联/读取/写入/取消/ACK/CAS/恢复；A全部cursor作用域组件补BFF分支。不把另tenant缺grant403当唯一隔离证明 |
| TENANT-03 | pass | D明确命名有界全局候选返回tenant，后续claim/read/leaseCAS/ACK/退款/sync保持tenant；A全局供给/未关联观察明确global，无tenant0普通跨租户入口 |
| TENANT-04 | pass | A四条query去tenant→sqlc生成→实际行为失败，D GetOperation去tenant→knownforeign测试预期失败；变异仅Fedora隔离副本，非编译失败，不进入交付源码 |
| SQLC-01 | pass | A全部48生产查询；D完整QuotaLedger/Admin/PlanQuota/dispatch/delete/release/sync都走固定sqlc pgx。Ent仅兼容DTO，无同账本第二Ent事务；无关用户/Network Ent保留 |
| SQLC-02 | pass | A六项延迟provider真实PG无持锁并snapshot复核；D真实production pgx构造/audit+事务故障P证明单pgx.Tx原子操作；J/S/W网络位于提交后，原账户/receipt/tenant锁同Tx |
| SQLC-03 | pass | A migrations→sqlc；D Ent/Atlas权威schema.sql→sqlc，固定sqlc1.30.0/Ent0.14.6/Atlas0.36.0与摘要；无手工第二schema，A二次生成无diff |
| SQLC-04 | pass | A/D最终模块make入口实际执行compile、再生成无diff、provenance及非生成生产Go手写SQL扫描；仅可重生生成目录豁免。两仓CI调用同门禁；Acc1d32精确SHA CI已success。此行通过为门禁实现及实际Fedora执行，Gov精确SHA GitHub执行尚待SHIP-03，不将workflow定义冒充已跑CI |
| SQLC-05 | pass | D-up旧populated升级/幂等receipt继续、D-restore失败回滚/恢复、D-fix套餐查询/降额兼容、L旧lab与非GPU累计退款、B-old Network/auth都复用新账本；最后统一源码门禁仍由BASE/SHIP单列 |

## 收口统计与剩余实证

当前 95 行中 **85 pass、10 not_verified、0 fail**：82 项软件必验全部通过，
SHIP-01/02/04 审计、发布与清理通过；SHIP-03按用户明确决定接受不等待CI，保留not_verified；
OWNER/LIVE/DEPLOY 共9项按原需求保持 not_verified。没有删除或降低原验收要求。

本次已用 D-neg、Q、R、O、J/J-race 关闭此前 CREATE-03/04、DELETE-06、
RELEASE-02/03/06、AUTH-03、PROC-02/03/04 的实证缺口；BASE-03/04 与 DOC-01/03
也已有最终模块和文档结果。无需把这些场景重复改成“待测试”。

交付剩余边界：

1. SHIP-03 的 Governance CI 未确认成功，用户已明确接受继续收尾，不再作为本批阻塞。
2. OWNER/LIVE/DEPLOY 九项按原范围保持 not_verified，后续真实 owner、硬件和生产工作另批验收。
3. 最后纯证据提交不修改已受测生产代码；其精确SHA/远端一致性在外置发布回执及完成报告中记录。

软件合同通过不代表正式 owner 或真实 GPU 业务闭环通过。备份、清理、版本和授权CI例外见[收尾记录](release/closeout.md)。

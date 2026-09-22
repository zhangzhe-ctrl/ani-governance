# 恢复与故障报告（recovery）

## 已验证的恢复路径（pass，最终批次全部补齐）

| 项 | 场景 | 观察结果 | 证据 |
| --- | --- | --- | --- |
| FAIL-02 | 占额提交后、首次发送前 SIGKILL Governance（屏障保证 QUEUED\|attempt=0） | 同库重启后同一 operation_id/报文重投，ACKED，无二次扣额 | /tmp/accept-full.log、acceptance.json |
| FAIL-03 | owner 接受后丢 ACK（1ns 超时请求） | 同 ID 同报文重试收敛，provider 单元不重复 | TestSimPG_FAIL03 |
| FAIL-04 | provider 已分配、owner 未写完成的中间态 + 重启 | 新 Simulator.Recover 补记完成，不重复分配 | TestSimPG_FAIL04 |
| FAIL-05 | 单元 2 创建失败 + 单元 1 清理失败 | 释放事实 0 条，占额保留，命令保持 accepted | TestSimPG_FAIL05_06 |
| FAIL-06 | 撤销注入后重放/恢复 | 清理收敛：单元释放、aborted、通知累计值=1 | TestSimPG_FAIL05_06 |
| FAIL-08 | DELETE 第 2 单元释放失败 | 第 1 单元部分退额（occupied 2→1），第 2 继续占额；重放后归零 | TestSimPG_FAIL08_09_16 |
| FAIL-09 | Governance 不可达 | 通知保留 pending 并计数；恢复后同一通知投递成功 | TestSimPG_FAIL08_09_16 |
| FAIL-13 | 退额先于 ACK 到达 | 合法处理；迟到 ACK 不复活占额 | TestQuotaPostgresReleaseBeforeAck |
| FAIL-14 | 全额退额后重放旧创建 | 幂等命中终态，provider 不重新分配 | TestSimPG_FAIL14 |
| FAIL-15 | QUEUED 撤销 vs worker 领取竞争（10 轮） | 只出现"未发撤销（全额退）"或"已领取（attempt≥1，占额保持）" | TestQuotaPostgresCancelClaimRace |
| FAIL-16 | Governance DB 不可用 | Occupy 返回 503 不转发；owner 通知保留待重试 | TestQuotaPostgresStorageUnavailable + FAIL-08/09 |
| FAIL-17 | owner 永久合同错误 | retry_blocked 持额；ResumeDispatch + 撤销注入后同一操作完成清理归零（未直接改余额） | TestSimPG_FAIL17 |
| FAIL-18 | 租约接管后旧 worker 迟到回写 | generation 检查拒绝旧 ACK/UNKNOWN 回写；新 worker ACK 成功且不回退 | TestQuotaPostgresLeaseGenerationGuard |
| CON-06 | 3 并发删除（不同 key）+ ABORT 封闭交叉 | 同一 (charge,ordinal) 仅一个释放事实，收敛后无残留、全额退额 | TestSimPG_CON06 |
| CON-04/05 | 多配额向量原子性 / 双实例并发 | 第二项不足整笔回滚；两实例 20 并发恰收 8 | TestQuotaPostgresMultiVector / TwoInstances |
| POL-01/02/03/05 | 降额、政策变更并发、换套餐/删项、到期后释放 | 见 TestQuotaPostgresPolicyChanges / ReleaseAfterExpiry | 同左 |
| AUTH-05 | 无证书/错误 CA/同 CA 其他 SAN/正确链 | 前三者 Unauthenticated/握手失败；正确证书未知 charge → NotFound | TestSimPG_AUTH05_CertNegatives |

## 稳定标识关联

operation_id / charge_id / resource_id / release_event_id 全部持久化且重试不变；恢复不重新生成 ID（§6.5/§6.6）。owner/provider/账本三方可用 resource_id + operation_id 关联。

## 不变量

每轮套件结束 SQL 重算 `account.occupied_units = SUM(original-released)`，最终 violation_count=0（invariant-report.json）。

## 实现增量（本批补齐时发现并修复）

- 模拟器分配改为单元级独立提交：部分创建故障可跨进程崩溃存活，清理与恢复语义闭合（§11.5）。
- 退额通知事件 ID 按 (charge, 累计值, 原因) 派生：累计增长产生新事件，避免同 ID 异内容冲突；同累计值重发幂等。

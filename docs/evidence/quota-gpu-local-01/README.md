# QUOTA-GPU-LOCAL-01 证据索引

- 日期：2026-09-22（同日第二批补齐残留项）
- 基线：业务源码核对点 5a5d0d8；执行时 HEAD 5c2dd57（仅多出本批计划文档提交，无业务源码差异）
- 范围：docs/quota-gpu-local-execution-plan.md P0～P8，本地隔离环境（真实 PostgreSQL 16 容器、独立进程模拟器）
- 结果边界声明：**通用配额模块及本地持久 GPU 模拟闭环通过指定验收；真实 GPU 服务接入和真实硬件分配 not_verified。**

## 命令

复现模板见 docs/quota-gpu-local-execution-plan.md §15；实际入口 `scripts/quota-lab/run.py`（preflight/prepare/migrate/build/seed/start/accept/collect/stop/cleanup）。

## 结果索引

| 阶段 | 状态 | 证据 |
| --- | --- | --- |
| P0 环境冻结 | pass | versions.lock、p0-scope.md、p0-baseline-*.log（基线测试全绿）、p0-head.txt |
| P1 合同/目录/兼容 | pass | quota_code_map_test、plan_quota 兼容测试全绿 |
| P2 迁移与持久账本 | pass | migration-report.md、p2-db02/03/07-*、p7-mandatory-tests.log（quota_pg 18/18） |
| P3 转发与内部退额 | pass | FLOW-02（ACKED，无二次扣额）、FAIL-02（kill+重启恢复）、BOUND-03 |
| P4 GPU 模拟器与实验入口 | pass | FLOW-01 全链；simulator 集成测试 8/8（含 mTLS 证书负向） |
| P5 正常链/权限链 | pass | acceptance.json：CFG、FLOW、AUTH、CON 全部 pass |
| P6 并发/故障/政策矩阵 | pass | **69 pass / 1 not_verified(REAL-GPU) / 0 fail**；FAIL-01～18、CON-01～06、POL-01～06 全部有真实测试/链路证据 |
| P7 构建边界与回归 | pass | production-boundary.md、p7-mandatory-tests.log、git diff --check 通过、双构建通过 |
| P8 交付清理 | pass | cleanup-report.md、接口登记已更新 |

## 验收矩阵汇总（acceptance.json 最终）

- **pass 69**：第 16 节全部必需项，含此前 not_verified 的残留项：
  - DB-05（运行账号 DDL 拒绝）、DB-06（pg_dump 恢复另一库核对版本/数据/约束）
  - AUTH-04（无政策项 403 / 零额度 409 API 场景）、AUTH-05（无证书/错 CA/同 CA 其他 SAN/正确链）
  - CON-04（多配额向量原子回滚）、CON-05（双实例并发）、CON-06（并发删除+封闭，单一释放事实）
  - FAIL-03～09、13～18（模拟器全链集成 + 账本 PG 测试：丢 ACK 重投、Recover 对账、部分创建/清理失败保留占额并收敛、部分退额、通知重试、终态重放、ResumeDispatch、撤销/领取竞争、DB 不可用 503、租约代次防迟到回写）
  - EXT-01（合成 CONCURRENT 项走同一账本）、POL-01/02/03/05（降额/并发政策变更/换套餐/到期后退额）
- **not_verified 1**：REAL-GPU（真实 GPU 服务接入，固定）。
- **fail 0**。

### 残留补齐的实现增量

- 模拟引擎恢复语义重构：单元级独立提交（部分分配可跨崩溃存活）、AcceptCreate/Delete 重放续跑、清理失败保持 accepted 由 Recover 收敛、通知事件按 (charge,累计值,原因) 派生（累计增长产生新事件，规避同 ID 异内容冲突）。
- 新增测试：`quotalab/simulator/integration_pg_test.go`（8 个真实 PG+mTLS 集成用例）、`data/quota_ledger_pg_test.go` 追加 10 个用例（合计 18）。
- Ent schema 注解补 CHECK 约束 → schema.sql 与迁移一致，已知偏差消除。

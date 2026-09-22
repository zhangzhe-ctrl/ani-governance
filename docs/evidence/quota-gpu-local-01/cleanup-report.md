# 清理报告（cleanup-report）

任务 run-dir：/tmp/quota-gpu-local-01.a1（manifest：env.json、pids.json）

| 资源 | 处置 |
| --- | --- |
| governance / gpu-simulator 进程 | 已停止（run.py stop，SIGTERM 进程组） |
| quota-gpu-local-01-redis 容器 | 已按 manifest 清理（label 校验通过） |
| quota-gpu-local-01-pg 容器 | **保留**：早于 run.py 创建（无 label），按禁止范围清理原则未删除；内含任务库 governance/gov_upgrade/gov_bad 等，可 `docker rm -f quota-gpu-local-01-pg` 手动移除 |
| 机密文件（admin-password、control-token、access-key-encryption、JWT key、证书私钥） | 位于 run-dir/secrets 与 run-dir/certs，run-dir 位于 /tmp；已随任务结束说明保留/删除责任，未提交仓库 |
| 数据库 | governance/gov_upgrade/gov_bad 等任务库随容器删除；无任何真实业务库被连接 |
| 仓库内证据 | docs/evidence/quota-gpu-local-01/ 全部脱敏：不含 JWT、密码、完整 DSN、私钥；日志仅保留摘要 |

未做：不自动 commit/push；复现请按 docs/quota-gpu-local-execution-plan.md §15 模板（run.py 会重建容器与数据库）。

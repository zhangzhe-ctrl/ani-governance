# Ubuntu kind Governance 重新初始化记录（2026-09-21）

目标主机：`ubuntu`（139.198.29.154）；集群 context：`kind-kind-test`；命名空间：`ani-system`。本轮仅调整 Governance，未调整前端、Network 服务或其数据库。

## 当前部署

- 代码基线：`2619764`，加上本轮发现的租户 `plan_id` 读取修复。
- 镜像：`ani-governance:0.1.0-2619764-planfix`。
- 镜像 ID：`sha256:0f50a5a9ebe9743ec93f2ee73b77ce7d89208571a0dc0cb1dd13697337b85f85`。
- 使用新 ConfigMap：`governance-configs-2619764`，`migrate: false`；配置新增 TCP readiness probe，监听端口为 7788。
- 新数据库：`gwa_init_20260921`；保留原数据库 `gwa`。结构通过 Atlas 应用版本 `20260921134442`，数据通过 `admin init` 显式初始化。
- 新 Redis 会话/缓存使用 DB 14，Asynq 使用 DB 15；使用前确认两者为空，没有清空原 DB 0/1。
- 已重新生成本环境 JWT RSA 密钥，旧会话不能在新环境复用；保留既有下游 mTLS 材料和地址配置。
- HTTP Service：`ani-gateway`，NodePort `30082`，服务端口 `8080` → 容器 `7788`；SSE NodePort 仍为 `30083`。

## 账号与调用

| 账号 | 用户名 | 租户编号 | 登录接口 |
| --- | --- | --- | --- |
| 首平台管理员 | `admin` | 不传 | `POST /api/v1/auth/platform/password/login` |
| 租户管理员 | `admin` | `kindtest` | `POST /api/v1/auth/password/login` |

租户“Kind 测试租户”绑定“基础管理”套餐，包含 DASHBOARD/OPM。账号通过 IMMEDIATE 直接激活，无需邮件邀请；两个账号使用不同随机密码。

密码只保存在 Ubuntu 的权限为 0600 的文件中，不进入仓库或测试输出：

```bash
ssh ubuntu 'cat /home/ubuntu/Workspace/.codex-runs/governance-kind-reinit-20260921/credentials.json'
```

验收使用 Ubuntu 上 `http://127.0.0.1:30082`，经过实际 Docker 节点端口映射和 kind NodePort，没有用独立服务替代 Kubernetes 部署。外部地址是 `http://139.198.29.154:30082`；本执行端的公网直连探测未成功，不能据此声称公网访问已验收。需要从本机安全访问时，可使用已有 SSH 入口：

```bash
ssh -N -L 17788:127.0.0.1:30082 ubuntu
```

然后请求 `http://127.0.0.1:17788`。登录 `password` 直接传用户输入，不传 AES/Base64；部署入口未新增 HTTPS，此次原始密码请求只通过 Ubuntu 本机回环 NodePort 验证。

## 验证结果

| 验证 | 结果 |
| --- | --- |
| Atlas 初始化、admin 初始化与 preflight | PASS |
| 重跑 admin init 不覆盖已有初始化数据 | PASS |
| 平台原始密码登录、无需验证码、HttpOnly refresh Cookie | PASS |
| 错误密码 | PASS，401 |
| 平台个人资料、初始菜单和权限、单独菜单/权限接口、套餐列表 | PASS |
| 基础套餐租户的个人资料、非空菜单/权限、用户列表和角色列表 | PASS |
| 租户访问平台套餐列表 | PASS，403 |
| refresh Cookie 轮换 | PASS，旧访问令牌与重复使用的刷新令牌均被拒绝 |
| 租户登出授权、清 Cookie、吊销该用户全部会话 | PASS，两个会话及刷新令牌均失效 |
| 平台登出与旧令牌失效 | PASS |
| 服务重启前后初始化/业务数据 | PASS，17 张表的行数与逐行内容摘要一致，没有启动刷新 |
| 浏览器/前端、外网 HTTPS、邮件、Network 业务链路 | not_verified，本轮未调整前端或下游业务 |

首次严格验收发现：`sys_tenants.plan_id` 已有值，但 Ent 仅把它作为未公开的边外键，通用 DTO 映射拿不到它，租户菜单过滤误判“无套餐”。本轮在 Ent schema 中将现有 nullable 外键显式绑定为字段，重新生成 Go 代码；租户详情和列表可正确返回 `planId`，无需变更已有数据库结构。Atlas schema diff 输出 `Schemas are synced, no changes to be made.`。`TestTenantRepoSqlite_`、`TestAuthSvcSqlite_`、`TestTenantServiceSqlite_` 定向回归通过。

初次切换镜像时还观测到 Pod 已 Ready 但服务尚未监听的短暂连接重置，已补 TCP readiness probe，并在接口验收前等待实际端口就绪。

## 证据与恢复

远端私有目录：`/home/ubuntu/Workspace/.codex-runs/governance-kind-reinit-20260921/`（含凭据与配置，不要整体公开）。

- 原数据库备份：`gwa.before.dump`，原部署和配置：`deployment.before.json`、`config.before.json`。
- 初始化：`atlas-apply.log`、`admin-init.log`。
- 修复验证：`navigation-probe.log`、`tenant-regression.log`、`service-regression.log`、`schema-diff.log`。
- NodePort 验收：`http-acceptance-final.log`、`http-results.json`；保留最初失败的 `http-acceptance-initial-failed.log` 与启动窗口失败记录。
- 重启数据对照：`startup-before.json`、`startup-after.json`、`startup-check.log`。
- 本次测试脚本：`accept.py`、`startup-check.py`；不会输出账号密码或访问令牌。

原数据库和旧配置均保留。需要恢复到原部署时，在 Ubuntu 上显式执行：

```bash
kubectl --context kind-kind-test -n ani-system patch deployment ani-governance \
  --type=json --patch-file /home/ubuntu/Workspace/.codex-runs/governance-kind-reinit-20260921/rollback.patch.json
kubectl --context kind-kind-test -n ani-system rollout status deployment/ani-governance --timeout=60s
```

该补丁恢复原 Pod 模板，从而恢复旧镜像、旧数据库、旧 Redis 和旧 JWT 配置；不会删除新库。恢复命令已准备但未执行，不声称回滚实测通过。

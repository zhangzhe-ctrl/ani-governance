# GOV-ACC-V12-01 合入 main

2026-09-24，用户另行明确授权“两个项目的推送，都合到主分支”。此前不合并 main 的执行边界至此被此指示替代；仍不部署生产。GitHub CI 延续用户“不等待”的决定。

## 合并输入与处理

- Governance main：`00838f4f73603637ba1eecd1f67c72c25a0b0191`，比原任务基线新增 OPA 移除和审计 geo_location 字段删除。
- Governance 本批分支：`1deb8bcefa7ce07e04121563b25006fd7cecfa95`，实现提交 `bd9ad1a`，保留原验收历史。
- Accelerator：`1d32dd9a9173b8869fa0ef2ae64e20b88f2ca0a3` 原样创建并推送 main；远端此前只有两个任务分支。没有重写既有分支历史或修改已验收 Acc 源码。

Governance 保留双方修改：Casbin/noop 边界、移除 OPA 依赖、四个审计字段的删除，与完整 pgx/sqlc 配额实现、GPU 能力和权限撤销校验同时存在。

三个冲突分别处理：接口登记保留两边完整功能组；schema.sql 从合并后的权威 Ent schema 和 constraints.sql 在 Fedora 重生成；atlas.sum 用固定 Atlas v0.36.0 按完整迁移链重算。没有手改生成代码。Ent v0.14.6、sqlc v1.30.0 同步重生成，sqlc 模型相对本批分支只去掉四个已移除审计字段。

`scripts/ci-quota-pg.sh` 补上 main 的 `20260923143455_drop_geo_location.sql`，隔离 PG 显式执行 GPU 三迁移及该删除迁移，保持测试数据库与合并后的权威 schema 一致。没有更改历史迁移内容；Atlas 顺序校验和重新生成不是重写迁移。

## 验证与资源边界

运行位置为 Fedora `/home/chabking/gov-acc-main-merge-20260924`，新任务私有缓存，`GOWORK=off GOMAXPROCS=2 GOFLAGS=-p=2 GOPROXY=https://proxy.golang.org,direct`。独立 Podman PostgreSQL 16.10 容器 `gov-acc-main-merge-pg`，只绑定回环 25434，不重用已关闭的上一批数据库。

最终运行、退出码和清理结果见本目录追加回执。合并验证只证明合并后的代码/数据回归，不把历史 A/B/C 日志改称在新 main 上重新执行了完整硬件或 owner 验收。

本次 `make verify-gpu` 的生成/格式、三构建、data/service/server/pkg 普通回归、PG data（6.436s）/套餐 service（0.123s）、data race（54.571s）/service race（1.159s）及8项lab（7.030s）均通过。该次整体退出2：审计阶段发现 main 删除 OPA 配置后 `auth.yaml` 的旧样例文件校验锁不匹配，原日志保留在 `verify.log`，没有改称整体 exit 0。

经核对仅为 main 已接受的 OPA 配置删除/注释变化，两段 PEM 测试密钥逐字完全相同；仅更新 `scripts/gpu-audit-fixture-sha256.txt` 中该文件摘要，不扩大 secret 例外。`fixture-lock-update.json` 记录旧/新文件摘要及密钥未变断言。随后 `make verify-gpu-audit` 重跑 exit 0：0 可达漏洞，另有1个导入包/8个所需模块的未调用告警；gitleaks 0发现。没有因校验锁修正重复已通过的业务测试。

因此本轮全部合并门禁项通过，证据分别对应原运行与修复后审计。`source-parity-final.json` 与 `source-final.sha256` 证明1744个非文档文件与Fedora受测目录逐字一致；后添加本记录和日志不改变受测业务源码。

所有生产部署和已有数据库升级仍另行审查。已应用 main 较晚迁移的数据库须核对真实 Atlas 历史，不能把两个分支的时间顺序自动当成已部署库可直接升级的证明；本次只显式迁移新的隔离测试库。

Accelerator GitHub 默认分支原指向 `codex/accelerator-v1.2`。main 代码已发布，但现有 GitHub CLI 凭据无效、浏览器未登录，默认分支设置未变更；Git SSH 推送权限不等同仓库设置 API 登录。

验证资源已清理：专属 PostgreSQL 容器及匿名卷、私有模块/构建缓存、lab 证书已移除。受保护的数据库备份保留于 Fedora `/home/chabking/gov-acc-main-merge-20260924/recovery/cluster.sql`，权限0600；源码与证据保留。独立复核见 `cleanup-result.json`，备份摘要见 `recovery.sha256`，数据库备份内容未入库。

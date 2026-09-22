# AK/SK 与 Network VPC 实际验收

2026-09-22，本批实现及必要验收 **pass**。120 个按用例名汇总的最终结果见 [acceptance.json](acceptance.json)，每次执行（含修正前的失败）见 [acceptance.jsonl](acceptance.jsonl)。只查询真实 PostgreSQL fixture，不代表云网络创建或网络数据面验收。

## 版本、制品与环境

| 项目 | 实际值 |
| --- | --- |
| 执行主机 | `ssh ubuntu` / `i-8yg2l7u8` |
| 集群 / namespace | `kind-kind-test` / `aksk-vpc-20260922`，独立 PostgreSQL 18、Redis 7、PVC、角色、证书和 Secret |
| Governance 输入 | `5a2a2e8c6c303a280acf2f6168d63fcebca8990c` + 本批源码；最终本地提交见 Git 历史 |
| Network 输入 / 提交 | `66f787bd30134141726c596612501a83cf75bdb7` + 按文件整合 `9e56e1c` 必要内容 + 双 actor；本地提交 `3e40bb0c4ba0a5375c120f3a87f9579084e766eb` |
| Governance 镜像 | `ani-governance:aksk-20260922-06d94eac1f25e7d7` |
| Network 镜像 | `ani-network-service:aksk-20260922-f3c503a6c8a4bce6` |
| NodePort | `http://172.18.0.2:30188`（在 ubuntu 上真实调用） |
| 远端任务目录 | `/home/ubuntu/workspace/aksk-vpc-20260922` |
| 工具链 | Go 1.26.7、gow v1.0.3、Buf 1.60.0、Ent v0.14.6、Atlas v1.3.0、gnostic OpenAPI v0.7.1；其他插件版本由 `api/buf.aksk.gen.yaml` 固定 |

[构建镜像 ID](built-images.txt)、[运行 imageID](images.json)、[二进制 SHA256](binaries.sha256) 均已记录。`collect.py` 在实际 Pod 内执行 SHA256，对照构建的 Governance/server、admin、Network 三个制品；同时逐文件比较最终应用源码与已构建快照，均一致。[源码完整 SHA256 清单](source-manifest.json) 同时保留于远端 `evidence/source-manifest.json`，摘要也写入 acceptance.json：

- Governance：`e5b7e80e26aecbfd1d7ff5f6c7dc8d4654ade2b5e13e136acacfaa2c9e4752c3`
- Network：`9252fa1336b1f91fae09fb4b2ce7b76d1f25291047624fb9a945d69f6980163c`

源码清单包含相关 API/应用/存储源文件；后续证据文档的本地提交不会改变上述应用输入。没有 push，没有覆盖共享 ani-system/ani-network 部署。

## 实际验收

| 范围 | 结果及证据 |
| --- | --- |
| 固定向量与窗口 | Go `TestVPCSignatureFixedVector` 与 Python 得到合同同一签名；Go 受控时钟 ±300 接受、±301 拒绝；Python 离线四用例见 [python-offline-tests.log](python-offline-tests.log) |
| 空库闭环 | 显式 Atlas apply → admin init/check → 平台 API 创建套餐、租户/管理员、角色 → 租户登录 → 创建 Key **201** → Python NodePort → Governance → Network mTLS → [持久化 VPC 对照响应](vpc-a.json) |
| 管理协议 | snake_case、数字 ID/role_id、真实 201、删除 200/status、FieldMask lowerCamel 解码与有效期清空；HTTP adapter/Service/Repo 测试及真实 HTTP 均通过 |
| 签名负向 | 错 SK/签名、路径/时间篡改、过去/未来窗口、三个头各自缺失/重复/逗号合并、混用 Bearer、query/body 拒绝；窗口内重复读允许 |
| 权限/隔离 | 无权限、无 NETWORK 套餐、非启用/跨租户角色、租户 OFF、Key 调用户接口/Key 管理拒绝；A→B 与不存在同样 404（动态 request_id 不参与等价比较）；伪造公网租户/actor 无效；管理跨租户 GET/PUT/DELETE 均 404 |
| 生命周期 | 停用/启用、删除、到期/清空、改绑无权限角色/恢复、重置旧 SK 拒绝新 SK 成功、角色停用立即拒绝；错误签名+停用角色仍先返回 401 |
| mTLS | 实际 Network Pod 正确 user/Key actor 成功；缺证书、同 CA 错服务 SAN 证书拒绝；RPC/header tenant 不一致 PermissionDenied。每个负向用例使用独立 port-forward 并先跑成功对照 |
| 用户回归 | 新平台/租户用户登录，用户 JWT 查询 VPC，平台/租户登出及旧 JWT 401；Key 不因用户登出冒充会话或被当成用户会话 |
| 密钥/审计 | 实库仅密文；缺主密钥启动拒绝；空/明文/坏密文均 503、无降级；列表/详情和各次重启前后日志/审计无真实 SK/完整签名。Key subject_id 明确、user_id 为空；错误签名不记为已认证主体；[主体统计](audit-subject-counts.json) |
| 数据约束/启动 | PostgreSQL 跨租户复合 FK、引用角色删除 RESTRICT 拒绝；运行角色没有 DDL；重启前后结构与 11 张初始化/业务表摘要一致，见 [restart-snapshot.json](restart-snapshot.json) |

Go 定向包及编译：[key-tests-retry.log](key-tests-retry.log)、[key-final-build.log](key-final-build.log)。完整加密/认证/审计小包与新用户登录回归：[key-regression.log](key-regression.log)。Network 定向测试见关联仓库 `docs/execution/records/governance-aksk-vpc-20260922/`。没有默认跑全仓重型套件。

Atlas 的最终声明式结构由 Ent 生成表 + `cmd/schema` 的复合外键导出为 `app/admin/service/schema.sql`，部署不需要 Go。实际对比没有本批 AK/SK、审计或复合约束差异；[schema diff](key-schema-diff.log) 仅列出实施前遗留的 files/avatar 差异，未执行这些 DROP。

## 复现与访问

完整源码内命令及受限凭据调用示例见 [实验 README](../../../scripts/aksk-lab/README.md)，管理合同见 [执行文档](../../aksk-vpc-execution-plan.md)。远端阶段：

```bash
cd /home/ubuntu/workspace/aksk-vpc-20260922/governance
python3 scripts/aksk_vpc_client.py --self-test
# 仅用于新的独立实验；已有本次namespace会被prepare拒绝覆盖。
python3 scripts/aksk-lab/lab.py prepare
bash scripts/aksk-lab/build-images.sh
python3 scripts/aksk-lab/lab.py deploy
python3 scripts/aksk-lab/lab.py setup
python3 scripts/aksk-lab/lab.py contract
python3 scripts/aksk-lab/lab.py mtls
python3 scripts/aksk-lab/lab.py final_checks
python3 scripts/aksk-lab/lab.py security_checks
python3 scripts/aksk-lab/collect.py
```

当前环境已经完成这些阶段，不要重跑 prepare/deploy/setup。需要本机访问可显式 SSH 转发：

```bash
ssh -N -L 17788:172.18.0.2:30188 ubuntu
```

然后使用 `http://127.0.0.1:17788` 与明确的 `ANI_ALLOW_HTTP_FOR_TEST=1`。未建立公网 HTTPS，也未验证公网直连。所有真实凭据只在远端 `private/`（0700/0600）与 Secret；不要打印、提交或复制到文档。实验账号名为 `admin`，租户 code 为 `aksk-a/aksk-b/aksk-empty`；密码与最终 Key 从受限状态文件注入客户端，脚本只输出 VPC。

## 执行中修正与范围

保留失败记录，没有将其抹成从未失败：初期固定 digest 镜像拉取超时后复用已加载且已核验的节点镜像；Dockerfile 修正本地基础镜像引用；测试辅助函数更正 viewer API；结构开发库复验纠正首次 SQL 中无关 MFA 字段误改后才应用目标库；角色配置改为有权限的平台用户；Pod Ready 后等实际 NodePort；404 比较排除动态 request_id；TLS 拒绝可能结束 port-forward，因此逐例重建并加成功对照；PostgreSQL 18 的 RESTRICT 拒绝按其实际 SQLSTATE 处理。最终结果均重新验证通过。

本批必要事项无未完成项。云网络创建/数据面、其他业务 API、前端、公网 HTTPS、独立 IAM、存量迁移和回滚演练不在本批验收范围；对应状态为 not_verified，不作生产完成声明。

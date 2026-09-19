# Governance 入口网关的 mTLS 配置

Governance 兼任所有服务的外部入口网关：验证用户登录、租户、套餐和接口权限，
再用自己的服务身份调用对应领域服务。现有 Model 客户端与业务代码全部复用。
本目录统一维护 Governance 的客户端证书申请与部署挂载，目前已接入
Governance → Model，不表示其他服务已经全部接入。

保留一份 `certificate.yaml` 和一份 `deployment-patch.yaml`。以后接入更多服务，
在同一份部署补丁中补充客户端配置；同一信任体系下复用证书和 CA 挂载，
不按下游服务新增 patch。

## 部署

在远程 Kubernetes 管理节点执行；本地不编译、不运行集群命令。
前提是 cert-manager 已安装，命名空间内存在 Model 所信任 CA 对应的
`Issuer/ani-internal-ca`。公共签发机构由基础设施部署（ani-installer）负责，
本目录不安装 cert-manager、不生成或携带 CA 私钥。
若平台提供 ClusterIssuer，修改 certificate.yaml 的 issuerRef 名称及 kind 即可。

配置适配现有 Deployment/governance 和容器 governance；其他部署名需对应调整。
`ANI_MODEL_ADDR` 保留当前配置，新部署应先设置为 Model 的集群服务地址和端口。
进入 `scripts/deploy/governance-mtls/`，把平台提供的 **Model 信任根公钥 PEM**
保存为 ca.pem 后执行：

```sh
NS=gov-model-20260919-01  # 替换为目标命名空间
kubectl -n "$NS" create configmap governance-model-ca \
  --from-file=ca.crt=ca.pem --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$NS" apply -f certificate.yaml
kubectl -n "$NS" wait --for=condition=Ready certificate/governance-client --timeout=60s
kubectl -n "$NS" patch deployment governance --type=strategic --patch-file deployment-patch.yaml
kubectl -n "$NS" rollout status deployment/governance --timeout=60s
```

这两份声明应随 Governance 部署保留；重放旧 model-lab 部署脚本后必须重新应用此
补丁，或将补丁合入实际部署清单，否则旧脚本会恢复旧证书路径。
客户端在启动时读证书。cert-manager 更新 Secret 后，执行
`kubectl -n "$NS" rollout restart deployment/governance` 并等待 rollout 完成，
随后查询模型列表。自动重载与根 CA 轮换不属于本次实现。
证书自动续期不延长根 CA 的寿命；短期实验 CA 不作为正式环境长期签发机构。
机制参考：https://cert-manager.io/docs/usage/certificate/

## 一次请求如何走

1. 浏览器/外部客户端调用 Governance 的 `GET /api/v1/models`。
2. Governance 完成登录、租户、套餐及接口权限检查，从可信上下文获取用户与租户。
3. 现有 ModelClient 用 `ani-governance` 客户端证书连接 Model，验证 Model
   的 CA 和固定身份 `ani-model-service`；重新构造内部用户/租户 metadata，
   不透传外部伪造身份头。
4. Model 验证客户端证书、调用方身份和允许的 RPC，然后按可信租户读取自己的数据库。
5. Governance 将结果转换成已有 HTTP 返回格式交给客户端。

证书证明调用者是哪个服务，不代替用户权限和租户数据隔离。
外部 HTTPS 使用的服务端证书与这里的内部客户端证书是两件事。

## 如何接入其他服务

- Governance → 新服务：在 Governance 加该服务客户端及 HTTP 入口，登记现有权限和
  套餐关系；配置目标地址、目标 CA 和服务端身份。同一信任体系下复用
  `governance-client-tls`，无需每接一个服务再给 Governance 签一张证书。
- 新服务自己的部署申请服务端证书，挂载自己的私钥和客户端信任 CA；接收端明确允许
  `ani-governance` 调用哪些 RPC，从可信调用中获取租户并限定数据库查询。
  不能只因证书由同一个 CA 签发就放行所有服务或所有方法。
- Inference → Model 等内部调用仍直接调用目标服务，使用 Inference 自己的证书，
  不借用 Governance 的证书，也不因为 Governance 是外部入口就绕行它。
- 每接一个接口，只验证真实成功、未授权拒绝、跨租户不泄露；复用现有登录和权限机制。

## 回退

部署前记录 Deployment revision。若切换失败，用 `kubectl rollout undo deployment/governance
--to-revision=<原 revision>` 恢复原挂载和环境变量，并等待 rollout 完成。
旧 Secret 在本次切换中保留；切勿在确认新链路成功前删除。

## 本次实际验证（2026-09-19）

- Fedora：`/home/chabking/ani-governance-runs/cert-manager-20260919`。
- Kubernetes：ani-test-1～3；namespace `gov-model-20260919-01`。
- 复用已安装的 cert-manager，把原实验 CA 导入该 namespace 的
  `Secret/ani-internal-ca`，创建同名 CA Issuer；没有更换 Model 信任根，
  没有从 Inference 实验中复制证书或 CA。
- 该导入是本次旧实验迁移操作，不是 Governance 业务职责，也没有声称已完成
  ani-installer 的正式公共 CA 交付。正式部署接平台提供的签发机构即可。
- 原 CA 到期时间为 **2026-09-25 17:53:54 UTC**；不可将此实验 CA 作为长期部署配置。
- 新证书 SAN 为 `ani-governance`，EKU 为 TLS Web Client Authentication；
  有效期至 2026-09-22 10:51:11 UTC。旧 Deployment revision 为 17。
- [必要验收结果](checks.json)：真实登录、两个租户的返回与实际 PostgreSQL 数据逐项
  比对、未登录 401、无权限 403（均未进入 Model 业务）、伪造租户头无效、
  无证书/错误服务证书 TLS 拒绝、最终读成功，全部 pass。
- [Network 回归](network-check.json)：真实登录后 VPC 查询与数据库字段比对通过。
- 本次仅改部署声明与验收脚本，沿用当前镜像，无 Go 改动，因此没有重新编译。
  Python 语法与声明接线检查在 Fedora 执行；未运行全量测试。
- 自然续期后自动重载、根 CA 轮换、所有其他服务推广：`not_verified`。

重跑必要验证（Fedora，使用原任务私有账号及数据）：

```sh
python3 scripts/model-lab/check-cert-manager.py \
  /home/chabking/ani-governance-runs/model-list-bootstrap-20260919-01 \
  /home/chabking/ani-governance-runs/cert-manager-20260919/evidence/checks.json
```

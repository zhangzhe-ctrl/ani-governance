# Network 多监听器 固定规格和 VPC 预设修改计划

日期：2026-10-10。目标文件已授权实施、生成、定向验证、独占测试库和 10～12 任务隔离实例验收。用户随后明确禁止修改前端代码，本次范围收敛为后端；优先跑通业务主流程，再针对相关断点完成必要验证。不提交、推送、创建 PR、合并或正式业务部署。

## 目标与范围

交付三条可运行主链：

1. 用户选择固定规格并创建多监听器 LB，经 Governance 身份与权限检查、Resource 持久化和 worker 推进，形成一个 Gateway 及各监听器的转发配置；不同端口分别访问其独立后端，更新和删除保持资源身份与占用正确。
2. 用户只能选择部署预设中的 VPC CIDR；经相同身份链和后端白名单、平台网段冲突检查后持久创建 VPC，并能正常在该 VPC 内划分 Subnet。
3. 使用平台已部署的固定 small、medium、large 三档 EnvoyProxy/GatewayClass，租户选择准确保存到数据库并映射到对应部署配置。

多监听器先扩展现有 HTTP 协议和默认 `/` 转发规则，包含创建后增删监听器、修改端口、后端和健康检查。HTTPS、证书、TCP/UDP/TLS、复杂路由、在线改规格、动态规格、CIDR 自定义和自动分配不自动纳入本批。若用户要求增加协议，先补充其具体合同和必要改动。

Subnet IP 统计沿用上一轮问题结论，本批不实施；它不阻塞上述三条主链。保留现有三种 LB 入口 private、public、public_private 的兼容性，测试按实际改动选择，不展开完整环境组合矩阵。

## 固定输入与当前断点

| 仓库 | 本次检查的 main | 当前断点 |
| --- | --- | --- |
| ani-governance | `dd1e772ac5d0718c00a3cc197c9e750c0b9fe896` | LB 输入、响应和 BFF 映射只有单 listener；缺少 VPC 预设查询入口 |
| ani-resource-service | `6690df41110af98b91909c9f5bbb78b00805b249` | 单监听器数据关系；Gateway 被视为不可变；flavor 仅 small 且创建 SQL 未写 flavor；VPC 仅校验通用私网 CIDR |

用户后续明确禁止修改前端。本批修改范围为 Governance、Resource，不包含前端或 Installer。实施从这些仓库届时最新 main 的任务专用工作树开始，保留无关修改；若基线变化，只核对本计划涉及的接口、配置和行为差异。后端验收使用真实 Governance HTTP 入口，不以旧 ani-console 的 Core 页面证明当前链路。

固定规格材料位于 `/home/chabking/下载/loadbalancer-doc-dev/loadbalancer/examples/` 的 `envoy-proxy.yaml` 与 `gatewayclass.yaml`，用于核对固定参数和 Class 映射，下载目录不成为运行依赖。Installer main `78d8418307b6156954feb10aee8d6972d2ff1bbd` 的 Envoy 模板仅覆盖 small-noeip，这是部署材料的来源参考，不能据此认定 10～12 现网缺少三档预置，也不据此扩大本批修改范围。KCN 对照为本地 main `2b9467c66dc9024fbf321a4b3a98a9e5f3fef377`，远端最新版本和 10～12 的实际部署版本在实施前核对。

## 多监听器合同与修改位置

一个 LB 共用 Gateway、入口类型、型号、VIP/EIP 和父 VPC/Subnet。每个监听器拥有稳定 `id`、唯一且稳定的 `name`、`protocol`、`port`、独立后端集合和健康检查。`name` 对应 Gateway listener name 与 HTTPRoute `parentRefs.sectionName`；每个监听器生成自己的 HTTPRoute 和 BackendTrafficPolicy。名称、数量和端口限制按实际安装 CRD 校验，现有无 hostname 的 HTTP 模型要求端口不重复。

创建时分配稳定身份；更新沿用现有 LB `expected_version`、`idempotency_key`、desired/applied version 和单 LB lease。更新集合明确区分遗漏、替换与清空：遗漏保持原集合，提供集合表示整体替换，空集合拒绝；删除最后一个监听器应走 LB 删除。保留的监听器不更换 ID/name。

Proto 追加集合字段，保留旧单 listener 字段及其编号。旧请求规范化为一个 `http` 监听器；同时提交旧字段与新集合时拒绝歧义。保留旧受理回执、旧 fingerprint 与历史重放，不能批量重写幂等记录或让升级后的重试失效。

| 所有者与关键文件 | 必要修改 |
| --- | --- |
| Governance：`api/protos/catalog/service/v1/vpc.proto`、`app/admin/service/internal/service/network_service.go`、`app/admin/service/internal/data/network_client.go` | 集合输入及响应映射、Create/Update/Get/List；保留可信租户/actor、返回归属校验和现有 HTTP 路径 |
| Resource：`api/network/v1/load_balancer.proto`、`internal/service/network/load_balancer.go`、`internal/biz/network/load_balancer.go` | 集合 DTO、每监听器校验、后端归属、健康参数、稳定身份及幂等规范化 |
| Resource：新增迁移、`internal/data/network/queries/load_balancers.sql`、`internal/data/network/load_balancers.go` | 配置版本到 listener、成员、健康参数的关系；listener 与组件唯一键加入监听器身份；保留 tenant 复合外键，回填旧配置并保留已有 ID、CR 名称和 UID |
| Resource：`internal/biz/network/load_balancer_worker.go`、`internal/data/network/load_balancer_worker.go` | Gateway 目标版本随 listener 配置推进；纳入监听器退休、共享 Backend 引用及失败恢复 |
| Resource：`internal/data/network/kc_load_balancer.go`、`kc_load_balancer_generated.go` | Gateway 集合和每监听器 Route/Policy；允许 listeners 的 UID/resourceVersion CAS 更新；核对完整 Service/EndpointSlice 端口集合及 owner 链 |
| 租户前端现有 LB 表单和详情 | 多行监听器及独立后端/健康检查，增删改与逐监听器展示；只修改实际使用的页面和客户端 |

Gateway 更新仅开放本批监听器变化，Class、网络归属及 VIP/EIP 等继续受原守卫约束。新增沿 Backend → Gateway listener → Route/Policy → 观察确认推进。删除先撤除对应转发，清理 Route/Policy 和 Gateway listener，确认端口撤除后释放不再被任何监听器引用的 Backend/子网占用；不能删整个 Gateway 来删除一个监听器。

全部目标监听器确认后才提升 applied_version。部分失败保留原 applied 状态并继续恢复；一个监听器的后端失效只撤除对应转发，不误删其他监听器配置。配置观察与逐端口真实流量验收分别报告。

## 固定三规格

三型号数值直接沿用附件，不新增规格管理或用户自定义入口。

| flavor | 副本 | 每副本 requests | 每副本 limits | 公网 Class | 私网 Class |
| --- | ---: | --- | --- | --- | --- |
| small | 2 | 1 CPU / 1Gi | 2 CPU / 2Gi | lb-small | lb-small-noeip |
| medium | 4 | 2 CPU / 2Gi | 4 CPU / 4Gi | lb-medium | lb-medium-noeip |
| large | 6 | 4 CPU / 4Gi | 8 CPU / 8Gi | lb-large | lb-large-noeip |

Resource 对 `flavor` 使用固定白名单，空值兼容为 small。追加迁移调整旧 CHECK；创建源 SQL、sqlc 参数及仓储必须显式落库 flavor。适配器按 `(flavor, exposure)` 映射 Class：private 使用 noeip，public/public_private 使用公网变体。能力核查按固定部署材料验证所需 Class/EnvoyProxy 的身份、接受状态和参数，更新实际安装指纹，不将 small 参数套到其他型号。

`kc_load_balancer_generated.go` 的实际 Deployment/Pod 观察也必须改为按选定 flavor 核对：当前副本、updated/available replicas、Ready Pod 和 EndpointSlice Pod 数均固定为 2，requests/limits 固定为 small。Class 选择、安装能力检查和运行时观察共用同一份固定规格值，避免 medium/large 配置已生成却始终无法 configured。

在 10～12 上只读核对已有三档预置及所需入口变体，使用其固定 Class/EnvoyProxy。若所需对象缺失或参数不符，报告具体环境前置缺口，停止依赖该预置的真实环境验收；本批不补装、重建共享对象或修改 Installer。Governance 沿用已有 flavor 字段；本次不修改前端。规格只在创建时选择，本批不增加在线切换。

## VPC 预设与平台网段检查

预设由部署管理员配置在当前集群的 Resource 服务启动 YAML 中，拟新增字段 `network.vpc_cidr_presets`（字符串数组）。当前 main 尚无该字段；在 `internal/conf/v1/conf.proto` 的 `Network` 中追加字段，沿用现有 `-conf` 文件/目录加载入口，默认目录为 `configs`，仓库示例为 `configs/config.yaml`。`cmd/ani-resource-service/app.go` 和 `governance.go` 将加载并校验后的预设传入 Network 用例，Governance 经 Resource RPC 获取，不另存一份预设，不在前端写死选项。

以下仅示意配置结构，示例网段不是现网最终值：

```yaml
network:
  vpc_cidr_presets:
    - "10.64.0.0/16"
    - "10.65.0.0/16"
```

保留少量固定、规范的 RFC1918 CIDR，禁止租户自行填写；具体几项值在核对 10～12 的有效站点配置和实际平台网段后冻结。静态配置验证非法 CIDR、非规范网段及重复项；配置缺失或为空时不提供候选，新 VPC 首次受理拒绝，既有 VPC 与已受理请求的幂等重放保持原合同。预设不新增管理页面、管理表或写入接口。

配置沿用现有服务配置交付方式，修改后重启/滚动 Resource 生效，本批不增加热更新、不扩展 Installer 的输入或模板。Resource 仓库当前只提供 YAML 示例和 RBAC 材料，没有完整服务 Deployment/配置挂载模板；10～12 的实际配置文件、挂载来源和启动参数在实施前只读核对，不能把仓库示例路径当作现网挂载路径。

增加租户读取预设的 RPC 和 HTTP 入口，拟为 `GET /api/v1/networks/vpc-cidr-presets`；返回上述配置中通过当前平台网段冲突检查的可选 CIDR，不返回平台私有拓扑。前端只提供下拉选择。CreateVPC 沿用现有 `cidr` 字段，首次受理要求精确匹配同一份配置白名单并再次确认无冲突，不能只做前端限制。展示和创建复用同一检查逻辑。

冲突范围包含有效 Pod/默认子网、完整 Service CIDR、节点物理及管理网段、平台地址池和明确声明的机房/VPN保留网段。复用现有 KCN/NodeFacts 和平台 pool 查询；补齐网卡前缀及待创建、删除中但未释放的 pool。`intranetNetworks` 是路由目的范围，不能把聚合路由直接当作整个范围已分配。无法确认必需网段事实或观测过期时拒绝新受理，不新增服务启动时必须在线访问集群的前提。

配置与平台观测检查在外部读取阶段完成；写事务沿现有 cluster 锁再次核对 ANI 管理的地址池，平台池准入也使用对应互斥检查，防止之后占入预设或现有 VPC。外部 Kubernetes 配置不与 PG 事务原子提交，Provider 首次创建前复核相关事实，不承诺冻结外部管理员变更。

白名单校验只放在 VPC 首次受理入口，不放入 Subnet 也调用的 `NewVPCIntent`。旧幂等键重放返回已受理结果；新键执行当前策略。不同隔离 VPC 可以复用预设，不增加 CIDR 全局唯一约束。现有 VPC 表的 cidr 足够保存选择，无需预设管理表。

Resource 改动限配置 Proto/验证/装配、预设用例与平台网段适配、VPC/平台池准入及必要查询、network Proto/service 和 Governance 调用 allowlist。Governance 改动限 DTO、BFF/client、HTTP 路由、请求/机器身份白名单及 API 权限/NETWORK 套餐登记；在现有登记中追加计划的新编号，不改旧编号或整体接口风格。目录同步按 dry-run、显式同步和策略刷新完成，不重复首次播种。

## 实施顺序

| 阶段 | 可运行结果 | 本阶段停止点 |
| --- | --- | --- |
| 1 固定规格接线 | 同一现有单监听器主链可以选择三型号，真实持久化并使用平台已有的对应 Class | 修复 flavor 落库、映射或观察中的软件断点；平台预置缺失则报告环境前置缺口，不扩展到 Installer |
| 2 多监听器纵向接线 | HTTP → 身份/权限 → Resource → PG → 真实 worker/适配器，两个监听器分别关联后端；增删改、旧重放和清理可运行 | 不停留在仅 Proto/数组改动；Gateway 更新、逐端口观察和回收未接通时不算完成 |
| 3 VPC 预设接线 | 读取预设 → 选择 → 后端匹配与冲突检查 → 持久创建 → 正常划分 Subnet；自定义及冲突请求拒绝 | 平台网段未知时保留明确断点，不放宽校验或任意选值 |
| 4 真实环境验收 | 10～12 上真实监听器转发、三规格运行事实及 VPC 预设创建/拒绝；只清理本任务资源 | 软件/外部替身闭环与集群业务结果分别记录 |

文档随对应实现更新：Resource 原规格的单监听器/small 限制、固定映射及 VPC 预设配置说明、Governance 接口登记。复用这份计划与既有结果入口，不复制多份状态台账。

## 执行环境

- 本地只做源码编辑、Git、文档和轻量差异检查。生成、格式化、依赖工具、编译、测试、独占 PostgreSQL、镜像和 API 驱动使用 `ssh fedora`。Fedora 不可用时保留未执行，不回退本地或用集群节点编译。
- 每仓工具按自身 go.mod、Makefile 与工具锁；不统一降级或使用 latest。远端使用任务私有目录、源码清单及哈希，先暂存和审查生成物再写回本地，保留期间工作区变更。沿现有共享重任务锁串行执行，不另建平行重流水线。
- 数据库测试只用 Fedora 上本任务独占数据库和角色，不能使用业务 DSN。集成 fixture 的迁移和清理均限本任务资源。
- 集群验证只用用户指定的 10～12。实施前只读核对 SSH 目标、context、集群 UID、节点、组件版本和现有对象身份。控制与产物驱动从 Fedora 发起；集群节点只承载部署和真实业务验证。
- 不重置集群、恢复节点快照、重装 KCN/OVN、改共享网段或扩大测试租户授权。既有 Class/EnvoyProxy 只读核对，缺失或不匹配按环境前置缺口报告；通过产品链清理本 run 的 LB、Attachment、Subnet/VPC 与后端，保留公共预置 Class 和共享池。

## 必要验证与扩大条件

默认仅运行本轮变更对应的具名函数或子场景；环境未准备不写成 pass。以下范围为后续实施的测试计划，不是已经执行的结果。

| 验证 | 最小必要行为与复用位置 |
| --- | --- |
| 生成与编译 | Resource 的 `make config`、`make sql` 按改动执行；Governance 使用正式 `make api`，不手改生成物。编译受影响模块和实际入口，检查必要生成一致性；不因每次小改反复全仓生成/格式化 |
| LB 参数与映射 | 扩展既有 normalization/fingerprint/health-port/wire 用例：两个监听器、独立健康检查、重复名称/端口、非法集合、旧请求兼容；三值接受、未知型号拒绝和正确映射；三档实际 Deployment/Pod 资源、副本和 EndpointSlice Pod 数观察 |
| PG 迁移与隔离 | 在独占库验证当前旧 schema 升级、旧配置/ID/回执重放；三 flavor 实际落库；listener/config/member/组件复合关系跨租户拒绝；重试不重复占用 EIP/VIP |
| worker 与多端口 | 扩展既有 LB 生命周期和删除关系用例：增加、改端口、移除，另一监听器配置保留；缺任一 Service/EndpointSlice 端口不能算全部 configured；部分失败保留 applied_version |
| 必要恢复场景 | 复用 `TestLBServiceProcessesRecoverUnknownMutationsWithTwoWorkers`，只运行相关 route UPDATE 响应丢失和新增 Gateway UPDATE 响应丢失子场景；不重跑完整创建/删除故障矩阵 |
| VPC 预设 | YAML 数组加载及两个运行入口注入同一策略，非法配置拒绝，空配置不开放新建；查询过滤平台冲突项，创建匹配同一白名单、非预设拒绝、平台重叠拒绝、未知/过期事实拒绝；策略变化不破坏旧幂等重放；Subnet 正常划分、不同 VPC CIDR 可复用；平台池互斥的相关并发场景 |
| Governance 定向联调 | 复用 `TestNetworkJointHTTP` 和真实 Resource peer/PG/mTLS/worker；选择新增 LB listeners 与 VPC presets 的 JWT/AK 子场景，核对权限/套餐拒绝和跨租户后端拒绝。外部 KC/Envoy/实例 owner 替身只在真实外部协议处使用 |
| 平台预置只读核对 | 在 10～12 核对所需 Class/EnvoyProxy 的实际身份、参数和接受状态，确认三规格映射所依赖的前置条件；不运行 Installer 渲染、编译或安装 |
| 10～12 真实业务 | 用已有真实纳管后端，经产品接口验证两个端口分别到独立后端，并在同一个 LB 上增删改和清理。三规格串行创建、核对实际副本/资源/Class、访问并释放，不同时压入三档负载；预设读取/创建/拒绝放在同一 run |

Gov 当前 joint 用例的 JWT/AK 块串联全部 Network 主链，没有独立 LB 子场景。必要修改只在原 fixture 中拆出具名 LB/VPC 子场景，共享已有 helper，之后通过 `scripts/network-joint-integration` 的 `-run` 参数选择；不能声称该选择器现在已经存在，也不复制第二套模拟资源系统。Resource `scripts/lb-live-runtime` 当前限定旧 Ubuntu 路径，若需复用只参数化任务根和明确 Fedora 主机约束，保留身份与清理守卫。

真实后端必须有现有实例 owner、Attachment 和 Provider 归属事实；普通 Pod 存在不能替代纳管。若缺这一前提或 large 资源不足，记录实际主链断点，不扩展实例 owner 功能、降低预置规格或通过重复尝试制造通过。

不默认执行 Network 全量集成/race、Image、SNAT 全生命周期、Kubeflow、性能/HA 或全环境矩阵。只有新失败指向其他模块、修改了共享并发/身份路径、或仓库明确发布门禁要求时扩大，并写明原因。已通过项目仅在相关代码再次变化或有未解决风险时重跑。

Resource AGENTS 要求提交前 `make verify`：若后续任务进入提交阶段，在稳定候选上于 Fedora 完整运行一次该必要门禁；它不触发全量 integration/race/audit。Governance 同样保留实际适用的生成/CI 门禁，不把日常定向回归自动扩成全部业务验收。本次已进入后端实施；未进入提交阶段，不触发提交前全量门禁。

## 完成条件与结果记录

以真实可运行主链判断完成：多监听器及每监听器转发、三型号持久化和运行映射、仅预设且不冲突的 VPC 创建全部成立；必要身份、隔离、幂等、恢复和产品清理同时成立。用实际来源和具名结果区分 `pass`、`fail`、`not_verified`，模块编译、配置 Ready 或 HTTP 200 不替代数据面验收。

执行时只保留恢复/复现必需的源码哈希、工具版本、具名命令退出码、目标身份和清理结果，原始日志放任务私有工件。提交、推送、PR、合并和部署按后续明确授权处理，本次不执行发布动作。

## 实施结果与剩余断点

本次只改后端，代码在以下任务工作树；未提交、推送、创建 PR 或部署共享服务。生成物由 Fedora 正式入口生成，写回前按本地快照校验，未覆盖其他任务修改。

| 仓库 | 实施起点 origin/main | 任务工作树 |
| --- | --- | --- |
| Governance | `dd1e772ac5d0718c00a3cc197c9e750c0b9fe896` | `/home/chabking/workspace/ani-governance-network-presets-20261010` |
| Resource | `6690df41110af98b91909c9f5bbb78b00805b249` | `/home/chabking/workspace/ani-resource-network-presets-20261010` |

两仓分支均为 `codex/network-lb-vpc-presets-20261010`。原工作区无关修改保留；未修改前端、Installer、KCN 或 Envoy Gateway。

### 实际主链

Fedora 隔离闭环已验证三个正向子场景：`TestNetworkJointHTTP/fixed_flavors`（small JWT、medium AK、large JWT，串行持久化并核对映射/资源后释放）、`multiple_listeners`（两个独立纳管后端，创建、增加、改端口/健康检查、移除、同请求跨凭证重放和删除）、`vpc_presets`（JWT/AK 读取同一策略，创建 VPC、划分 Subnet，拒绝自定义和平台冲突 CIDR）。身份、Casbin 权限/NETWORK 套餐、mTLS、Resource 用例、PostgreSQL、worker 和 KC 适配器均为真实业务代码；仅 KC/Envoy 控制器及实例 owner 外部协议使用替身，不能据此宣称真实流量通过。

监听器追加版本关系和 tenant/placement 复合外键，Gateway 使用 UID/resourceVersion CAS。完整 Service/EndpointSlice 端口与实际 targetPort、单后端身份失效的选择性撤转发、集合遗漏保持、全部目标确认后推进 applied_version、旧 schema/ID/回执重放和占用释放均有定向行为证据。VPC 策略只在首次受理执行；空配置/策略变化不破坏历史重放，Subnet 不受 VPC 白名单限制，隔离 VPC 可复用 CIDR。平台池和 VPC 共用 cluster 锁，Provider 首次创建前再次检查。

### 具名验证与复现

执行主机为 `ssh fedora`，任务根 `/home/chabking/workspace-runs/network-presets-20261010-01a12425`；使用既有重任务锁与 CPU/内存上限。Resource Go 1.26.7，Governance Go 1.26.9；API/配置/sqlc 生成分别使用锁定工具的 `make api`、`make config sql`。

| 具名范围 | 实际结果及日志 |
| --- | --- |
| Resource `TestLBActualAdapterThreeExposuresUpdateAndDelete` 的 private small/medium/large 与 public_private small | pass；`evidence/resource-main-01.log`，退出 0 |
| 同一多监听器增删改流程使用独立后端/健康端口 8080 与 9090 | pass；`evidence/resource-listeners-independent-01.log`，退出 0；配置、身份、版本、选择性失效和删除占用释放均通过 |
| Resource `TestLBMultipleListenersMainFlow`、`TestLBListenersUpgradeFrom0008AndReplay` | pass；`evidence/resource-targeted-02.log`，退出 0 |
| Resource `TestVPCCIDRPresetsMainFlow`、`TestVPCCIDRPresetUnknownAndExpiredFacts`、`TestVPCCIDRPresetContendsWithPlatformPool`、`TestLBNewRelationsRejectCrossTenantSQLAndAPI` | 均 pass；`evidence/resource-targeted-01.log` 中这些具名用例通过；该批因旧升级 fixture 失败退出 1，已仅修复并重跑升级用例 |
| Governance 三个正向主链及 `authentication_and_plan` | 均 pass；`evidence/governance-targeted-02.log`；整批因 preset_access 的 fixture 误授权限退出 1，未改写成整批 pass |
| Governance `preset_access`：无身份/无权限、新接口未知 query、JWT/AK 跨租户 VPC/多监听器受理 | pass；纠正 fixture 授权后，仅重跑该子场景，`evidence/governance-preset-access-01.log`，退出 0 |
| YAML 加载/非法与空配置、LB normalization/fingerprint/独立 health-port/wire、25 个 Network AK operation | pass；`evidence/resource-targeted-01.log`、`backend-remaining-01.log`、`backend-remaining-02.log` 对应具名检查 |
| `TestLBServiceProcessesRecoverUnknownMutationsWithTwoWorkers/route_UPDATE_response_lost` | pass；`evidence/resource-recovery-01.log` 中子场景通过；同批 Gateway fixture 请求未携原 Backend ID，计数断言失败，整批退出 1 |
| 同恢复用例 `/gateway_UPDATE_response_lost`，真实 listener port 变更并保留旧 Backend ID | pass；`evidence/resource-recovery-gateway-02.log`，退出 0；双进程恢复、无重复 Provider 创建和产品清理通过 |
| 读取期间 NodeFacts 续报正常接受，未知/过期仍拒绝，VPC 主流程再次运行；事实失败保留 Cause | pass；`evidence/resource-vpc-final-02.log`，退出 0；Resource 实际入口同步编译通过 |
| 两服务实际入口及 lb-api 编译、最终格式化与生成物写回 | pass；`evidence/backend-format-build-01.log`，退出 0；写回前逐文件校验本地快照 |
| 旧 VPC 受理/租户/幂等、事务失败不留半条记录，及未知/过期事实再次拒绝 | pass；`evidence/resource-vpc-legacy-01.log`，退出 0；只补既有夹具的明确策略与外部事实，`testenv.NewDatabase` 的默认策略仍 fail-closed |
| 10～12 真实 VPC 预设 → VPC → Subnet → 拒绝 → 删除，JWT/AK 两种身份 | pass；修正 HTTP 夹具默认 1s 期限后，`evidence/governance-vpc-live-full-04.log` 退出 0，同一轮 lifecycle/rejections 均通过；两 VPC、两 Subnet 的八个创建/删除操作均 succeeded，四个实际 Provider UID 持久化，最终无活动资源/占用。首轮 `governance-vpc-live-01.log` 退出 1 保留 |
| 仅复现真实 VPC 候选/拒绝，无重复生命周期 | 夹具默认 1s 时 `governance-vpc-live-rejections-02.log` 退出 1（AK 候选读取 504，后端耗时 0.995s）；对齐正式 `app/admin/service/configs/server.yaml` 的 10s 后，`governance-vpc-live-rejections-03.log` 退出 0（JWT/AK 候选 200、自定义 400、平台冲突 412、无新增操作） |

正向联调选择器为 `scripts/network-joint-integration -run '^TestNetworkJointHTTP/(fixed_flavors|multiple_listeners|vpc_presets|authentication_and_plan)$'`；恢复选择器为 `scripts/integration -tags=networkintegration ./internal/data/network -run '^TestLBServiceProcessesRecoverUnknownMutationsWithTwoWorkers/(route_UPDATE_response_lost|gateway_UPDATE_response_lost)$' -v`。不默认展开 Network 全量 integration/race 或其他业务矩阵。原始失败日志保留；补验由明确 fixture 修正或相关源变化触发，没有删除断言或扩大豁免。首次 Governance 实际入口编译触发任务 cgroup OOM，后续改用串行包编译和较低 Go 内存目标，保留原失败。

发布前，Governance 新接口通过任务私有 `governance-validation.mod` 指向 Resource 候选源码完成联合验证，仓库 go.mod 未写入临时路径或虚构版本。用户随后授权提交并推送两仓 main；Resource 先发布 `9191cebb408ba8defde75c3653704406f017401c`，Governance go.mod 锁定正式版本 `v0.0.0-20261010084537-9191cebb408b`，模块校验保持开启，正式消费不使用 replace 或私有 modfile。

软件联合闭环通过产品链清理 LB、Attachment、Subnet/VPC、后端及供给资源，最终活动资源/占用为 0；软件阶段日志中的 26 个独占 PostgreSQL/Redis 容器均已核对不存在。真实 VPC 阶段同样经产品接口删除资源；隔离 Resource 数据库在退出时的活动 VPC、Subnet、Attachment、pending binding 和未成功操作均为 0，实际 Resource 进程已停止，独占数据库已移除。

### 真实 10～12 验收：VPC 同轮主流程通过，LB 未完成

异常关机后的复核已恢复访问：`kubernetes-admin@ani-lab`，kube-system UID `5277649d-e28d-4a0a-ae76-8354365c63bc`，ani-01/02/03 均 Ready。实际仅安装 `lb-small-noeip` / `envoy-proxy-small-noeip`（2 副本及 small 资源符合）；`lb-small`、`lb-medium`、`lb-medium-noeip`、`lb-large`、`lb-large-noeip` 和对应 EnvoyProxy 缺失，三档完整安装能力与真实 LB 流量验收未成立。

共享 Resource 启动参数为 `-conf /run/configs`，配置来自 ani-system Secret `resource-configs` 的 `config.yaml`，不是仓库 configs 目录；它未包含 vpc_cidr_presets，本批没有修改该共享配置。实际平台网段为默认 `10.16.0.0/16`、System `100.64.0.0/16` / `100.65.0.0/16`、Service `10.96.0.0/16`。原有 NodeFacts 采集代码在任务临时部署中从三节点读取实际网卡、OVS 和地址并续报；已分配前缀包括 `172.16.101.0/24` 与 `100.64.0.0/17`、`100.64.128.0/17`，没有用静态伪造记录代替。

真实 VPC 使用 Fedora 上独占 PostgreSQL、实际 Resource `-conf` 入口、任务私有 mTLS，以及上述真实集群/KCN。隔离配置的有效预设为 `10.73.0.0/16`，Subnet 使用 `10.73.1.0/24`；同时配置 Service/管理冲突项以验证候选过滤。JWT/AK 均读到同一候选，两种身份各自完成创建、幂等重放、Subnet 划分和产品删除，数据库保存了实际 Provider UID 与成功操作。首轮平台冲突返回 503 的失败保留；进一步拆出同一夹具的 `rejections`，复现出默认 HTTP 1s 期限取消实时 KC 查询（AK 候选 504，后端耗时 0.995s）。生产 REST 装配消费配置期限，仓库正式配置为 10s；仅将联调夹具期限对齐该值，未改变业务规则或错误断言。随后 `rejections` 和完整 `vpc_presets` 各运行一次，均退出 0：同一完整轮包括候选过滤、JWT/AK 生命周期、自定义 CIDR 400、平台冲突 412 及产品清理。

真实依赖仍存在独立的查询抖动：`vpc-live-rejection-diagnostic-02.log` 的 16 次真实策略读取中，一次节点列表查询超过 5 秒，错误链保留到 Kubernetes URL/context deadline；其余读取成功。该证据没有被改写为全通过，也没有通过延长 Resource RPC 期限或修改集群制造成功。临时诊断程序已移除，原始失败、诊断和临时数据库最终数据保留在任务私有目录。

真实资源清理后，两轮三节点采集器各自的 21 个任务对象和任务租户 Namespace 均按各轮实际 UID 删除并核实不存在，所属 Pod/ReplicaSet 均为 0；截至最终收尾，日志中的 37 个已知任务容器均已核对不存在。临时 Resource 单元均已 inactive、MainPID=0；独占 PostgreSQL 和各轮 Governance PostgreSQL/Redis 已移除，未使用的任务 NodeFacts 镜像及归档已删除，任务私有 SSH agent 已停止。节点工具只读采集，没有变更共享 KCN 部署、网段、Class/EnvoyProxy 或公共池。首轮清理证据在任务私有 `vpc-live-runtime/`，修复后完整轮的最终业务数据和带 UID 的删除回执在 `vpc-rejections-02/`。

按目标限制保留共享 Class/EnvoyProxy、KCN、节点及公共池。2026-10-10 用户要求立即收尾，不再验收缺失的五套平台预置配置；本批交付后端实现与已有验证，不补装共享组件，不继续扩大测试。三规格及多监听器真实流量仍为 not_verified，不能将软件替身闭环写成实机通过。验收快照时尚未提交或推送；用户随后授权 main 源码发布，正式 SDK 消费见上述版本及下方发布记录。共享服务未部署，原目标中的实机 LB 完成条件尚未成立。

### 2026-10-10：授权交付远端 main

用户明确授权提交并推送两仓相关改动。Resource 先以 `9191cebb408ba8defde75c3653704406f017401c` 交付 main；Governance 同批提交锁定 `v0.0.0-20261010084537-9191cebb408b`，不使用私有 replace/modfile。最终 Governance 提交号以 Git 和交付回执为准，不为回填自身 SHA 追加提交。

发布前的 Resource `make verify` 已执行；生成、边界、故障构建与 tidy 阶段通过。首次测试因任务临时 socket 路径 157 字节而中断，缩短任务私有 TMPDIR 后原用例和全部单元、vet/build、模块校验通过；远端快照无 `.git`，末项差异检查在本地真实工作树通过。未修改 socket 实现或断言；原失败和续跑日志保留在 `resource-publish-verify-01.log` / `resource-publish-verify-continuation-02.log`。

Governance 的正式 SDK 拉取首次因国内校验镜像暂不可用退出 1（`governance-published-sdk-pin-01.log`），切换官方代理后保持 checksum 校验并退出 0（`governance-published-sdk-pin-02.log`）；正式下载的三份 Network API 源码 hash 与已验证候选一致。Fedora `go test -count=1` 的 data/service/auth 包 `^TestNetwork` 子集、实际 server/admin 两入口编译和 `go mod verify` 在正式依赖下均退出 0（`governance-published-sdk-checks-01.log`）。只更新 SDK 版本及校验和，未扩大依赖升级、集群验收或五套配置测试。

main 源码交付不等于部署或实机 LB 验收；CI 结果以对应提交的实际运行状态为准。前端、共享配置及集群组件保持原状，工作树中的历史失败证据不随业务提交。

## 依据

- Governance：[开发指南](../development.md)、[仓库维护规范](../contributing/repository-hygiene.md)、[现有 Network 接口登记](../interface-integration-register.md#2026-10-09租户-network-补齐与响应合同)。
- Resource 固定源码：[LB 合同](https://github.com/zhangzhe-ctrl/ani-resource-service/blob/6690df41110af98b91909c9f5bbb78b00805b249/docs/specs/vpc-connectivity-lb.md)、[单监听器关系](https://github.com/zhangzhe-ctrl/ani-resource-service/blob/6690df41110af98b91909c9f5bbb78b00805b249/migrations/0007_load_balancers.sql)、[Gateway 生成与观察](https://github.com/zhangzhe-ctrl/ani-resource-service/blob/6690df41110af98b91909c9f5bbb78b00805b249/internal/data/network/kc_load_balancer.go)、[平台 CIDR 检查](https://github.com/zhangzhe-ctrl/ani-resource-service/blob/6690df41110af98b91909c9f5bbb78b00805b249/internal/data/network/kc_node_facts.go)。
- 部署材料来源参考，本批不修改 Installer：[Envoy 安装任务](https://github.com/zhangzhe-ctrl/ani-installer/blob/78d8418307b6156954feb10aee8d6972d2ff1bbd/kubekey/builtin/core/roles/ani/envoy/tasks/main.yaml)、[当前小规格模板](https://github.com/zhangzhe-ctrl/ani-installer/blob/78d8418307b6156954feb10aee8d6972d2ff1bbd/kubekey/builtin/core/roles/ani/envoy/templates/envoy-proxy.yaml)。
- Gateway API：[v1.5.1 Listener 合同](https://github.com/kubernetes-sigs/gateway-api/blob/v1.5.1/apis/v1/gateway_types.go)、[Route 对监听器的关联](https://github.com/kubernetes-sigs/gateway-api/blob/v1.5.1/apis/v1/shared_types.go)。

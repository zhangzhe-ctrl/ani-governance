# Gov 前端 HTTP 接口规范

Sandbox 实验性端口预览：`POST /api/v1/sandbox/{sandbox_id}/ports` 的 `data` 包含 `port`、`protocol`（http 或 tcp）、`idempotency_key`、`expected_version` 和可选 `ttl_seconds`（60–3600，默认 3600）；`POST /api/v1/sandbox/{sandbox_id}/ports/{preview_id}:close` 以 `data` 传幂等键和版本；响应包含异步 `operation_id` 和端口状态。`GET /api/v1/sandbox/{sandbox_id}/ports?page=1&pageSize=20` 返回 `items`/`total`；`GET /api/v1/sandbox/{sandbox_id}/port-operations/{operation_id}` 查询操作。四个接口分别要求 `sandbox:preview:open`、`sandbox:preview:close`、`sandbox:preview:list`、`sandbox:preview:operation:read` 和 SANDBOX 套餐。功能默认禁用；启用后用户主动开放才创建 NodePort Service。**NodePort 没有额外入口认证，所有能到达集群任一节点该端口的来源都可能访问，返回的节点地址不是唯一入口**；地址只证明已观察到节点及 Pod 关联，不保证客户端网络可达或应用健康。关闭、暂停、删除和到期触发 Service 回收，操作完成前保持待确认；未知创建不能因暂时查不到而当作已关闭。 Kubernetes 对象回收不保证已建立连接立即终止，NodePort 之后可能被重新分配。Pod selector 按标签转发，前提是普通租户无法在该 Namespace 创建 Pod 或修改 Pod 标签；UID 用于观察校验，不能充当 selector 的隔离条件。当前为候选源码合同，尚未集群部署验收。

本规范定义经 Gov 向前端提供的 HTTP JSON 接口的请求、响应和分页规则。新增和修改接口遵循本规范；存量接口按明确范围迁移，状态在[接口集成登记](../interface-integration-register.md)维护。VPC 已实现下述页码协议，可作为其他服务的参考，不代表所有存量接口已经统一。

Governance BFF 当前提供 `/api/v1/inference/...` 推理服务管理接口，包括普通及 GPU 推理的创建、列表/详情、操作查询、更新、启停/重启和删除。业务管理请求直接访问 Governance；Higress 仅承载 OpenAI 兼容推理数据面（例如 `/v1/completions`），不得把这些 `/api/v1/...` 路由加入 Higress。Inference BFF 的列表请求使用 `page`/`pageSize`，响应使用 `items`/`total`；`total` 在 JSON 中是十进制字符串。

Sandbox 管理从 Governance BFF 进入，Resource 不暴露公网 HTTP。接口为 `POST /api/v1/sandbox`、`GET /api/v1/sandbox`、`GET /api/v1/sandbox/{sandbox_id}`、`GET /api/v1/sandbox/operations/{operation_id}`，以及带 `:pause`、`:resume` 的 POST 和 DELETE。创建和生命周期请求使用 `data` 对象，返回异步受理的 `operation_id`/`resource_id`；列表返回 `items`/`total`。Sandbox 是独立资源，不要求其他实例。Gov 完成认证、权限、SANDBOX 套餐检查和可信租户映射；Resource 按租户和 Sandbox ID 检查归属。列表使用 `page/pageSize` 和默认值 `1/20`。

Sandbox 终端：`POST /api/v1/sandbox/{sandbox_id}/exec` 使用独立 Sandbox ID，`data` 包含 UUID 格式 `idempotency_key`、正整数 `expected_version`、`rows` 和 `cols`（1–4096）；路径和请求体的 `sandbox_id` 不一致时拒绝。需要 `sandbox:terminal:connect` 权限及 SANDBOX 套餐。同步返回短期 `session_id`、`ws_url`、`expires_at`，`session_id` 是会话身份，不是 Sandbox 身份，连接 URL 含单次票据，必须按凭据处理。相同租户、用户、Sandbox 和幂等键重放同一会话，改变请求参数产生冲突；已使用或过期的票据不能作为新会话复用。Resource 验证租户、版本及 Namespace/Pod UID，Session Gateway 在签发和连接时复核身份并在容器内检查 Pod UID。只有已运行的 Sandbox 可申请；镜像须提供 `/bin/sh`。最后一次校验到建连之间仍存在与暂停/删除并发的窗口，已建立连接会随 Pod 退出结束，不保证受理瞬间踢出。2026-11-04 已在 66 使用隔离兼容镜像完成 WebSocket 交互、跨租户拒绝、版本错误和票据重放验收；当前正式跨仓 API 依赖尚未发布。

Sandbox 环境变量元数据：`GET /api/v1/sandbox/{sandbox_id}/environment?page=1&pageSize=20`，按名称排序，返回 `items`/`total`，默认 1/20，最大 100。仅查询当前通过身份核验且处于运行就绪状态的 Pod 的显式 `env` 配置，包含 `name`、`source`、可选 `secret_ref`/`field_ref`，不返回值；不包含镜像 ENV 或 Kubernetes Service 自动注入项。当前创建不支持自定义变量或密钥引用；新 Pod 当前仅有 `ANI_SANDBOX_POD_UID`（`source=downward_api`，`field_ref=metadata.uid`），旧 Pod 可能为空。暂停或没有可核验 Pod 时返回错误，不能把它解释为没有变量。接口需 `sandbox:environment:list` 和 SANDBOX 套餐，实际部署和验收以 Resource 执行状态为准。

Sandbox 出口声明：`GET /api/v1/sandbox/{sandbox_id}/egress` 查询当前可核验运行中 Pod 被标准 Kubernetes NetworkPolicy 选中的出口声明，需 `sandbox:egress:read` 和 SANDBOX 套餐。返回 `source=kubernetes_network_policy`、`declaration`、`isolated`、`rules`、`enforcement=unknown`；按下述规则另外返回意图与 `host_allowlist_status`。规则包含目的 Pod/Namespace 标签选择器或 IPBlock、协议与端口。多个选中策略规则取并集；没有选中的 Egress 声明时 `unrestricted` 表示标准 NetworkPolicy 未声明隔离，`deny_all` 表示已隔离且没有允许规则，`restricted` 表示存在有限允许规则。每条规则空 `peers` 表示任意目的地，空 `ports` 表示任意协议和端口。该结果只描述标准 NetworkPolicy，不能证明 CNI 执行状态、覆盖其他集群级策略或推导 Host 白名单；未知 Host 不是空白名单，也不代表全部允许或拒绝。运行中 Pod、Namespace 或规则快照无法核验时返回错误；非运行状态的意图查询见下文。须部署带租户 NetworkPolicy list 权限的 Resource 并登记授权，尚未集群验收。


Sandbox 创建和检查点克隆目前不支持实际执行域名白名单或全阻断。新的 `data.egress_intent.network_egress_policy` 若为 `allowlist`（包括空列表）或 `deny_all`，返回 HTTP 501、reason `SANDBOX_EGRESS_ENFORCEMENT_UNSUPPORTED`；不创建新操作。未声明意图和 `internet` 沿用原路径，但 `internet` 不保证实际连通。已受理历史命令先作精确幂等回放；历史资源的出口意图仍可查询，`intent_enforcement=not_applied_by_sandbox` 表示并未由 Sandbox 实施网络控制。历史已登记但结果不明的受限命令返回 HTTP 503、reason `SANDBOX_EGRESS_OUTCOME_UNCONFIRMED`；不继续向旧 Resource 投递，保持 pending 和原有重放屏障，不视为失败，也不能清除。历史普通创建可在只读核验 Resource 已受理后回放；历史克隆在缺少可信只读证明时继续等待。相同请求重放返回上述 503；支持 Gov 意图回退的新版操作查询可显示 `unknown`，其含义见操作详情说明。旧版 Gov 可能返回 404，不能据此认定操作不存在或失败。升级前须逐项核查。GET 的 `host_allowlist_status=known` 仅表示已知意图；`not_applicable` 表示其他策略，`unknown` 表示未提交或未知。NetworkPolicy 查询与意图相互独立，不证明实际数据面拦截。

Sandbox 普通事件：`GET /api/v1/sandbox/{sandbox_id}/events?page=1&pageSize=20`，返回 `items`/`total`，默认 1/20、最大 100，按 `occurred_at` 降序、事件 ID 降序；事件字段为 `id`、UTC RFC3339 时间 `occurred_at`、`type`、`reason` 和 `message`。读取该租户 Sandbox 已记录 Pod UID 对应、Kubernetes 当前保留的 Pod 事件，包括暂停、恢复前旧 Pod 和销毁后的可见事件；不包含安全事件。响应 `scope` 为 `retained_events_for_recorded_pods`，`history_complete_before_migration` 为 false，表示迁移前覆盖不能保证。`type`、`reason` 限于安全值，未知或缺省为 `Unknown`；`message` 是按事件原因映射的安全说明，不透传可能含敏感信息的 Kubernetes 原始消息。尚未记录任何 Pod UID、Namespace 身份无法核验或事件源不可用时返回错误。事件没有独立持久归档，保留受集群配置和清理影响，迁移前已经丢失的 Pod UID 无法补回；空列表不证明从未发生事件。接口需要 `sandbox:event:list` 和 SANDBOX 套餐；需部署带有事件读取权限的 Resource，实际部署和验收以 Resource 执行状态为准。Gov 与缺少 scope 的旧 Resource 混用时返回不可用。

Sandbox 安全事件查询：`GET /api/v1/sandbox/{sandbox_id}/security-events?page=1&pageSize=20&severity=warning` 要求 `sandbox:security-event:list` 和 `SANDBOX` 套餐。当前没有可信的独立 Sandbox 安全事件来源，经权限、租户和 Sandbox 归属检查后返回 HTTP 503 `SANDBOX_SECURITY_EVENT_SOURCE_UNAVAILABLE`，不能将其展示成空列表或无安全事件。Kubernetes Pod Warning 通过普通事件接口查询，不作为安全事件。接口支持的筛选参数保留，以便今后接入可信来源；本合同的代码变更尚未部署，当前集群实际行为以执行记录为准。

Sandbox 日志：`GET /api/v1/sandbox/{sandbox_id}/logs?tailLines=100&follow=true` 使用 `text/event-stream`，要求用户身份、`sandbox:logs:read` 和 SANDBOX 套餐。`tailLines` 默认 100、范围 1–1000；`follow` 默认 true，false 表示读取后结束。数据源是已核验当前 Pod 容器由 Kubernetes 提供的带时间戳 stdout/stderr，仅覆盖 Kubernetes 尚保留的日志，不是独立持久归档；暂停、销毁及 Pod 不可核验时不能保证读取历史。成功时先发送 `ready`（JSON `pod_uid`），随后 `log`（JSON `text`，片段可拆行，需依次拼接）和 `end`（JSON `reason`：`EOF`、`TIME_LIMIT`、`BYTE_LIMIT` 或 `TARGET_UNAVAILABLE`）；开始后的故障发送 `error`，心跳为 SSE 注释。上游 READY 之前失败返回 HTTP 错误，之后不能再改变 HTTP 状态。

服务在完整日志行成功发送后可发送 `cursor` 事件，包含 `since_time` 和 `pod_uid`；发现已接收日志的时间倒序时可能立即发送指向更早时间的回退游标，客户端须以最新事件为准；若已保存游标后收到 `cursor_invalidated`（含 `pod_uid`、`reason=TIMESTAMP_UNAVAILABLE`），须丢弃该 Pod 游标并从原始起点重新拉取；续传使用 `sinceTime=<since_time>&podUid=<pod_uid>`，两者必须同时提供，且不能同时指定 `tailLines`；Pod 变化返回错误。Kubernetes 按秒读取，可能重放同一秒的行；相同内容的重复行无法可靠区分。日志乱序、日志轮转、Pod 消失或同秒日志超过每次 8 MiB 上限时无法保证完整续传。单连接最多 10 分钟，并有连接数、写入超时和缓冲上限；不支持 SSE `Last-Event-ID` 自动续传。当前仅为源码合同，部署需要 Resource Pod 日志读取权限、接口权限登记及单独授权，尚无真实集群验收。

Sandbox 监控：`GET /api/v1/sandbox/{sandbox_id}/metrics?start=2026-11-03T00:00:00Z&end=2026-11-03T01:00:00Z&step=60` 返回按时间网格对齐的历史指标；时间为整秒 RFC3339，缺省 end 为当前时间、start 为 end 前 1 小时，步长以秒计默认 60、范围 15–3600；范围最多 24 小时、1440 个间隔，不能超过当前时间 1 分钟。`GET /api/v1/sandbox/{sandbox_id}/metrics/latest` 返回当前 Pod 最近指标，不接受查询参数。历史响应 `scope=known_pods_only`，最新响应 `scope=current_pod_only` 且包含 `pod_uid`；每个 `series` 包含 `name`、`unit`、`status`、可选 `reason` 和 `points`，每点有 UTC RFC3339 `timestamp`、`status`，只有 `status=ok` 才有 `value`。指标包括 `cpu`（核）、`memory`（字节）、`network_receive` 和 `network_transmit`（字节/秒）。CPU 使用 Pod 父 cgroup，网络仅统计 CNI 的 eth0 接口，不包含其他接口；CPU 与网络是 2 分钟速率。最新指标的单项来源重复时该项为 unavailable、原因 AMBIGUOUS_METRIC_SOURCE，其余指标继续返回；内存是 Kata VM guest 的 MemTotal − MemAvailable，含 guest 系统开销，并非容器 RSS 或工作集，按 procfs 的字节输出；历史速率窗口可能跨相邻状态边界。空、未采到、陈旧、来源不可验证的点为 `unavailable` 而不是 0；在已记录 UID 和当时可用的 KSM 观测范围内，同一时间多个 Pod 或多条 Kata runtime 来源重叠为 `ambiguous`，不提供数值；KSM 可用也不能保证 informer 缓存无延迟或全部 UID 完整。历史仅覆盖 Resource 持久记录过的 UID，旧版混跑、回滚或 Pod 在 UID 首次写库前消失可能造成遗漏，整个时间段覆盖始终是 best effort，不能从某个迁移时间推断完整；暂停或删除后仅已知 UID 在监控系统保留期内可查询，最新指标需要当前运行 Pod。历史身份最多读取 Resource 保留的 1000 个已知 Pod UID，分批核对全量身份后查询区间内有观测的 Pod；超限、上游截断或查询超时返回错误，不静默截断。单次查询仍受总时间预算限制，不能保证大历史在每个环境都能在预算内完成。Gov 完成用户和 Sandbox 权限检查，并从 Resource 核验 Sandbox 的集群和当前或历史 Pod UID 后，委托 Observability 模块查询指标；Gov 不再直接访问 Prometheus。Observability 使用受管集群数据源，要求 kube-state-metrics 和 cAdvisor 样本具有与集群匹配的 `ani_cluster_id` UUID、Pod UID 映射和节点身份；由于平台的 `up` 不含该标签，分别以 `instance`、`job`、`endpoint`、`service` 关联原始目标，并要求上游健康和采样新鲜；cAdvisor 还要求 `metrics_path`。Kata 内存依赖 kata-monitor 的 `ani_cluster_id`、节点、`cri_uid`、`cri_namespace`、`cri_name` 和运行时 `sandbox_id`，并按采样时间和同一 target 关联其原始 `up`；运行时 ID 不是业务 `sandbox_id`。缺少数据时返回 unavailable。仅对冻结的 `kata` 或 `sandbox-kata` RuntimeClass 使用上述解释；运行时不匹配或缺少集群、节点可信绑定时返回 503，单项采集缺失时为 unavailable。权限分别为 `sandbox:metrics:read`、`sandbox:metrics:latest` 并需 SANDBOX 套餐。2026-11-04 曾在 66 对临时 Kata Monitor 采样和旧 Gov 直接读取监控数据的 HTTP 链路进行实测：同一 Pod 的最新及历史内存采样在一个时刻均为 69275648 bytes，与该时刻 guest 样本的总量减可用量一致；这只证明该次测试点。新 Observability Sandbox RPC 和 Gov 新接线目前仅完成本地联合测试，API 依赖尚未发布，尚未部署或进行该新链路的真实验收；本合同不能据旧链路结果推断新链路已通过。

Sandbox 文件接口以工作盘 `/workspace` 为根，路径为相对路径。 查询的目录不存在时返回 404；空目录返回 `items=[]`、`total=0`。`GET /api/v1/sandbox/{sandbox_id}/files` 使用 `path`、`page`/`pageSize`，按路径升序递归返回 `items`/`total`；传入任一新分页参数后缺省页码为 1、页大小为 20、最大 100。为兼容旧客户端，未传新分页参数时继续接受 `limit`（默认 100，最大 500）/`cursor` 并返回 `next_cursor`，两种模式不能混用。`total` 是当前单次成功遍历并通过安全过滤的文件和目录数；超过 10000 条或执行输出上限时请求失败，目录变化时不同页不保证同一快照。`GET /api/v1/sandbox/{sandbox_id}/files/content?path=...` 返回 `content_base64`。`POST /api/v1/sandbox/{sandbox_id}/files` 使用 `data` 包含 `idempotency_key`、`path`、`content_base64`、`overwrite` 和可选 `expected_version`；`DELETE` 同一路径使用 `data` 的 `idempotency_key`、`path` 和可选版本。文件内容单次上限 1 MiB。变更响应有 `operation_id`、`state` 和 `reason`，可通过 `GET /api/v1/sandbox/{sandbox_id}/file-operations/{operation_id}` 查询。相同幂等键及参数重放返回原结果，不同内容冲突；`unknown` 不证明是否执行，不能自动重复提交新键。文件列表、读取、写入、删除和操作查询分别授权。

同一 `POST /api/v1/sandbox/{sandbox_id}/files` 也接受 `multipart/form-data`：必填字段为二进制文件 `file`、工作盘内完整相对目标 `path` 和 `idempotency_key`，可选字段为十进制 `expected_version` 和 `overwrite`（`true` 或 `false`，默认 `false`）。例如：`curl -X POST -H 'Authorization: Bearer <token>' -F 'file=@./input.bin' -F 'path=uploads/input.bin' -F 'idempotency_key=<unique-key>' -F 'expected_version=1' -F 'overwrite=false' <base>/api/v1/sandbox/<sandbox_id>/files`。客户端的原始文件名不会决定写入路径，`path` 必须包含目标文件名；文件可为任意二进制。此格式复用相同写权限、租户边界、状态限制、幂等、版本与响应，原 JSON 合同继续支持；单文件仍最多 1 MiB，完整请求（含 multipart 头）最多 1 MiB + 64 KiB，超限返回 413。Gov 在内部协议中转换为 Base64，前端无需编码。禁止重复字段、额外字段和多个文件。2026-11-04 已在 66 使用隔离兼容镜像上传 65,799 字节二进制文件并通过读回哈希核验；正式跨仓 API 依赖尚未发布。

Sandbox Token：`POST /api/v1/sandbox/{sandbox_id}/tokens` 的 `data` 包含 `idempotency_key`、可选 `expires_in`（Go duration，默认 `15m`，最长 `1h`）及 `scopes`。当前 `files` 能访问文件接口，`exec` 能访问代码执行接口；需要签发者同时具备相应全部权限，且每次使用重新核对其当前权限。未指定时沿用旧默认 `connect`，但当前无对应可访问入口；`ports` 尚不可签发。凭证内容只在签发及有效重放响应返回，并提供 `token_id`、`expires_at`、`scopes`；同键同参数在有效期内重放原凭证，过期或撤销后重放冲突。`DELETE /api/v1/sandbox/{sandbox_id}/tokens/{token_id}` 撤销；Token 仅用于对应 Sandbox 的允许接口，不能管理生命周期或签发令牌。暂停、恢复或删除造成的版本变化使旧 Token 失效。

代码执行：`POST /api/v1/sandbox/{sandbox_id}/code-runs` 的 `data` 包含 `idempotency_key`、`language`、`code`、可选 `stdin`、`timeout_seconds` 和 `expected_version`；返回 `run_id`、状态和重放标记。通过 `GET /api/v1/sandbox/{sandbox_id}/code-runs/{run_id}` 查询状态、输出、退出码及截断标记。执行和查询分别授权；`unknown` 不表示远端进程已停止，也不能自动使用新键重试。返回的 `created_at` 是 Resource 持久受理时间；`completed_at` 是确认终态并写库的时间，而不是容器内程序的精确结束时间。运行中、结果未知或历史记录缺少可信完成时间时 `completed_at` 为 null。

操作历史：`GET /api/v1/sandbox/{sandbox_id}/operations?page=1&pageSize=20` 按租户与独立 Sandbox 查询，默认 1/20、最大 100，后端按创建时间、ID 和来源倒序，在同一数据库快照内返回 `items`/`total`（Proto JSON 中 `total` 为十进制字符串）。列表合并生命周期操作、代码执行、文件写删和预览端口操作，只返回操作 ID、Sandbox ID、action、state、reason、created_at 和可用时的 completed_at，不返回代码、输出、文件路径/内容或幂等键；新权限 `sandbox:operation:list` 因此授权查看这些操作的状态、类型和时间元数据。历史文件操作没有记录写/删类型时返回 `file_operation`，不猜测。`completed_at` 为 Resource 持久确认终态或将意图标为 superseded 的时间；unknown、进行中和历史未知完成时间为 null，superseded 不证明底层动作停止。超时未确认的 code-run/file dispatch 只读显示 unknown，不重新执行。

操作详情在 Resource 明确返回未找到时，按当前认证租户、映射的 Resource 租户和 operation ID 查询 Gov 未确认创建或克隆意图。若存在，返回该意图的 `id`、`sandbox_id`、`action`、真实登记时间 `created_at`、`state=unknown` 和 `reason=SANDBOX_OWNER_OUTCOME_UNKNOWN`；`completed_at` 为空。此结果只证明 Gov 已登记意图，不证明 Resource 已受理、已拒绝或已完成；无对应未确认意图时保留未找到。Resource 超时或不可用时不使用此回退。

`action=create/pause/resume/delete` 的详情通过 `GET /api/v1/sandbox/operations/{operation_id}` 查询；`code_run` 的 ID 是 run_id，使用 `GET /api/v1/sandbox/{sandbox_id}/code-runs/{run_id}`；`file_write/file_delete/file_operation` 使用 `GET /api/v1/sandbox/{sandbox_id}/file-operations/{operation_id}`；`preview_open/preview_close` 使用 `GET /api/v1/sandbox/{sandbox_id}/port-operations/{operation_id}`。不同来源的 ID 不保证跨表唯一，客户端以 action 和 ID 一起区分；各自详情仍要求各自权限。

创建时可以从 Resource Image 目录选择镜像，传入 `image_id` 和 `image_scope`（`tenant` 或 `platform`），也可以填写已登记的租户或平台镜像 `image_reference`；两种方式不能同时提供。Resource 校验登记和租户权限，解析并冻结实际镜像引用和 digest。当前不能直接使用未登记的镜像地址。

创建命令在 Gov 持久登记，投递超时保持原命令和幂等身份重试；未知结果不能仅凭超时清除。历史命令保留原摘要及旧引用，不在投递时改写。创建重放依照冻结意图匹配，冲突返回 `409 SANDBOX_IDEMPOTENCY_CONFLICT`；生命周期重放保留 Resource 返回标志。历史验证基于旧父实例模型，不代表独立创建已完成真实验收。

## 推理服务管理接口

当前注册的管理路由如下；创建、更新、删除、启动、停止和重启请求的业务字段直接位于 JSON 顶层，不使用 `data` 包装：

- `POST /api/v1/inference/services`：创建服务。
- `GET /api/v1/inference/services`：列表，可选 `status`、`capability`，分页使用 `page`/`pageSize`。
- `GET /api/v1/inference/services/{resource_id}`：详情。
- `POST /api/v1/inference/services/{resource_id}:update`：部分更新。
- `POST /api/v1/inference/services/{resource_id}:delete`：删除。
- `POST /api/v1/inference/services/{resource_id}:start`、`:stop`、`:restart`：生命周期操作。
- `GET /api/v1/inference/operations/{operation_id}`：操作详情。
- `GET /api/v1/inference/services/{resource_id}/operations`：服务操作列表，分页使用 `page`/`pageSize`。

列表筛选在分页前执行；服务状态、能力和发布状态由 Governance 根据 Inference 的受信读结果映射，不能由前端把旧的 `running` 值直接推断为可调用。详情和列表均由 Governance BFF 鉴权并按租户隔离。

## 1 请求约定

推理调用 Key 管理使用独立 `/api/v1/inference-keys`，其创建、轮换请求保持
`{data:{...}}`，不属于上述推理服务扁平请求迁移；列表各项和详情返回完整
`secret`。完整报文、分页上限及旧 Key 补存边界见[推理调用 Key 合同](inference-keys.md)。

- 查询参数放在 URL 中，创建和修改的业务数据使用 JSON 请求体。
- 业务字段及请求体结构以接口定义为准。模型创建、版本创建、导入、组合导入及推理创建、更新、删除、启动、停止、重启共 10 个接口不使用 `data` 包装；其他接口保持各自已声明结构。
- 本批次只迁移请求体，响应结构不变。推理路径中的 `resource_id` 为资源身份；请求体可省略该字段，若提供则必须与路径一致。`idempotency_key`、`expected_generation`、`update_mask` 的含义保持不变。
- 公共分页参数统一使用 `page`、`pageSize`；筛选和排序参数由各接口声明。

## 2 详情响应

查询单个资源时，直接返回资源对象，不增加资源名称、`data` 或 `code/data` 包装。

例如，查询 VPC 详情：

```http
GET /api/v1/networks/vpcs/vpc_0123456789abcdef0123456789abcdef
```

```json
{
  "id": "vpc_0123456789abcdef0123456789abcdef",
  "name": "研发网络",
  "cidr": "10.20.0.0/16"
}
```

示例仅展示部分业务字段。前端直接读取响应体中的 `id`、`name`，无需按 `vpc` 或 `data` 嵌套取出资源对象。

## 3 列表响应与分页

列表统一返回 `items` 和 `total`：

- `items`：当前页的数据数组。
- `total`：当前调用者有权查看、且符合筛选条件的总条数，不是当前页条数。JSON 中使用十进制字符串。

例如，每页两条，首次列表请求就直接查询 VPC 列表的第三页：

```http
GET /api/v1/networks/vpcs?page=3&pageSize=2
```

```json
{
  "items": [
    {
      "id": "vpc_1123456789abcdef0123456789abcdef",
      "name": "测试网络",
      "cidr": "10.30.0.0/16"
    }
  ],
  "total": "5"
}
```

| 参数 | 含义 | 默认值 | 合法范围 |
| --- | --- | --- | --- |
| `page` | 页码，从第一页开始 | `1` | VPC 为 `1` 到 `4294967295` 的整数 |
| `pageSize` | 每页条数 | `20` | `1` 到 `100` 的整数 |

没有传入某个分页参数时，独立使用该参数的默认值；例如只传 `page=3` 时，`pageSize` 仍为 `20`。参数使用单个规范十进制正整数；显式传入零、负数、空值、前导零、小数、重复参数或超出范围的值时，返回 `400`，不静默修正。未声明的查询参数同样返回 `400`。

列表支持直接跳到任意合法页，不要求先请求前面的页。分页参数只使用 `page/pageSize`，响应直接返回 `items/total`。

没有数据时，`items` 返回 `[]`，`total` 返回 `"0"`。页码超出最后一页时，`items` 仍为 `[]`，`total` 保留实际匹配总数。

列表必须先按确定的顺序排序，再执行分页；主排序值相同时使用唯一键兜底。列表查询与计数采用相同的可信租户、权限和业务筛选范围，数据库读取使用同一个只读快照。连续请求之间数据可能变化，分页不表示结果已被冻结。

VPC 的具体规则：`name` 去首尾空白后区分大小写精确匹配；`state` 按接口定义筛选，不传时排除 `deleted`。排序固定为 `created_at DESC, vpc_id DESC`，不开放任意排序字段。

## 4 创建修改和删除

写入接口需要返回单个资源时，采用与详情相同的对象结构；没有业务结果需要返回时，返回 `{}`。使用 `204` 的接口不返回响应体。

部分更新按接口声明的更新字段处理：没有指定更新的字段保持原值；清空字段必须显式表达，不能把省略字段当成清空。

异步受理必须保留任务或操作信息。请求受理成功，不代表创建、删除或其他业务操作已经完成。

## 5 错误响应

通过 HTTP 状态码区分请求成功或失败，不把失败统一包装成 `200`。

错误体包含错误码、业务原因和说明。例如：

```http
HTTP/1.1 400 Bad Request
```

```json
{
  "code": 400,
  "reason": "INVALID_NETWORK_REQUEST",
  "message": "分页参数不合法",
  "metadata": {
    "request_id": "req-example"
  }
}
```

前端根据 HTTP 状态和 `reason` 处理错误，`message` 用于展示说明，不依赖其固定文案判断业务分支。

响应提供 `X-Request-ID` 或 `metadata.request_id` 时，前端保留该值用于排查；它们不是每个错误都必定存在的业务字段。

本规范有一处显式例外：平台组件日志流
`GET /api/v1/platform/components/{component}/logs/stream` 为兼容旧版前端，错误体使用旧格式
`{"code": <字符串>, "message": <字符串>, "request_id": <字符串>}`，不使用数字 `code` 与 `reason`。
该例外仅限这一条路由，其他接口仍按本节执行。登记见
[接口集成登记](../interface-integration-register.md) 的 `PLAT-COMP-02`。

## 6 公共字段与特殊结果

- 64 位整数在 JSON 中使用十进制字符串，例如 `"total": "3"`、`"version": "1"`；其他字段类型以接口定义为准。
- 时间使用 RFC 3339 字符串，例如 `"2026-10-15T08:30:00Z"`。
- 字段名和枚举值按接口定义使用，不自行转换大小写或把所有 ID 强制转换成数字。
- 普通详情不增加包装，普通列表不增加外层 `data`。成功响应不统一添加 `code/message/data` 外壳。
- 有业务含义的组合结果保留完整结构，例如资源与操作、凭证与密钥、制品与下载地址。列表需要的 `observation` 等元数据也应保留。
- 导航树、权限快照、容量快照、统计和日志按各自业务结构返回，不因为内部有数组就强行改成普通分页列表。

## 7 VPC 参考实现与其他服务接入

现有 VPC 已实现 Gov HTTP → Resource → PostgreSQL 的页码查询链路，可作为其他服务的参考。各 owner 按相同规则接入，不要求使用相同的 Go 接口，也不以建立跨仓公共分页框架为前提。

以下链接固定到已核对的源码版本：Gov `ba7db4c`、Resource `98a5348`，避免引用到其他分支的实现。

| 参考内容 | 代码位置 | owner 应参考的规则 |
| --- | --- | --- |
| HTTP 路由与请求响应定义 | Gov：[i_network.proto](https://github.com/zhangzhe-ctrl/ani-governance/blob/ba7db4c30b6ee8a2a8046a1adb1591bf6f01e2a4/api/protos/admin/service/v1/i_network.proto)、[vpc.proto](https://github.com/zhangzhe-ctrl/ani-governance/blob/ba7db4c30b6ee8a2a8046a1adb1591bf6f01e2a4/api/protos/catalog/service/v1/vpc.proto)；Resource：[network.proto](https://github.com/zhangzhe-ctrl/ani-resource-service/blob/98a534804231c7842acaa080fe1ebec066d921b2/api/network/v1/network.proto) | 详情直接返回对象；列表使用 `page/pageSize` 和 `items/total`；分页字段保留是否传入的信息 |
| 参数校验与签名 | Gov：[network_policy.go](https://github.com/zhangzhe-ctrl/ani-governance/blob/ba7db4c30b6ee8a2a8046a1adb1591bf6f01e2a4/pkg/middleware/auth/network_policy.go)、[vpc_page.go](https://github.com/zhangzhe-ctrl/ani-governance/blob/ba7db4c30b6ee8a2a8046a1adb1591bf6f01e2a4/app/admin/service/internal/service/vpc_page.go)、[AK 客户端](https://github.com/zhangzhe-ctrl/ani-governance/blob/ba7db4c30b6ee8a2a8046a1adb1591bf6f01e2a4/scripts/ops/aksk_vpc_client.py)；Resource：[page.go](https://github.com/zhangzhe-ctrl/ani-resource-service/blob/98a534804231c7842acaa080fe1ebec066d921b2/internal/biz/network/page.go) | 参数单值与范围校验、独立默认值、API Key 查询参数验签，以及简单的 `PageWindow{Limit, Offset}` 窗口计算 |
| 身份传递与结果映射 | Gov：[network_service.go](https://github.com/zhangzhe-ctrl/ani-governance/blob/ba7db4c30b6ee8a2a8046a1adb1591bf6f01e2a4/app/admin/service/internal/service/network_service.go)、[network_client.go](https://github.com/zhangzhe-ctrl/ani-governance/blob/ba7db4c30b6ee8a2a8046a1adb1591bf6f01e2a4/app/admin/service/internal/data/network_client.go)；Resource：[network.go](https://github.com/zhangzhe-ctrl/ani-resource-service/blob/98a534804231c7842acaa080fe1ebec066d921b2/internal/service/network/network.go)、[list.go](https://github.com/zhangzhe-ctrl/ani-resource-service/blob/98a534804231c7842acaa080fe1ebec066d921b2/internal/biz/network/list.go) | 可信身份和租户贯穿调用链；保留领域筛选与结果字段；空数组和总数按前端合同返回 |
| 数据查询与计数 | Resource：[vpcs.sql](https://github.com/zhangzhe-ctrl/ani-resource-service/blob/98a534804231c7842acaa080fe1ebec066d921b2/internal/data/network/queries/vpcs.sql)、[postgres.go](https://github.com/zhangzhe-ctrl/ani-resource-service/blob/98a534804231c7842acaa080fe1ebec066d921b2/internal/data/network/postgres.go) | 确定排序后直接执行 `LIMIT/OFFSET`；Count/List 使用相同租户与筛选范围，并在同一个 Repeatable Read 只读事务内执行 |
| 定向测试 | Gov：[参数与签名测试](https://github.com/zhangzhe-ctrl/ani-governance/blob/ba7db4c30b6ee8a2a8046a1adb1591bf6f01e2a4/pkg/middleware/auth/network_policy_test.go)、[Service 测试](https://github.com/zhangzhe-ctrl/ani-governance/blob/ba7db4c30b6ee8a2a8046a1adb1591bf6f01e2a4/app/admin/service/internal/service/network_service_test.go)、[HTTP 联调测试](https://github.com/zhangzhe-ctrl/ani-governance/blob/ba7db4c30b6ee8a2a8046a1adb1591bf6f01e2a4/app/admin/service/internal/service/network_joint_http_test.go) 的 `vpc_pages`；Resource：[page_test.go](https://github.com/zhangzhe-ctrl/ani-resource-service/blob/98a534804231c7842acaa080fe1ebec066d921b2/internal/biz/network/page_test.go)、[vpc_page_integration_test.go](https://github.com/zhangzhe-ctrl/ani-resource-service/blob/98a534804231c7842acaa080fe1ebec066d921b2/internal/data/network/vpc_page_integration_test.go) | 直接查询第三页、参数默认值与非法值、空页和超界页、总数、稳定排序、筛选、租户权限及计数快照 |

Gov 通过 [go.mod](https://github.com/zhangzhe-ctrl/ani-governance/blob/ba7db4c30b6ee8a2a8046a1adb1591bf6f01e2a4/go.mod) 固定正式 Resource 模块版本。各 owner 参考以上分层、查询和测试方式，业务筛选、可见性和授权条件由所属领域维护。参数解析和窗口计算保持清晰边界，后续有实际重复再提取公共代码。

## 模型版本筛选与操作历史（2026-10-21）

- `GET /api/v1/models/{model_id}/versions?page=1&pageSize=20&status=ready`
  的 `status` 可省略；接受 `pending`、`importing`、`ready`、`error`，兼容界面别名
  `downloading`→`importing`、`failed`→`error`。Model 在数据库分页前筛选，返回
  canonical 状态和相同筛选条件的 `total`；Governance 不进行本地筛选或计数。
- `GET /api/v1/models/{model_id}/operations?page=1&pageSize=20` 返回 `items/total`，
  项字段为 `id`、`model_id`、`model_version_id`、`action`、`actor`、`result`、
  `before_state`、`after_state`、`error_class`、`request_id`、`created_at`、`task_id`。
  时间为 RFC 3339，`total` 为十进制字符串。按创建时间、唯一 ID 倒序。
- 这两个 Model 查询的页码为正 int32，页大小为 `1..100`，缺省 `1/20`，不接受其他查询
  参数、重复参数、空值、前导零或查询请求体。操作历史不支持 `status` 筛选。
- `result=accepted` 表示异步导入已受理，不能显示成导入成功；`succeeded` 和
  `failed` 表示该条已记录动作的结果。任务当前结果仍以任务查询为准。历史记录
  仅覆盖 Model 实际产生并能关联到模型的审计事件，不补造未记录的删除等动作。
- 两个入口均经过 Governance 用户认证、`model:service:read` 和 `MODEL` 套餐闸门。
  模型归属由 Model 验证；前端不能指定租户或操作人。

模型详情 `GET /api/v1/models/{model_id}` 的 HTTP 响应直接返回 Model 资源，
没有 `model` 外层；内部 RPC 的 GetModelResponse 保持原形状。

## ModelDev 当前租户执行环境

`POST /api/v1/modeldev/environment:ensure` 显式开通当前租户环境；请求必须为
`application/json` 空对象 `{}`（最多1024字节并紧接EOF），不允许query，且恰有一个
规范非零UUID `Idempotency-Key`。`GET /api/v1/modeldev/environment` 无body/query，
仅返回持久观察。两接口返回 `{"environment": <view>}`，采用snake_case，state为
`PROVISIONING/READY/BLOCKED/NEEDS_REVIEW`，phase为
`NAMESPACE/RESOURCES/EXPERIMENT/VERIFY/PUBLISH/COMPLETE`。未确认的namespace_uid、
experiment_id、binding_digest与last_verified_at省略。只有PROVISIONING返回202和
`Retry-After: 2`；其他存在状态返回200，无登记Get返回404
`ENVIRONMENT_NOT_REGISTERED`。Get不表示本次live健康探测，BLOCKED不表示开通成功。

两接口要求当前tenant用户、当前有效角色的ALL数据范围、独立
`modeldev:manage_environment`/`modeldev:view_environment`权限及对应精确API grant，
仍服从既有MODEL套餐/租户闸门。相同key重试重新授权并读取最新结果，不缓存首次状态。
浏览器不能指定tenant/namespace/cluster/Experiment或覆盖出站x-ani-*。
Ensure/Get不进入执行acceptance、quota或可靠CREATE链。
Prepare/Finalize的严格环境拒绝只输出有限`metadata.environment_reason`，不转发上游message。

During the one-time ModelDev environment maintenance window, startup setting
`ANI_MODELDEV_CREATE_PAUSED=true` returns HTTP 503 with reason
`MODELDEV_CREATE_MAINTENANCE` only for the ModelDev Create action. Existing
authentication and tenant-user restrictions still run; Ensure/Get, queries,
Stop and quota release do not use that gate.


Sandbox 检查点：`POST /api/v1/sandbox/{sandbox_id}/checkpoints` 的 `data` 包含 `name`、`idempotency_key` 和必需的正数 `expected_version`；要求 `sandbox:checkpoint:create` 和 SANDBOX 套餐。返回 `checkpoint_id`、`operation_id`、`state`、`replayed`，表示异步受理而非创建完成；使用 `GET /api/v1/sandbox/operations/{operation_id}` 查询进度。同一 Sandbox、幂等键和参数重放原结果，参数不一致或版本冲突返回 409。`GET /api/v1/sandbox/{sandbox_id}/checkpoints?page=1&pageSize=20` 需要 `sandbox:checkpoint:list` 及 SANDBOX 套餐，按受理时间和 ID 倒序返回 `items/total`，默认 1/20，最大 100。只读取 Resource 持久账本；空列表不代表外部或旧业务没有快照。ready 项按已记录 UID、绑定和 CSI 后端快照身份重新核验；来源变化显示 unknown，权限或依赖故障返回错误。范围为文件系统 crash-consistent，不含内存；实际占用未知时 `size_known=false`。创建、恢复、克隆的真实存储效果及控制器能力尚未联调验证；需 Resource 对应版本、权限登记和授权。

克隆接口 `POST /api/v1/sandbox/{source_sandbox_id}/checkpoints/{checkpoint_id}:clone` 直接创建新的独立 Sandbox；`data` 包含 `idempotency_key`、源 Sandbox 的 `expected_version`、目标名称与规格、镜像选择和可选 `egress_intent`，需 SANDBOX 套餐、`sandbox:checkpoint:clone` 和 `sandbox:create`。出口意图沿用普通创建的校验、受限策略拒绝和历史精确回放规则。返回目标 Sandbox ID 与异步 operation ID；克隆完成后目标处于暂停状态，须另行恢复启动。它只复制工作盘快照，不复制进程或内存；请求不接受 PVC、CSI 标识或租户自报。克隆盘的来源引用未释放前，删除源 Sandbox 会返回引用阻挡原因。代码已在本地完成生成和定向回归，尚未部署或现场验收；禁止通过普通 Sandbox 创建接口先创建空工作盘来模拟克隆。

恢复接口 `POST /api/v1/sandbox/{sandbox_id}/checkpoints/{checkpoint_id}:restore` 接收 `data.idempotency_key` 和正数 `data.expected_version`，需 `sandbox:checkpoint:restore` 和 SANDBOX 套餐。Resource 须在锁内确认同租户检查点及 Sandbox 已暂停，返回异步 operation；恢复仅覆盖文件系统，不恢复内存或进程。恢复和克隆当前不支持 `WaitForFirstConsumer` 存储类；明确拒绝后不因后台重试或配置变化而迟到创建。恢复切盘后的启动失败不会自动切回旧盘。代码已在本地完成生成和定向回归，尚未部署或现场验收。

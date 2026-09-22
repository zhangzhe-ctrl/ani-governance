# 最终合同（contract）

接口状态以 docs/interface-integration-register.md 为准；本文件是本批交付的报文快照。

## 管理接口（正式构建）

- `GET /admin/v1/quota-definitions`（QUOTA-01，平台管理，TENANT 模块）
  ```json
  {"items":[{"code":"gpu.count","displayName":"GPU 占用","unit":"gpu","accountingKind":"CONCURRENT","enforcement":"LAB_ONLY"}],"total":4}
  ```
- `GET /admin/v1/tenants/{id}/quota-accounts`（QUOTA-02）
  ```json
  {"tenantId":1,"items":[{"quotaCode":"gpu.count","unit":"gpu","limit":"8","occupied":"2","available":"6","overLimit":false,"enforcement":"LAB_ONLY"}]}
  ```
- 复用 PLAN-11～14：PlanQuota 新增 `quotaCode`（tag 5），`quotaType` deprecated；Update 必须非空 updateMask；code/type 同现必须一致否则 400 INVALID_QUOTA_REQUEST；同套餐同 code 唯一（409）。
- 复用 TENANT-08：QuotaUsage 新增 `quotaCode`；gpu.count 行旧枚举投影为 UNSPECIFIED。

## 实验接口（仅 quota_lab 构建）

- `POST /api/v1/quota-lab/gpu-allocations`（QUOTA-LAB-01）：`{"data":{"name":"lab-gpu-1","gpuCount":2}}` + `Idempotency-Key`（UUID 必需）→ 202 `{"operationId","chargeId","resourceId"}`；202 仅表示治理侧持久接受。
- `GET /api/v1/quota-lab/gpu-allocations/{resource_id}`（QUOTA-LAB-02）：转发 simulator 纯读取，不扣额。
- `DELETE /api/v1/quota-lab/gpu-allocations/{resource_id}`（QUOTA-LAB-03）：202 `{"operationId"}`，不立即退额。
- `GET /api/v1/quota-lab/operations/{operation_id}`（QUOTA-LAB-04）：本用户操作的投递状态（QUEUED/DISPATCHING/UNKNOWN/ACKED/CANCELED_UNSENT）与账本标识。

## 内部退额（QUOTA-03，独立 mTLS gRPC listener）

`quota.service.v1.QuotaReleaseService/ReportQuotaRelease`：请求不含 tenant_id/owner_service；租户与 owner 从 charge 及证书精确 DNS SAN 取出（lab owner=ani-gpu-simulator，TLS≥1.3，RequireAndVerifyClientCert）。

累计释放语义：`new_total=max(stored,incoming)`；`delta=new_total-stored`；`occupied-=delta`。同 event 同内容幂等返回权威累计；同 event 异内容 gRPC FailedPrecondition QUOTA_RELEASE_CONFLICT；released_total 越界/跨 operation/code 不匹配 → FailedPrecondition；未知 charge → NotFound；错误服务身份 → Unauthenticated/PermissionDenied；存储故障 → Unavailable（owner 保留重试）。

## 错误合同（HTTP 侧）

400 INVALID_QUOTA_REQUEST / 409 IDEMPOTENCY_CONFLICT、QUOTA_EXCEEDED / 403 QUOTA_NOT_CONFIGURED、QUOTA_ADMISSION_DENIED / 503 QUOTA_ADAPTER_UNAVAILABLE、QUOTA_STORAGE_UNAVAILABLE / 404 跨租户对象（不泄露存在性）/ 409 QUOTA_HISTORY_PRESENT（删除有账本历史的租户）。

## 幂等与哈希

- operation 幂等键 `(tenant_id,actor_type,actor_id,action,idempotency_key)`；内容一致性由 request_hash 判定（可信 tenant/actor/action/owner + 校验后业务参数，固定字段顺序，schema_version=1；不含 request-id/时间戳/新 UUID）。
- resource_id 创建时生成并随原操作持久化，重试返回原值。

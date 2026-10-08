# Image publisher 停用边界

Image 状态与 command 的权威归属为 Resource；Governance 负责可信租户/actor、权限检查、公开 HTTP 错误与响应校验。完整状态合同见 [Resource Image API](https://github.com/zhangzhe-ctrl/ani-resource-service/blob/58d28575f523613444216ae93637b715a4463e78/docs/specs/image-api.md)。此链接是固定版本证据，不构成跨仓内部包依赖。

空间已开通、publisher 未签发（version=0）时，`POST /api/v1/images/publisher-credential:disable` 必须返回 HTTP409、reason `CREDENTIAL_NOT_ISSUED`。Resource 的原始错误是 FailedPrecondition，ErrorInfo domain 为 `image.ani.io`；不保存成功停用 command。Governance 保留该稳定 reason，不将未签发响应伪造成 disabled。

已有成功停用 command 优先重放其原结果，即使当前凭证已重新签发。不同 actor 或参数复用同 key 返回 IDEMPOTENCY_CONFLICT；新 key 旧版本返回 VERSION_CONFLICT。已停用的新请求仍需匹配当前 version，不增加外部停用写。成功响应必须是合法的 disabled 元数据；非法成功响应仍拒绝为 503 IMAGE_INVALID_RESPONSE。

## 隔离回归入口

`scripts/image-joint-integration` 中的 `TestImageJointHTTP` 复用实际 PG/Redis、JWT/AK、Casbin、HTTP 路由及租户解析，并将停用合同请求发往独立 Resource 测试进程。进程来自指定 Resource SHA，使用真实 Lifecycle、迁移、受限 runtime role、TenantService、GovernanceTLS 与 GovernanceUnary；只有 Harbor 为隔离 provider fixture。没有跨仓 internal import、local replace 或产品部署。

先在 Fedora 精确 Resource SHA 的干净源码下构建：

```sh
go test -mod=readonly -tags=imageintegration -c -o "$RUN_DIR/resource-contract.test" ./internal/data/image
```

再在精确 Governance SHA 下运行：

```sh
IMAGE_RESOURCE_CONTRACT_BINARY="$RUN_DIR/resource-contract.test" scripts/image-joint-integration
```

两步必须沿用任务 runbook 的互斥锁、总预算、缓存和时限，记录两仓 SHA、二进制 SHA256、命令及退出码。测试 helper 仅存在于 Resource `_test.go`，由 stdin EOF 退出并清理自有库；它不是普通容器业务替身，也不能证明 live Harbor 或 IMG-08/09 接入完成。生成契约未变化，Go module 的既有固定契约版本保持不变；运行服务代码 SHA 单独绑定。

本次修复与失败/成功证据统一登记在 Resource 的 `docs/execution/status.md` 和对应本轮记录，Governance 不建立第二份当前状态账本。

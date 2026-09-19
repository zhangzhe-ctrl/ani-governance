# Fedora 定向验证

所有命令在 `ssh fedora` 执行，`source /home/chabking/ani-governance-runs/network-vpc-20260919-01/tools/env.sh`。工具路径与只供本任务的 file GOPROXY 写入该运行目录；共享缓存只读来源与任务已有私有 Go cache 延续 Model 片。

Governance `backend/`：

```sh
bash scripts/generate-network-slice.sh
go test ./app/admin/service/internal/service ./app/admin/service/internal/data -run 'TestNetwork' -count=1
go test ./app/admin/service/internal/service ./app/admin/service/internal/data -run 'TestModel(ListTrustedIdentity|ClientIdentityAndCancellation|ClientTransportOutage)|TestResourceTenantUUIDPersistence' -count=1
go test ./pkg/middleware/auth -run 'TestModelAuthorizationTenantDomain|TestServer_TenantCheckerGating' -count=1
go build -o "$R/ani-governance" ./app/admin/service/cmd/server
go build -o "$R/tools/probe" scripts/network-lab/probe.go
```

Network：

```sh
go test ./internal/server -run TestGovernanceMTLSBoundary -count=1
go test ./internal/data ./internal/service -run 'TestVPC(ConnectionDeadlineIsDependencyFailure|DependencyDeadlineMapping)' -count=1
go build -o "$R/ani-network-service" ./cmd/ani-network-service
```

生成一致性：对 `api/gen/go/*.go`（递归）、Ent Go 源码和后端 OpenAPI 的文件（数量见 generation-consistency.json）做 SHA256，重新执行同一限定生成命令，比较字节一致性。没有运行 make verify、go test ./...、前端生成/检查、网络平台全套验收。

真实验收：`accept.py R OLD deploy/setup/login/contract/recovery/revoke`，依赖保留的 Model 实验 namespace 与 helper。最后 `collect.py R OLD` 导出脱敏文件。第一次 SQL API 追加遇到显式 ID 播种未推进序列，修复为锁表后安全推进；DB 连接超时原先错误映射为 504，定向修复并复验。初始失败日志与原始 acceptance.jsonl 保留，最终 acceptance.json 汇总同名用例的最后结果。恢复按限时自动重连验收，不承诺 Ready 后第一条请求立即成功。

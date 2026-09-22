# 生产边界报告（BOUND-01～04）

| 项 | 结果 | 方法 |
| --- | --- | --- |
| BOUND-01 正式构建实验路径 404 | pass | `bin/governance`（无 quota_lab tag）以真实配置启动（ANI_QUOTA_ENABLED 关闭），`POST /api/v1/quota-lab/gpu-allocations` 返回 404；正式 OpenAPI（cmd/server/assets/openapi.yaml）grep 无 quota-lab 路由；首次种子（bootstrap SQL）不含实验路径 |
| BOUND-02 go list -deps | pass | `go list -deps ./app/admin/service/cmd/server` 不含 internal/quotalab、不含 simulator 实现包 |
| BOUND-03 构造与默认启动 | pass | 默认（disabled）无内部 mTLS 监听；`ANI_QUOTA_ENABLED=true` 且缺证书/地址时启动 exit≠0（fail-closed，不降级明文） |
| BOUND-04 两次定向生成 | pass | `scripts/generate-quota-slice.sh`（buf 1.60.0 + ent v0.14.6 固定）重跑 exit=0；生成文件由工具产出，未手改 |

## 构建矩阵

- `bin/governance`：正式构建，无实验路由/adapter/控制监听。
- `bin/governance-quota-lab`：`-tags quota_lab`，注册实验路由 + GPU adapter + 双侧控制监听。
- `bin/gpu-simulator`：`-tags quota_lab`，独立进程模拟器。
- 三种服务二进制 + `bin/admin` 输出到不同文件，无覆盖混用。

## 实验路由隔离机制

wiring_quota_default.go（!quota_lab）不导入 quotalab；实验路由经 rest_server.go 的 `extraRouteRegistrar` 参数由 lab 装配注入，正式构建传 nil。内部 mTLS listener 与 lab 控制监听均由环境变量开启；正式构建 owner 映射为空，即使误配 enabled 也会因无 owner 映射启动失败。

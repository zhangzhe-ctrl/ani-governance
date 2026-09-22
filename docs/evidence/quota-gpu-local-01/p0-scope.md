# P0 范围冻结

## 未来改动文件
按 docs/quota-gpu-local-execution-plan.md 第 13.1 节清单执行，本批不超出该清单。

## 构建/实验边界
- 正式构建：不含 quotalab 包、不含实验路由、不含模拟 adapter；`go list -deps` 必须证明（BOUND-02）。
- quota_lab 构建：-tags quota_lab；wiring_quota_lab.go 注册实验路由与 adapter；wiring_quota_default.go 为正式钩子，两文件互斥 build tag。
- 模拟器：cmd/quota-gpu-simulator（quota_lab tag），owner/provider 两个独立 PostgreSQL 数据库。

## API 清单
- QUOTA-01 GET /admin/v1/quota-definitions
- QUOTA-02 GET /admin/v1/tenants/{id}/quota-accounts
- QUOTA-03 gRPC quota.service.v1.QuotaReleaseService/ReportQuotaRelease（仅内部 mTLS listener）
- QUOTA-LAB-01..04 /api/v1/quota-lab/*（仅 quota_lab 构建）
- 复用 PLAN-11～14（增加 quotaCode 兼容）、TENANT-04/08（QuotaUsage 补 quotaCode）

## 本地隔离前提
- Docker 29.6.1，unix socket 本地，context=default；镜像 digest 已锁入 versions.lock
- 任务 run-dir 命名必须含 quota-gpu-local-01；端口由脚本选取并写 manifest
- 四类隔离数据库：governance、gpu owner、模拟 provider、Atlas dev

## 已知前置缺口
- 宿主机无 psql/atlas；使用任务工具目录（buf 1.60.0 已就位）与容器化 psql（postgres 镜像内置）
- Atlas v1.3.0 由 go install 编译（无官方预编译资产），校验值以 go module hash 为准记录

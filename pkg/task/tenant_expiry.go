package task

// TenantExpiryScanTaskType 是租户到期扫描任务的类型常量。
// 该任务为系统级常驻定时任务（不写入 sys_tasks 表），由 asynq 调度器按固定 cron 周期触发，
// handler 为 TaskService.AsyncTenantExpiryScan。
const TenantExpiryScanTaskType = "tenant_expiry_scan"

// TenantExpiryScanCronSpec 是租户到期扫描的系统级 cron 表达式（每小时整点）。
// 该任务不进入 sys_tasks 表，规避 typeName 去重问题，由 NewAsynqServer 启动时注册。
const TenantExpiryScanCronSpec = "0 * * * *"

// TenantExpiryScanTaskData 租户到期扫描任务的载荷（当前无参数）。
type TenantExpiryScanTaskData struct{}

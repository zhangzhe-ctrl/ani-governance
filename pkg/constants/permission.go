package constants

const (
	// SystemPermissionCodePrefix 系统权限代码前缀
	SystemPermissionCodePrefix = "sys:"

	// SystemAccessBackendPermissionCode 系统访问后台权限代码
	SystemAccessBackendPermissionCode = SystemPermissionCodePrefix + "access_backend"

	// SystemManageTenantsPermissionCode 系统管理租户权限代码
	SystemManageTenantsPermissionCode = SystemPermissionCodePrefix + "manage_tenants"

	// SystemAuditLogsPermissionCode 系统审计日志权限代码
	SystemAuditLogsPermissionCode = SystemPermissionCodePrefix + "audit_logs"

	// SystemPlatformAdminPermissionCode 系统平台管理员权限代码
	SystemPlatformAdminPermissionCode = SystemPermissionCodePrefix + "platform_admin"
	// SystemTenantManagerPermissionCode 系统租户管理员权限代码
	SystemTenantManagerPermissionCode = SystemPermissionCodePrefix + "tenant_manager"

	// SystemResetOthersCredentialPermissionCode 重置他人密码的权限代码。
	//
	// 用于支撑多平台角色（平台管理员 / 平台运维 / 平台只读……）按能力矩阵授权：
	// 原先"跨租户重置他人密码"只认 platform:admin 单一角色码，无法表达
	// "平台运维可重置、平台只读不可"。改为按本权限判定后，角色能力完全由权限绑定决定。
	SystemResetOthersCredentialPermissionCode = SystemPermissionCodePrefix + "reset_others_credential"
	// SystemResetOthersMFAPermissionCode 替他人重置 MFA 的权限代码（认证器丢失时的救援重置）。
	SystemResetOthersMFAPermissionCode = SystemPermissionCodePrefix + "reset_others_mfa"

	// SystemPermissionModule 系统权限模块标识
	SystemPermissionModule = "sys"

	// DefaultBizPermissionModule 业务权限模块标识
	DefaultBizPermissionModule = "biz"

	// UncategorizedPermissionGroup 未分类权限组标识
	UncategorizedPermissionGroup = "uncategorized"
)

// ProtectedPermissionCodes 受保护的权限代码列表，禁止删除
var ProtectedPermissionCodes = []string{
	SystemAccessBackendPermissionCode,
	SystemManageTenantsPermissionCode,
	SystemAuditLogsPermissionCode,
	SystemPlatformAdminPermissionCode,
	SystemTenantManagerPermissionCode,
	SystemResetOthersCredentialPermissionCode,
	SystemResetOthersMFAPermissionCode,
}

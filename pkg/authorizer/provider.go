package authorizer

import "context"

// PermissionData 权限数据
type PermissionData struct {
	Path   string
	Method string
	Domain string
}

type PermissionDataArray []PermissionData

// PermissionDataMap 权限数据映射
type PermissionDataMap map[string]PermissionDataArray

// Provider 权限数据提供者接口
type Provider interface {
	// ProvidePolicies 提供策略数据
	ProvidePolicies(ctx context.Context) (PermissionDataMap, error)
}

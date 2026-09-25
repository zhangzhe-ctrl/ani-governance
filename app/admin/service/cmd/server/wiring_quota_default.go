//go:build !quota_lab

package main

// 正式构建的配额装配钩子（计划 §11.1）：不导入 quotalab、不注册实验路由/adapter。

import (
	"go-wind-admin/pkg/localdeps/kratos-bootstrap/bootstrap"
	khttp "github.com/go-kratos/kratos/v2/transport/http"

	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/service"
)

// newQuotaAdapterRegistry 正式构建无任何实验 adapter。
func newQuotaAdapterRegistry(_ *bootstrap.Context) (*service.QuotaAdapterRegistry, func()) {
	return service.NewQuotaAdapterRegistry(), func() {}
}

// quotaInternalOwnerMap 正式构建不允许通过配置加载模拟 owner 身份。
func quotaInternalOwnerMap() map[string]string {
	return nil
}

// quotaLabRouteRegistrar 正式构建不注册实验路由（返回 nil，BOUND-01）。
func quotaLabRouteRegistrar(_ *bootstrap.Context, _ *data.QuotaLedgerRepo, _ *service.QuotaAdapterRegistry, _ *service.QuotaDispatchWorker, _ *data.TenantRepo) func(*khttp.Server) {
	return nil
}

// newGovernanceControl 正式构建无控制监听（返回 nil，不导入实验控制实现）。
func newGovernanceControl(_ *bootstrap.Context, _ *service.QuotaDispatchWorker, _ *data.QuotaLedgerRepo) (nilType, func()) {
	return nil, func() {}
}

// nilType 让返回签名在正式构建下为 nil 指针类型。
type nilType = *struct{}

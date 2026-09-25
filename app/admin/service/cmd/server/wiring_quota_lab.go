//go:build quota_lab

package main

// 仅实验构建的配额装配钩子（计划 §11.1）：注册 GPU 模拟 adapter 与实验路由。

import (
	"os"

	"go-wind-admin/pkg/localdeps/kratos-bootstrap/bootstrap"
	khttp "github.com/go-kratos/kratos/v2/transport/http"

	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/quotalab"
	"go-wind-admin/app/admin/service/internal/server"
	"go-wind-admin/app/admin/service/internal/service"
)

// newQuotaAdapterRegistry 注册模拟器 adapter（配置缺失时不注册，
// 占额前拒绝新操作，§8.2）。
func newQuotaAdapterRegistry(ctx *bootstrap.Context) (*service.QuotaAdapterRegistry, func()) {
	registry := service.NewQuotaAdapterRegistry()
	cfg := quotalab.GpuSimulatorAdapterConfigFromEnv()
	if cfg.Address == "" {
		ctx.NewLoggerHelper("quota-lab/wiring").Warn(ctx.Context(), "ANI_QUOTA_SIMULATOR_ADDR not set; lab GPU adapter not registered (new operations will be rejected before charging)")
		return registry, func() {}
	}
	adapter, cleanup, err := quotalab.NewGpuSimulatorAdapter(cfg)
	if err != nil {
		ctx.NewLoggerHelper("quota-lab/wiring").Errorf(ctx.Context(), "init gpu simulator adapter failed: %s", err.Error())
		return registry, func() {}
	}
	if err = registry.Register(adapter); err != nil {
		ctx.NewLoggerHelper("quota-lab/wiring").Errorf(ctx.Context(), "register gpu simulator adapter failed: %s", err.Error())
		cleanup()
		return registry, func() {}
	}
	return registry, cleanup
}

// quotaInternalOwnerMap 仅 lab 构建注册模拟 owner 身份（§9.3）。
func quotaInternalOwnerMap() map[string]string {
	return map[string]string{
		"ani-gpu-simulator": "ani-gpu-simulator",
	}
}

// newGovernanceControl 启动治理侧任务控制监听（仅 quota_lab；§11.6）。
// 未配置 ANI_QUOTA_LAB_CONTROL_ADDR 时返回 nil。
func newGovernanceControl(
	ctx *bootstrap.Context,
	worker *service.QuotaDispatchWorker,
	ledger *data.QuotaLedgerRepo,
) (*quotalab.GovernanceControl, func()) {
	addr := os.Getenv("ANI_QUOTA_LAB_CONTROL_ADDR")
	if addr == "" {
		return nil, func() {}
	}
	tokenFile := os.Getenv("ANI_QUOTA_LAB_CONTROL_TOKEN_FILE")
	if tokenFile == "" {
		ctx.NewLoggerHelper("quota-lab/wiring").Warn(ctx.Context(), "ANI_QUOTA_LAB_CONTROL_TOKEN_FILE not set; control listener disabled")
		return nil, func() {}
	}
	ctl, err := quotalab.NewGovernanceControl(worker, ledger, addr, tokenFile)
	if err != nil {
		ctx.NewLoggerHelper("quota-lab/wiring").Errorf(ctx.Context(), "init governance control failed: %s", err.Error())
		return nil, func() {}
	}
	if err = ctl.Start(); err != nil {
		return nil, func() {}
	}
	return ctl, func() { ctl.Stop() }
}

// quotaLabRouteRegistrar 注册实验用户入口路由（仅 quota_lab 构建）。
func quotaLabRouteRegistrar(
	ctx *bootstrap.Context,
	ledger *data.QuotaLedgerRepo,
	registry *service.QuotaAdapterRegistry,
	worker *service.QuotaDispatchWorker,
	tenantRepo *data.TenantRepo,
) func(*khttp.Server) {
	return func(s *khttp.Server) {
		labService := quotalab.NewQuotaLabService(ctx, ledger, registry, worker, tenantRepo)
		server.RegisterQuotaLabRoutes(s, labService)
	}
}

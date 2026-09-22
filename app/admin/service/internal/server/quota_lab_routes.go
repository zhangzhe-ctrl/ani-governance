//go:build quota_lab

package server

// 仅实验构建的实验路由注册（计划 §11.1/§10.2）。
// 复用真实认证/租户闸门/Casbin 中间件链；不复制弱化认证的 Governance。

import (
	"context"

	khttp "github.com/go-kratos/kratos/v2/transport/http"

	quotalabpb "go-wind-admin/api/gen/go/quota_lab/service/v1"

	"go-wind-admin/app/admin/service/internal/quotalab"
)

// RegisterQuotaLabRoutes 在真实 REST server 上注册 QUOTA-LAB-01～04。
// 202 状态码由手写 handler 显式设置（不进入正式 OpenAPI）。
func RegisterQuotaLabRoutes(s *khttp.Server, svc *quotalab.QuotaLabService) {
	r := s.Route("/")
	r.POST("/api/v1/quota-lab/gpu-allocations", quotaLabHandler(quotalabpb.OperationQuotaLabServiceCreateGpuAllocation, 202, true, svc.CreateGpuAllocation))
	r.GET("/api/v1/quota-lab/gpu-allocations/{resource_id}", quotaLabHandler(quotalabpb.OperationQuotaLabServiceGetGpuAllocation, 200, false, svc.GetGpuAllocation))
	r.DELETE("/api/v1/quota-lab/gpu-allocations/{resource_id}", quotaLabHandler(quotalabpb.OperationQuotaLabServiceDeleteGpuAllocation, 202, false, svc.DeleteGpuAllocation))
	r.GET("/api/v1/quota-lab/operations/{operation_id}", quotaLabHandler(quotalabpb.OperationQuotaLabServiceGetQuotaOperation, 200, false, svc.GetQuotaOperation))
}

// quotaLabHandler 与 access_key 的 keyHTTPHandler 同构：bind → middleware → result。
func quotaLabHandler[Req any, Reply any](operation string, status int, body bool, call func(context.Context, *Req) (*Reply, error)) func(khttp.Context) error {
	return func(ctx khttp.Context) error {
		in := new(Req)
		if body {
			if err := ctx.Bind(in); err != nil {
				return err
			}
		}
		if err := ctx.BindQuery(in); err != nil {
			return err
		}
		if err := ctx.BindVars(in); err != nil {
			return err
		}
		khttp.SetOperation(ctx, operation)
		handler := ctx.Middleware(func(ctx context.Context, req interface{}) (interface{}, error) { return call(ctx, req.(*Req)) })
		out, err := handler(ctx, in)
		if err != nil {
			return err
		}
		return ctx.Result(status, out.(*Reply))
	}
}

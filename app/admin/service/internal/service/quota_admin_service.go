package service

import (
	"context"

	bLogger "go-wind-admin/pkg/localdeps/kratos-bootstrap/logger"
	paginationV1 "go-wind-admin/pkg/localdeps/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/pkg/localdeps/kratos-bootstrap/bootstrap"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"

	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/pkg/middleware/auth"
)

// QuotaAdminService 实现 QUOTA-01/02（配额目录与租户账户读取）。
// 本批仅平台管理权限：租户身份不能通过修改 path id 查询他人账户，
// 未来自助入口另行设计。
type QuotaAdminService struct {
	adminV1.QuotaAdminServiceHTTPServer

	log            *bLogger.Helper
	quotaAdminRepo *data.QuotaAdminRepo
}

func NewQuotaAdminService(
	ctx *bootstrap.Context,
	quotaAdminRepo *data.QuotaAdminRepo,
) *QuotaAdminService {
	return &QuotaAdminService{
		log:            ctx.NewLoggerHelper("quota-admin/service/admin-service"),
		quotaAdminRepo: quotaAdminRepo,
	}
}

// requirePlatformUser 平台用户（tenant_id=0）守卫；其余身份统一 404，
// 不泄露对象存在性。
func requirePlatformUser(ctx context.Context) error {
	p, err := auth.PrincipalFromContext(ctx)
	if err != nil || p == nil {
		return err
	}
	if p.Type != auth.SubjectUser || p.TenantID != 0 {
		return data.QuotaErrNotFound("tenant not found")
	}
	return nil
}

func (s *QuotaAdminService) ListQuotaDefinitions(ctx context.Context, req *paginationV1.PagingRequest) (*adminV1.ListQuotaDefinitionsResponse, error) {
	if err := requirePlatformUser(ctx); err != nil {
		return nil, err
	}
	return s.quotaAdminRepo.ListDefinitions(ctx, req)
}

func (s *QuotaAdminService) ListTenantQuotaAccounts(ctx context.Context, req *adminV1.GetTenantQuotaAccountsRequest) (*adminV1.ListTenantQuotaAccountsResponse, error) {
	if req == nil || req.GetId() == 0 {
		return nil, data.QuotaErrInvalid("tenant id is required")
	}
	if err := requirePlatformUser(ctx); err != nil {
		return nil, err
	}
	return s.quotaAdminRepo.ListTenantAccounts(ctx, req.GetId())
}

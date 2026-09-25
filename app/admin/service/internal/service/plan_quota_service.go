package service

import (
	"context"

	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	paginationV1 "go-wind-admin/pkg/localdeps/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/pkg/localdeps/go-utils/trans"
	"google.golang.org/protobuf/types/known/emptypb"

	"go-wind-admin/app/admin/service/internal/data"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"

	"go-wind-admin/pkg/middleware/auth"
)

type PlanQuotaService struct {
	adminV1.PlanQuotaServiceHTTPServer

	log           *bLogger.Helper
	planQuotaRepo *data.PlanQuotaRepo
}

func NewPlanQuotaService(
	ctx *bootstrap.Context,
	planQuotaRepo *data.PlanQuotaRepo,
) *PlanQuotaService {
	return &PlanQuotaService{
		log:           ctx.NewLoggerHelper("plan-quota/service/admin-service"),
		planQuotaRepo: planQuotaRepo,
	}
}

func (s *PlanQuotaService) List(ctx context.Context, req *paginationV1.PagingRequest) (*identityV1.ListPlanQuotaResponse, error) {
	return s.planQuotaRepo.List(ctx, req)
}

func (s *PlanQuotaService) Get(ctx context.Context, req *identityV1.GetPlanQuotaRequest) (*identityV1.PlanQuota, error) {
	return s.planQuotaRepo.Get(ctx, req)
}

func (s *PlanQuotaService) Create(ctx context.Context, req *identityV1.CreatePlanQuotaRequest) (*emptypb.Empty, error) {
	if req == nil || req.Data == nil {
		return nil, adminV1.ErrorBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.Data.CreatedBy = trans.Ptr(operator.UserId)

	if err = s.planQuotaRepo.Create(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *PlanQuotaService) Update(ctx context.Context, req *identityV1.UpdatePlanQuotaRequest) (*emptypb.Empty, error) {
	if req == nil || req.Data == nil {
		return nil, adminV1.ErrorBadRequest("invalid parameter")
	}
	// 计划 §5.3：Update 必须非空 updateMask；不允许 allowMissing 隐式创建。
	if req.GetUpdateMask() == nil || len(req.GetUpdateMask().GetPaths()) == 0 {
		return nil, data.QuotaErrInvalid("update_mask is required")
	}
	if req.GetAllowMissing() {
		return nil, data.QuotaErrInvalid("allow_missing is not allowed for plan quotas")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.Data.Id = trans.Ptr(req.GetId())

	req.Data.UpdatedBy = trans.Ptr(operator.UserId)
	if req.UpdateMask != nil {
		req.UpdateMask.Paths = append(req.UpdateMask.Paths, "updated_by")
	}

	if err = s.planQuotaRepo.Update(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *PlanQuotaService) Delete(ctx context.Context, req *identityV1.DeletePlanQuotaRequest) (*emptypb.Empty, error) {
	if err := s.planQuotaRepo.Delete(ctx, req.GetId()); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

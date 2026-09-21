package service

import (
	"context"

	"github.com/go-kratos/kratos/v2/transport/http"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/go-utils/trans"
	"google.golang.org/protobuf/types/known/emptypb"

	"go-wind-admin/app/admin/service/cmd/server/assets"
	"go-wind-admin/app/admin/service/internal/data"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"

	"go-wind-admin/pkg/authorizer"

	"go-wind-admin/pkg/middleware/auth"
)

type RouteWalker interface {
	WalkRoute(fn http.WalkRouteFunc) error
}

type ApiService struct {
	adminV1.ApiServiceHTTPServer

	log *bLogger.Helper

	repo        *data.ApiRepo
	authorizer  *authorizer.Authorizer
	routeWalker RouteWalker
}

func NewApiService(
	ctx *bootstrap.Context,
	repo *data.ApiRepo,
	authorizer *authorizer.Authorizer,
) *ApiService {
	svc := &ApiService{
		log:        ctx.NewLoggerHelper("api/service/admin-service"),
		repo:       repo,
		authorizer: authorizer,
	}

	// Database initialization is an explicit deployment step; constructors never seed data.

	return svc
}

func (s *ApiService) RegisterRouteWalker(routeWalker RouteWalker) {
	s.routeWalker = routeWalker
}

func (s *ApiService) List(ctx context.Context, req *paginationV1.PagingRequest) (*permissionV1.ListApiResponse, error) {
	return s.repo.List(ctx, req)
}

func (s *ApiService) Get(ctx context.Context, req *permissionV1.GetApiRequest) (*permissionV1.Api, error) {
	return s.repo.Get(ctx, req)
}

func (s *ApiService) Create(ctx context.Context, req *permissionV1.CreateApiRequest) (*emptypb.Empty, error) {
	if req == nil || req.Data == nil {
		return nil, adminV1.ErrorBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.Data.CreatedBy = trans.Ptr(operator.UserId)

	if err = s.repo.Create(ctx, req); err != nil {
		return nil, err
	}

	// 重置权限策略
	if err = s.authorizer.ResetPolicies(ctx); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *ApiService) Update(ctx context.Context, req *permissionV1.UpdateApiRequest) (*emptypb.Empty, error) {
	if req == nil || req.Data == nil {
		return nil, adminV1.ErrorBadRequest("invalid parameter")
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

	if err = s.repo.Update(ctx, req); err != nil {
		return nil, err
	}

	// 重置权限策略
	if err = s.authorizer.ResetPolicies(ctx); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *ApiService) Delete(ctx context.Context, req *permissionV1.DeleteApiRequest) (*emptypb.Empty, error) {
	if err := s.repo.Delete(ctx, req); err != nil {
		return nil, err
	}

	// 重置权限策略
	if err := s.authorizer.ResetPolicies(ctx); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *ApiService) SyncApis(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	if err := s.syncWithOpenAPI(ctx); err != nil {
		return nil, err
	}

	// 重置权限策略
	if err := s.authorizer.ResetPolicies(ctx); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

// syncWithOpenAPI 使用 OpenAPI 文档同步 API 资源
func (s *ApiService) syncWithOpenAPI(ctx context.Context) error {
	return s.repo.SyncOpenAPI(ctx, assets.OpenApiData)
}

// GetWalkRouteData 获取通过 WalkRoute 获取的路由数据，用于调试
func (s *ApiService) GetWalkRouteData(_ context.Context, _ *emptypb.Empty) (*permissionV1.ListApiResponse, error) {
	if s.routeWalker == nil {
		return nil, adminV1.ErrorInternalServerError("router walker is nil")
	}

	resp := &permissionV1.ListApiResponse{
		Items: []*permissionV1.Api{},
	}
	var count uint32 = 0
	if err := s.routeWalker.WalkRoute(func(info http.RouteInfo) error {
		//log.Infof("Path[%s] Method[%s]", info.Path, info.Method)
		count++
		resp.Items = append(resp.Items, &permissionV1.Api{
			Id:     trans.Ptr(count),
			Path:   trans.Ptr(info.Path),
			Method: trans.Ptr(info.Method),
			Status: trans.Ptr(permissionV1.Api_ON),
		})
		return nil
	}); err != nil {
		s.log.Errorf(context.Background(), "failed to walk route: %v", err)
		return nil, adminV1.ErrorInternalServerError("failed to walk route")
	}
	resp.Total = uint64(count)

	return resp, nil
}

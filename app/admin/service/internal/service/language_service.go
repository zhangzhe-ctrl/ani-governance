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
	dictV1 "go-wind-admin/api/gen/go/dict/service/v1"

	"go-wind-admin/pkg/middleware/auth"
)

type LanguageService struct {
	adminV1.LanguageServiceHTTPServer

	log *bLogger.Helper

	languageRepo *data.LanguageRepo
}

func NewLanguageService(
	ctx *bootstrap.Context,
	languageRepo *data.LanguageRepo,
) *LanguageService {
	svc := &LanguageService{
		log:          ctx.NewLoggerHelper("language/service/admin-service"),
		languageRepo: languageRepo,
	}

	// Database initialization is an explicit deployment step; constructors never seed data.

	return svc
}

func (s *LanguageService) List(ctx context.Context, req *paginationV1.PagingRequest) (*dictV1.ListLanguageResponse, error) {
	resp, err := s.languageRepo.List(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *LanguageService) Get(ctx context.Context, req *dictV1.GetLanguageRequest) (*dictV1.Language, error) {
	resp, err := s.languageRepo.Get(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *LanguageService) Create(ctx context.Context, req *dictV1.CreateLanguageRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, dictV1.ErrorBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.Data.CreatedBy = trans.Ptr(operator.UserId)

	if err = s.languageRepo.Create(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *LanguageService) Update(ctx context.Context, req *dictV1.UpdateLanguageRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, dictV1.ErrorBadRequest("invalid parameter")
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

	if err = s.languageRepo.Update(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *LanguageService) Delete(ctx context.Context, req *dictV1.DeleteLanguageRequest) (*emptypb.Empty, error) {
	if err := s.languageRepo.Delete(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

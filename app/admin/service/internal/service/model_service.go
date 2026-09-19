package service

import (
	"context"
	"net/url"

	"github.com/go-kratos/kratos/v2/errors"
	klog "github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/transport"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	modelv1 "github.com/zhangzhe-ctrl/ani-model-service/api/model/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	adminv1 "go-wind-admin/api/gen/go/admin/service/v1"
	catalogv1 "go-wind-admin/api/gen/go/catalog/service/v1"
	"go-wind-admin/pkg/middleware/auth"
)

type ModelLister interface {
	ListModels(context.Context, string, uint32, uint32, string) (*modelv1.ListModelsResponse, error)
}
type ResourceTenantResolver interface {
	ResourceTenantID(context.Context, uint32) (string, error)
}
type ModelService struct {
	adminv1.UnimplementedModelServiceServer
	client  ModelLister
	tenants ResourceTenantResolver
}

func NewModelService(client ModelLister, tenants ResourceTenantResolver) *ModelService {
	return &ModelService{client: client, tenants: tenants}
}

func validateModelQuery(raw string) error {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return errors.BadRequest("INVALID_QUERY", "malformed query")
	}
	for key, vals := range values {
		if (key != "limit" && key != "status") || len(vals) != 1 || vals[0] == "" {
			return errors.BadRequest("INVALID_QUERY", "only single nonempty limit and status parameters are supported")
		}
	}
	return nil
}

func (s *ModelService) ListModels(ctx context.Context, req *catalogv1.ListModelsRequest) (*catalogv1.ListModelsResponse, error) {
	operator, err := auth.FromContext(ctx)
	if err != nil || operator == nil {
		return nil, errors.Unauthorized("INVALID_LOGIN", "login required")
	}
	if operator.GetTenantId() == 0 || operator.GetUserId() == 0 {
		return nil, errors.Forbidden("TENANT_REQUIRED", "tenant user required")
	}
	if req == nil {
		return nil, errors.BadRequest("INVALID_QUERY", "request required")
	}
	if tr, ok := transport.FromServerContext(ctx); ok {
		if ht, ok := tr.(*khttp.Transport); ok {
			if err := validateModelQuery(ht.Request().URL.RawQuery); err != nil {
				return nil, err
			}
		}
	}
	limit := uint32(100)
	if req.Limit != nil {
		limit = req.GetLimit()
	}
	if limit < 1 || limit > 100 {
		return nil, errors.BadRequest("INVALID_LIMIT", "limit must be between 1 and 100")
	}
	switch req.GetStatus() {
	case "", "pending", "downloading", "ready", "error":
	default:
		return nil, errors.BadRequest("INVALID_STATUS", "unsupported model status")
	}
	tenant, err := s.tenants.ResourceTenantID(ctx, operator.GetTenantId())
	if err != nil {
		return nil, err
	}
	reply, err := s.client.ListModels(ctx, tenant, operator.GetUserId(), limit, req.GetStatus())
	if err != nil {
		return nil, mapModelError(err)
	}
	if reply == nil {
		return nil, errors.ServiceUnavailable("MODEL_UNAVAILABLE", "model catalog unavailable")
	}
	out := &catalogv1.ListModelsResponse{Models: make([]*catalogv1.Model, 0, len(reply.Models))}
	for _, m := range reply.Models {
		if m == nil {
			return nil, errors.ServiceUnavailable("MODEL_INVALID_RESPONSE", "invalid model catalog response")
		}
		out.Models = append(out.Models, &catalogv1.Model{Id: m.Id, ModelId: m.ModelId, Name: m.Name, DisplayName: m.DisplayName, Source: m.Source, Capabilities: m.Capabilities, Status: m.Status})
	}
	return out, nil
}

func mapModelError(err error) error {
	klog.Errorf("model catalog RPC failed: %v", err)
	switch status.Code(err) {
	case codes.DeadlineExceeded:
		return errors.New(504, "MODEL_TIMEOUT", "model catalog timed out")
	case codes.InvalidArgument:
		return errors.BadRequest("INVALID_QUERY", "model catalog rejected query")
	case codes.PermissionDenied:
		return errors.Forbidden("MODEL_ACCESS_DENIED", "model access denied")
	default:
		return errors.ServiceUnavailable("MODEL_UNAVAILABLE", "model catalog unavailable")
	}
}

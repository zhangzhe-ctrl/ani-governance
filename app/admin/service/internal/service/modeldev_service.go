package service

import (
	"context"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01"
	modeldevv1 "go-wind-admin/api/gen/go/modeldev/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
)

// ModelDevCreateInput preserves the parsed user intent, including the
// distinction between omitted general_parameters and an explicit empty array.
// Identity is obtained separately from the trusted request context.
type ModelDevCreateInput struct {
	Intent cpup01.Intent
	IdempotencyKey string
}

// ModelDevService owns the current authorization, original-key lookup and
// durable Governance acceptance sequence. Resolving a candidate does not
// deliver an execution command or start computation.
type ModelDevService struct {
	authorization *data.ModelDevAuthorizationRepo
	tenants ResourceTenantResolver
	acceptances *data.ModelDevAcceptanceRepo
	bindings *data.ModelDevReleaseBindingRepo
	resolver *data.ModelDevClient
}

func NewModelDevService(
	authorization *data.ModelDevAuthorizationRepo,
	tenants ResourceTenantResolver,
	acceptances *data.ModelDevAcceptanceRepo,
	bindings *data.ModelDevReleaseBindingRepo,
	resolver *data.ModelDevClient,
) *ModelDevService {
	return &ModelDevService{
		authorization: authorization,
		tenants: tenants,
		acceptances: acceptances,
		bindings: bindings,
		resolver: resolver,
	}
}

// CreateExecution is called by the strict JSON HTTP adapter. It intentionally
// does not accept a protobuf request: repeated fields cannot retain whether an
// empty general_parameters array was present in the original user request.
func (s *ModelDevService) CreateExecution(context.Context, ModelDevCreateInput) (*modeldevv1.CreateExecutionResponse, error) {
	return nil, errors.New(501, "MODELDEV_CREATE_NOT_IMPLEMENTED", "modeldev create execution not implemented")
}

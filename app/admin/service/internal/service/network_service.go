package service

import (
	"context"
	"github.com/go-kratos/kratos/v2/errors"
	klog "github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/transport"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	networkv1 "github.com/zhangzhe-ctrl/ani-network-service/api/network/v1"
	adminv1 "go-wind-admin/api/gen/go/admin/service/v1"
	catalogv1 "go-wind-admin/api/gen/go/catalog/service/v1"
	"go-wind-admin/pkg/middleware/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"regexp"
	"strings"
)

// ResourceTenantResolver 把治理中心 uint32 租户主键换为下游服务的 resource tenant UUID。
// 原先定义在已删除的 model_service.go 中，现随 Network 接入保留在此；
// 后续其他下游接入复用本接口，勿重复声明。
type ResourceTenantResolver interface {
	ResourceTenantID(context.Context, uint32) (string, error)
}
type VPCGetter interface {
	GetVPC(context.Context, string, uint32, string) (*networkv1.GetVPCResponse, error)
}
type NetworkService struct {
	adminv1.UnimplementedNetworkServiceServer
	client  VPCGetter
	tenants ResourceTenantResolver
}

func NewNetworkService(client VPCGetter, tenants ResourceTenantResolver) *NetworkService {
	return &NetworkService{client: client, tenants: tenants}
}

var vpcIDPattern = regexp.MustCompile(`^vpc_[0-9a-f]{32}$`)

func (s *NetworkService) GetVPC(ctx context.Context, req *catalogv1.GetVPCRequest) (*catalogv1.GetVPCResponse, error) {
	operator, err := auth.FromContext(ctx)
	if err != nil || operator == nil {
		return nil, errors.Unauthorized("INVALID_LOGIN", "login required")
	}
	if operator.GetTenantId() == 0 || operator.GetUserId() == 0 {
		return nil, errors.Forbidden("TENANT_REQUIRED", "tenant user required")
	}
	if req == nil || !vpcIDPattern.MatchString(req.GetVpcId()) {
		return nil, errors.BadRequest("INVALID_VPC_ID", "invalid VPC ID")
	}
	if tr, ok := transport.FromServerContext(ctx); ok {
		if ht, ok := tr.(*khttp.Transport); ok && ht.Request().URL.RawQuery != "" {
			return nil, errors.BadRequest("INVALID_QUERY", "VPC detail accepts no query parameters")
		}
	}
	if s.client == nil {
		return nil, errors.ServiceUnavailable("NETWORK_UNAVAILABLE", "network read access is not configured")
	}
	tenant, err := s.tenants.ResourceTenantID(ctx, operator.GetTenantId())
	if err != nil {
		return nil, err
	}
	reply, err := s.client.GetVPC(ctx, tenant, operator.GetUserId(), req.GetVpcId())
	if err != nil {
		return nil, mapNetworkError(err)
	}
	if reply == nil || reply.Vpc == nil || reply.Vpc.TenantId != tenant || reply.Vpc.Id != req.GetVpcId() || reply.Vpc.State < networkv1.ResourceState_RESOURCE_STATE_PROVISIONING || reply.Vpc.State > networkv1.ResourceState_RESOURCE_STATE_DELETED {
		return nil, errors.ServiceUnavailable("NETWORK_INVALID_RESPONSE", "invalid network response")
	}
	v := reply.Vpc
	return &catalogv1.GetVPCResponse{Vpc: &catalogv1.VPC{Id: v.Id, Name: v.Name, Description: v.Description, Cidr: v.Cidr,
		State: strings.ToLower(strings.TrimPrefix(v.State.String(), "RESOURCE_STATE_")), Reason: v.Reason,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, Version: v.Version, ObservedAt: v.ObservedAt,
		ObservationStale: v.ObservationStale, LastOperationId: v.LastOperationId, SubnetCount: v.SubnetCount}}, nil
}
func mapNetworkError(err error) error {
	klog.Errorf("network VPC RPC failed: %v", err)
	switch status.Code(err) {
	case codes.NotFound:
		return errors.NotFound("VPC_NOT_FOUND", "VPC not found")
	case codes.InvalidArgument:
		return errors.BadRequest("INVALID_VPC_ID", "invalid VPC request")
	case codes.DeadlineExceeded:
		return errors.New(504, "NETWORK_TIMEOUT", "network query timed out")
	// Internal identity rejection is a dependency failure, not a user's login error.
	default:
		return errors.ServiceUnavailable("NETWORK_UNAVAILABLE", "network query unavailable")
	}
}

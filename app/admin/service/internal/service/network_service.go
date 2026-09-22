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
	GetVPC(context.Context, string, string, string) (*networkv1.GetVPCResponse, error)
}
// NetworkTenantClient is the tenant-scoped downstream surface the BFF may call.
// Every method replays the resolved tenant UUID and verified actor; the BFF
// never passes request-derived tenant identity downstream.
type NetworkTenantClient interface {
	VPCGetter
	ListVPCs(context.Context, string, string, string, string, int32, string) (*networkv1.ListVPCsResponse, error)
	CreateVPC(context.Context, string, string, string, string, string, string) (*networkv1.CreateVPCResponse, error)
	DeleteVPC(context.Context, string, string, string) (*networkv1.DeleteVPCResponse, error)
	GetOperation(context.Context, string, string, string) (*networkv1.GetOperationResponse, error)
	GetEIP(context.Context, string, string, string) (*networkv1.GetEIPResponse, error)
	ListEIPs(context.Context, string, string, string, string, int32, string) (*networkv1.ListEIPsResponse, error)
	CreateEIP(context.Context, string, string, string, string, string) (*networkv1.CreateEIPResponse, error)
	DeleteEIP(context.Context, string, string, string) (*networkv1.DeleteEIPResponse, error)
	GetVPCSnat(context.Context, string, string, string) (*networkv1.GetVPCSnatResponse, error)
	BindVPCSnat(context.Context, string, string, string, string, string) (*networkv1.BindVPCSnatResponse, error)
}
type NetworkService struct {
	adminv1.UnimplementedNetworkServiceServer
	client  NetworkTenantClient
	tenants ResourceTenantResolver
}

func NewNetworkService(client NetworkTenantClient, tenants ResourceTenantResolver) *NetworkService {
	return &NetworkService{client: client, tenants: tenants}
}

var vpcIDPattern = regexp.MustCompile(`^vpc_[0-9a-f]{32}$`)
var eipIDPattern = regexp.MustCompile(`^eip_[0-9a-f]{32}$`)
var operationIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// trustedOperator re-authenticates the caller identity shared by every Network
// BFF method; the tenant UUID is resolved from the persisted tenant record.
func trustedOperator(ctx context.Context, s *NetworkService) (*auth.Principal, string, error) {
	operator, err := auth.PrincipalFromContext(ctx)
	if err != nil || operator == nil {
		return nil, "", errors.Unauthorized("INVALID_LOGIN", "login required")
	}
	if operator.TenantID == 0 || operator.ID == 0 {
		return nil, "", errors.Forbidden("TENANT_REQUIRED", "tenant identity required")
	}
	if s.client == nil {
		return nil, "", errors.ServiceUnavailable("NETWORK_UNAVAILABLE", "network read access is not configured")
	}
	if _, err := operator.Actor(); err != nil {
		return nil, "", err
	}
	tenant, err := s.tenants.ResourceTenantID(ctx, operator.TenantID)
	if err != nil {
		return nil, "", err
	}
	return operator, tenant, nil
}

func (s *NetworkService) GetVPC(ctx context.Context, req *catalogv1.GetVPCRequest) (*catalogv1.GetVPCResponse, error) {
	if req == nil || !vpcIDPattern.MatchString(req.GetVpcId()) {
		return nil, errors.BadRequest("INVALID_VPC_ID", "invalid VPC ID")
	}
	if tr, ok := transport.FromServerContext(ctx); ok {
		if ht, ok := tr.(*khttp.Transport); ok {
			if err := auth.ValidateVPCReadRequest(ht.Request()); err != nil {
				return nil, err
			}
		}
	}
	_, tenant, err := trustedOperator(ctx, s)
	if err != nil {
		return nil, err
	}
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := s.client.GetVPC(ctx, tenant, actor, req.GetVpcId())
	if err != nil {
		return nil, mapNetworkError(err)
	}
	if reply == nil || reply.Vpc == nil || reply.Vpc.TenantId != tenant || reply.Vpc.Id != req.GetVpcId() || reply.Vpc.State < networkv1.ResourceState_RESOURCE_STATE_PROVISIONING || reply.Vpc.State > networkv1.ResourceState_RESOURCE_STATE_DELETED {
		return nil, errors.ServiceUnavailable("NETWORK_INVALID_RESPONSE", "invalid network response")
	}
	v := reply.Vpc
	return &catalogv1.GetVPCResponse{Vpc: wireVPC(v)}, nil
}

// actorOf re-derives the actor after trustedOperator validated the principal.
func actorOf(ctx context.Context) (string, error) {
	operator, err := auth.PrincipalFromContext(ctx)
	if err != nil || operator == nil {
		return "", errors.Unauthorized("INVALID_LOGIN", "login required")
	}
	return operator.Actor()
}

func wireVPC(v *networkv1.VPC) *catalogv1.VPC {
	return &catalogv1.VPC{Id: v.Id, Name: v.Name, Description: v.Description, Cidr: v.Cidr,
		State: strings.ToLower(strings.TrimPrefix(v.State.String(), "RESOURCE_STATE_")), Reason: v.Reason,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, Version: v.Version, ObservedAt: v.ObservedAt,
		ObservationStale: v.ObservationStale, LastOperationId: v.LastOperationId, SubnetCount: v.SubnetCount}
}

func wireEIP(v *networkv1.EIP) *catalogv1.EIP {
	r := &catalogv1.EIP{Id: v.Id, Name: v.Name, Description: v.Description,
		State: strings.ToLower(strings.TrimPrefix(v.State.String(), "RESOURCE_STATE_")), Reason: v.Reason,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, Version: v.Version, ObservedAt: v.ObservedAt,
		ObservationStale: v.ObservationStale, LastOperationId: v.LastOperationId, Address: v.Address,
		BindingId: v.BindingId, BindingState: v.BindingState, Scope: v.Scope, ManagedBy: v.ManagedBy}
	if v.BindingTarget != nil {
		r.BindingTarget = &catalogv1.EIPBindingTarget{Kind: v.BindingTarget.Kind, Id: v.BindingTarget.Id, State: v.BindingTarget.State}
	}
	return r
}

func wireSnat(v *networkv1.VPCSnatBinding) *catalogv1.VPCSnat {
	return &catalogv1.VPCSnat{Id: v.Id, VpcId: v.VpcId, EipId: v.EipId, EipAddress: v.EipAddress,
		State: strings.ToLower(strings.TrimPrefix(v.State.String(), "RESOURCE_STATE_")), Reason: v.Reason,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, Version: v.Version,
		DesiredEnabled: v.DesiredEnabled, AppliedEnabled: v.GetAppliedEnabled()}
}

func wireOperation(v *networkv1.Operation) *catalogv1.Operation {
	return &catalogv1.Operation{Id: v.Id, ResourceId: v.ResourceId, ResourceType: strings.ToLower(strings.TrimPrefix(v.ResourceType.String(), "RESOURCE_TYPE_")),
		Kind: strings.ToLower(strings.TrimPrefix(v.Kind.String(), "OPERATION_KIND_")), State: strings.ToLower(strings.TrimPrefix(v.State.String(), "OPERATION_STATE_")),
		Reason: v.Reason, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, CompletedAt: v.CompletedAt, NextAttemptAt: v.NextAttemptAt}
}

func (s *NetworkService) ListVPCs(ctx context.Context, req *catalogv1.ListVPCsRequest) (*catalogv1.ListVPCsResponse, error) {
	_, tenant, err := trustedOperator(ctx, s)
	if err != nil {
		return nil, err
	}
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := s.client.ListVPCs(ctx, tenant, actor, req.GetName(), strings.ToUpper(req.GetState()), req.GetLimit(), req.GetCursor())
	if err != nil {
		return nil, mapNetworkError(err)
	}
	if reply == nil {
		return nil, errors.ServiceUnavailable("NETWORK_INVALID_RESPONSE", "invalid network response")
	}
	out := &catalogv1.ListVPCsResponse{NextCursor: reply.GetNextCursor()}
	for _, v := range reply.GetItems() {
		if v == nil || v.TenantId != tenant {
			return nil, errors.ServiceUnavailable("NETWORK_INVALID_RESPONSE", "invalid network response")
		}
		out.Items = append(out.Items, wireVPC(v))
	}
	return out, nil
}

func (s *NetworkService) CreateVPC(ctx context.Context, req *catalogv1.CreateVPCRequest) (*catalogv1.CreateVPCResponse, error) {
	_, tenant, err := trustedOperator(ctx, s)
	if err != nil {
		return nil, err
	}
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := s.client.CreateVPC(ctx, tenant, actor, req.GetName(), req.GetCidr(), req.GetDescription(), req.GetIdempotencyKey())
	if err != nil {
		return nil, mapNetworkError(err)
	}
	if reply == nil || reply.Vpc == nil || reply.Vpc.TenantId != tenant {
		return nil, errors.ServiceUnavailable("NETWORK_INVALID_RESPONSE", "invalid network response")
	}
	return &catalogv1.CreateVPCResponse{Vpc: wireVPC(reply.Vpc)}, nil
}

func (s *NetworkService) DeleteVPC(ctx context.Context, req *catalogv1.DeleteVPCRequest) (*catalogv1.DeleteVPCResponse, error) {
	if req == nil || !vpcIDPattern.MatchString(req.GetVpcId()) {
		return nil, errors.BadRequest("INVALID_VPC_ID", "invalid VPC ID")
	}
	_, tenant, err := trustedOperator(ctx, s)
	if err != nil {
		return nil, err
	}
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := s.client.DeleteVPC(ctx, tenant, actor, req.GetVpcId())
	if err != nil {
		return nil, mapNetworkError(err)
	}
	if reply == nil || reply.Vpc == nil || reply.Vpc.TenantId != tenant || reply.Vpc.Id != req.GetVpcId() {
		return nil, errors.ServiceUnavailable("NETWORK_INVALID_RESPONSE", "invalid network response")
	}
	return &catalogv1.DeleteVPCResponse{Vpc: wireVPC(reply.Vpc)}, nil
}

func (s *NetworkService) GetOperation(ctx context.Context, req *catalogv1.GetOperationRequest) (*catalogv1.GetOperationResponse, error) {
	if req == nil || !operationIDPattern.MatchString(req.GetOperationId()) {
		return nil, errors.BadRequest("INVALID_OPERATION_ID", "invalid operation ID")
	}
	_, tenant, err := trustedOperator(ctx, s)
	if err != nil {
		return nil, err
	}
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := s.client.GetOperation(ctx, tenant, actor, req.GetOperationId())
	if err != nil {
		return nil, mapNetworkError(err)
	}
	if reply == nil || reply.Operation == nil || reply.Operation.TenantId != tenant || reply.Operation.Id != req.GetOperationId() {
		return nil, errors.ServiceUnavailable("NETWORK_INVALID_RESPONSE", "invalid network response")
	}
	return &catalogv1.GetOperationResponse{Operation: wireOperation(reply.Operation)}, nil
}

func (s *NetworkService) GetEIP(ctx context.Context, req *catalogv1.GetEIPRequest) (*catalogv1.GetEIPResponse, error) {
	if req == nil || !eipIDPattern.MatchString(req.GetEipId()) {
		return nil, errors.BadRequest("INVALID_EIP_ID", "invalid EIP ID")
	}
	_, tenant, err := trustedOperator(ctx, s)
	if err != nil {
		return nil, err
	}
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := s.client.GetEIP(ctx, tenant, actor, req.GetEipId())
	if err != nil {
		return nil, mapNetworkError(err)
	}
	if reply == nil || reply.Eip == nil || reply.Eip.TenantId != tenant || reply.Eip.Id != req.GetEipId() {
		return nil, errors.ServiceUnavailable("NETWORK_INVALID_RESPONSE", "invalid network response")
	}
	return &catalogv1.GetEIPResponse{Eip: wireEIP(reply.Eip)}, nil
}

func (s *NetworkService) ListEIPs(ctx context.Context, req *catalogv1.ListEIPsRequest) (*catalogv1.ListEIPsResponse, error) {
	_, tenant, err := trustedOperator(ctx, s)
	if err != nil {
		return nil, err
	}
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := s.client.ListEIPs(ctx, tenant, actor, req.GetName(), strings.ToUpper(req.GetState()), req.GetLimit(), req.GetCursor())
	if err != nil {
		return nil, mapNetworkError(err)
	}
	if reply == nil {
		return nil, errors.ServiceUnavailable("NETWORK_INVALID_RESPONSE", "invalid network response")
	}
	out := &catalogv1.ListEIPsResponse{NextCursor: reply.GetNextCursor()}
	for _, v := range reply.GetItems() {
		if v == nil || v.TenantId != tenant {
			return nil, errors.ServiceUnavailable("NETWORK_INVALID_RESPONSE", "invalid network response")
		}
		out.Items = append(out.Items, wireEIP(v))
	}
	return out, nil
}

func (s *NetworkService) CreateEIP(ctx context.Context, req *catalogv1.CreateEIPRequest) (*catalogv1.CreateEIPResponse, error) {
	_, tenant, err := trustedOperator(ctx, s)
	if err != nil {
		return nil, err
	}
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := s.client.CreateEIP(ctx, tenant, actor, req.GetName(), req.GetDescription(), req.GetIdempotencyKey())
	if err != nil {
		return nil, mapNetworkError(err)
	}
	if reply == nil || reply.Eip == nil || reply.Eip.TenantId != tenant {
		return nil, errors.ServiceUnavailable("NETWORK_INVALID_RESPONSE", "invalid network response")
	}
	return &catalogv1.CreateEIPResponse{Eip: wireEIP(reply.Eip)}, nil
}

func (s *NetworkService) DeleteEIP(ctx context.Context, req *catalogv1.DeleteEIPRequest) (*catalogv1.DeleteEIPResponse, error) {
	if req == nil || !eipIDPattern.MatchString(req.GetEipId()) {
		return nil, errors.BadRequest("INVALID_EIP_ID", "invalid EIP ID")
	}
	_, tenant, err := trustedOperator(ctx, s)
	if err != nil {
		return nil, err
	}
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := s.client.DeleteEIP(ctx, tenant, actor, req.GetEipId())
	if err != nil {
		return nil, mapNetworkError(err)
	}
	if reply == nil || reply.Eip == nil || reply.Eip.TenantId != tenant || reply.Eip.Id != req.GetEipId() {
		return nil, errors.ServiceUnavailable("NETWORK_INVALID_RESPONSE", "invalid network response")
	}
	return &catalogv1.DeleteEIPResponse{Eip: wireEIP(reply.Eip)}, nil
}

func (s *NetworkService) GetVPCSnat(ctx context.Context, req *catalogv1.GetVPCSnatRequest) (*catalogv1.GetVPCSnatResponse, error) {
	if req == nil || !vpcIDPattern.MatchString(req.GetVpcId()) {
		return nil, errors.BadRequest("INVALID_VPC_ID", "invalid VPC ID")
	}
	_, tenant, err := trustedOperator(ctx, s)
	if err != nil {
		return nil, err
	}
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := s.client.GetVPCSnat(ctx, tenant, actor, req.GetVpcId())
	if err != nil {
		return nil, mapNetworkError(err)
	}
	if reply == nil || reply.Binding == nil || reply.Binding.TenantId != tenant {
		return nil, errors.ServiceUnavailable("NETWORK_INVALID_RESPONSE", "invalid network response")
	}
	return &catalogv1.GetVPCSnatResponse{Snat: wireSnat(reply.Binding)}, nil
}

func (s *NetworkService) BindVPCSnat(ctx context.Context, req *catalogv1.BindVPCSnatRequest) (*catalogv1.BindVPCSnatResponse, error) {
	if req == nil || !vpcIDPattern.MatchString(req.GetVpcId()) || !eipIDPattern.MatchString(req.GetEipId()) {
		return nil, errors.BadRequest("INVALID_SNAT_REQUEST", "invalid VPC or EIP ID")
	}
	_, tenant, err := trustedOperator(ctx, s)
	if err != nil {
		return nil, err
	}
	actor, err := actorOf(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := s.client.BindVPCSnat(ctx, tenant, actor, req.GetVpcId(), req.GetEipId(), req.GetIdempotencyKey())
	if err != nil {
		return nil, mapNetworkError(err)
	}
	if reply == nil || reply.Binding == nil || reply.Binding.TenantId != tenant {
		return nil, errors.ServiceUnavailable("NETWORK_INVALID_RESPONSE", "invalid network response")
	}
	return &catalogv1.BindVPCSnatResponse{Snat: wireSnat(reply.Binding)}, nil
}
func mapNetworkError(err error) error {
	klog.Errorf("network VPC RPC failed: %v", err)
	switch status.Code(err) {
	case codes.NotFound:
		return errors.NotFound("VPC_NOT_FOUND", "VPC not found")
	case codes.InvalidArgument:
		return errors.BadRequest("INVALID_VPC_ID", "invalid VPC request")
	case codes.FailedPrecondition:
		return errors.New(412, "NETWORK_PRECONDITION_FAILED", "network resource is not in the required state")
	case codes.DeadlineExceeded:
		return errors.New(504, "NETWORK_TIMEOUT", "network query timed out")
	// Internal identity rejection is a dependency failure, not a user's login error.
	default:
		return errors.ServiceUnavailable("NETWORK_UNAVAILABLE", "network query unavailable")
	}
}

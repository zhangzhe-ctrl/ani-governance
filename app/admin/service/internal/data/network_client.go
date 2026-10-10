package data

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	networkv1 "github.com/zhangzhe-ctrl/ani-resource-service/api/network/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type NetworkClientConfig struct {
	Address, CAFile, CertFile, KeyFile string
	Timeout                            time.Duration
}
type NetworkClient struct {
	client          networkv1.NetworkServiceClient
	egress          networkv1.TenantEgressServiceClient
	loadBalancers   networkv1.TenantLoadBalancerServiceClient
	timeout         time.Duration
	connectionState func() connectivity.State
}

// NewNetworkClient constructs a fail-closed mTLS client; server identity is fixed.
func NewNetworkClient(c NetworkClientConfig) (*NetworkClient, func(), error) {
	if c.Address == "" || c.Timeout <= 0 {
		return nil, nil, fmt.Errorf("network address and positive timeout are required")
	}
	pem, err := os.ReadFile(c.CAFile)
	if err != nil {
		return nil, nil, fmt.Errorf("read network CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, nil, fmt.Errorf("network CA contains no certificates")
	}
	cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("load governance client certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, nil, err
	}
	trustedName := false
	for _, name := range leaf.DNSNames {
		if name == "ani-governance" {
			trustedName = true
		}
	}
	if !trustedName {
		return nil, nil, fmt.Errorf("governance certificate requires exact DNS SAN ani-governance")
	}
	// This fixed internal contract has no DNS TXT service-config authority.
	conn, err := grpc.NewClient(c.Address, grpc.WithDisableServiceConfig(), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{cert}, ServerName: "ani-network-service",
	})))
	if err != nil {
		return nil, nil, err
	}
	return &NetworkClient{client: networkv1.NewNetworkServiceClient(conn), egress: networkv1.NewTenantEgressServiceClient(conn), loadBalancers: networkv1.NewTenantLoadBalancerServiceClient(conn), timeout: c.Timeout, connectionState: conn.GetState}, func() { _ = conn.Close() }, nil
}

func NetworkConfigFromEnv() (NetworkClientConfig, error) {
	timeout := 3 * time.Second
	if v := os.Getenv("ANI_NETWORK_TIMEOUT"); v != "" {
		var err error
		timeout, err = time.ParseDuration(v)
		if err != nil {
			return NetworkClientConfig{}, err
		}
	}
	return NetworkClientConfig{Address: os.Getenv("ANI_NETWORK_ADDR"), CAFile: os.Getenv("ANI_NETWORK_CA"), CertFile: os.Getenv("ANI_NETWORK_CERT"), KeyFile: os.Getenv("ANI_NETWORK_KEY"), Timeout: timeout}, nil
}

func (c *NetworkClient) GetVPC(ctx context.Context, tenant string, actor string, vpcID string) (*networkv1.GetVPCResponse, error) {
	id, err := uuid.Parse(tenant)
	if err != nil || id == uuid.Nil || id.String() != tenant || !networkActorPattern.MatchString(actor) {
		return nil, fmt.Errorf("invalid trusted network identity")
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	// Rebuild metadata; never append inbound/public identity headers.
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("x-ani-tenant-id", tenant, "x-ani-actor", actor, "x-ani-request-id", uuid.NewString()))
	reply, err := c.client.GetVPC(ctx, &networkv1.GetVPCRequest{TenantId: tenant, VpcId: vpcID})
	// A disconnected transport can spend the entire deadline reconnecting.
	// Report that dependency outage as 503; a connected, slow RPC remains 504.
	if status.Code(err) == codes.DeadlineExceeded && c.connectionState != nil && c.connectionState() != connectivity.Ready {
		return nil, status.Errorf(codes.Unavailable, "network transport unavailable: %v", err)
	}
	return reply, err
}

// trusted validates and carries the replayed tenant UUID and the verified
// actor; both are asserted once per call and never taken from request bodies.
type trusted struct {
	tenant string
	actor  string
}

func (c *NetworkClient) trusted(tenant, actor string) (trusted, error) {
	id, err := uuid.Parse(tenant)
	if err != nil || id == uuid.Nil || id.String() != tenant || !networkActorPattern.MatchString(actor) {
		return trusted{}, fmt.Errorf("invalid trusted network identity")
	}
	return trusted{tenant: tenant, actor: actor}, nil
}

// classify maps a disconnected-transport deadline to dependency outage.
func (c *NetworkClient) classify(err error) error {
	// A disconnected transport can spend the entire deadline reconnecting.
	// Report that dependency outage as 503; a connected, slow RPC remains 504.
	if status.Code(err) == codes.DeadlineExceeded && c.connectionState != nil && c.connectionState() != connectivity.Ready {
		return status.Errorf(codes.Unavailable, "network transport unavailable: %v", err)
	}
	return err
}

// outCall runs one tenant-scoped RPC with rebuilt trusted metadata; never
// append inbound/public identity headers.
func outCall[T any](c *NetworkClient, ctx context.Context, tc trusted, call func(context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("x-ani-tenant-id", tc.tenant, "x-ani-actor", tc.actor, "x-ani-request-id", uuid.NewString()))
	reply, err := call(ctx)
	return reply, c.classify(err)
}

var networkActorPattern = regexp.MustCompile(`^governance:(user|access-key):[1-9][0-9]*$`)

// resourceState maps the wire state string ("" or RESOURCE_STATE_*) to the
// pinned enum; unknown names are rejected before any RPC.
func resourceState(state string) (networkv1.ResourceState, error) {
	if state == "" {
		return networkv1.ResourceState_RESOURCE_STATE_UNSPECIFIED, nil
	}
	if value, ok := networkv1.ResourceState_value[state]; ok {
		return networkv1.ResourceState(value), nil
	}
	if value, ok := networkv1.ResourceState_value["RESOURCE_STATE_"+strings.ToUpper(state)]; ok {
		return networkv1.ResourceState(value), nil
	}
	return 0, fmt.Errorf("invalid state filter")
}

func (c *NetworkClient) ListVPCs(ctx context.Context, tenant, actor string, name, state string, limit int32, cursor string) (*networkv1.ListVPCsResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	resourceState, err := resourceState(state)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.ListVPCsResponse, error) {
		return c.client.ListVPCs(ctx, &networkv1.ListVPCsRequest{TenantId: tc.tenant, Name: name, State: resourceState, Limit: limit, Cursor: cursor})
	})
}

func (c *NetworkClient) CreateVPC(ctx context.Context, tenant, actor string, name, cidr, description, idempotencyKey string) (*networkv1.CreateVPCResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.CreateVPCResponse, error) {
		return c.client.CreateVPC(ctx, &networkv1.CreateVPCRequest{TenantId: tc.tenant, Name: name, Cidr: cidr, Description: description, IdempotencyKey: idempotencyKey,
			Attribution: &networkv1.Attribution{Actor: actor, DirectCaller: "ani-governance", CorrelationId: uuid.NewString()}})
	})
}

func (c *NetworkClient) DeleteVPC(ctx context.Context, tenant, actor, vpcID string) (*networkv1.DeleteVPCResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.DeleteVPCResponse, error) {
		return c.client.DeleteVPC(ctx, &networkv1.DeleteVPCRequest{TenantId: tc.tenant, VpcId: vpcID})
	})
}

func (c *NetworkClient) GetOperation(ctx context.Context, tenant, actor, operationID string) (*networkv1.GetOperationResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.GetOperationResponse, error) {
		return c.client.GetOperation(ctx, &networkv1.GetOperationRequest{TenantId: tc.tenant, OperationId: operationID})
	})
}

func (c *NetworkClient) GetEIP(ctx context.Context, tenant, actor, eipID string) (*networkv1.GetEIPResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.GetEIPResponse, error) {
		return c.egress.GetEIP(ctx, &networkv1.GetEIPRequest{TargetTenantId: tc.tenant, EipId: eipID})
	})
}

func (c *NetworkClient) ListEIPs(ctx context.Context, tenant, actor string, name, state string, limit int32, cursor string) (*networkv1.ListEIPsResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	resourceState, err := resourceState(state)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.ListEIPsResponse, error) {
		return c.egress.ListEIPs(ctx, &networkv1.ListEIPsRequest{TargetTenantId: tc.tenant, Name: name, State: resourceState, Limit: limit, Cursor: cursor})
	})
}

func (c *NetworkClient) CreateEIP(ctx context.Context, tenant, actor string, name, description, idempotencyKey string) (*networkv1.CreateEIPResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.CreateEIPResponse, error) {
		return c.egress.CreateEIP(ctx, &networkv1.CreateEIPRequest{TargetTenantId: tc.tenant, Name: name, Description: description, IdempotencyKey: idempotencyKey})
	})
}

func (c *NetworkClient) DeleteEIP(ctx context.Context, tenant, actor, eipID string) (*networkv1.DeleteEIPResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.DeleteEIPResponse, error) {
		return c.egress.DeleteEIP(ctx, &networkv1.DeleteEIPRequest{TargetTenantId: tc.tenant, EipId: eipID})
	})
}

func (c *NetworkClient) GetVPCSnat(ctx context.Context, tenant, actor, vpcID string) (*networkv1.GetVPCSnatResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.GetVPCSnatResponse, error) {
		return c.egress.GetVPCSnat(ctx, &networkv1.GetVPCSnatRequest{TargetTenantId: tc.tenant, VpcId: vpcID})
	})
}

func (c *NetworkClient) BindVPCSnat(ctx context.Context, tenant, actor, vpcID, eipID, idempotencyKey string) (*networkv1.BindVPCSnatResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.BindVPCSnatResponse, error) {
		return c.egress.BindVPCSnat(ctx, &networkv1.BindVPCSnatRequest{TargetTenantId: tc.tenant, VpcId: vpcID, EipId: eipID, IdempotencyKey: idempotencyKey})
	})
}

func (c *NetworkClient) CreateSubnet(ctx context.Context, tenant, actor, vpcID, name, cidr, description, idempotencyKey string, gateway *string) (*networkv1.CreateSubnetResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.CreateSubnetResponse, error) {
		return c.client.CreateSubnet(ctx, &networkv1.CreateSubnetRequest{TenantId: tc.tenant, VpcId: vpcID, Name: name, Cidr: cidr, Description: description, IdempotencyKey: idempotencyKey, Gateway: gateway,
			Attribution: &networkv1.Attribution{Actor: tc.actor, DirectCaller: "ani-governance", CorrelationId: uuid.NewString()}})
	})
}
func (c *NetworkClient) ListSubnets(ctx context.Context, tenant, actor, vpcID, name, state string, limit int32, cursor string) (*networkv1.ListSubnetsResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	stateValue, err := resourceState(state)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.ListSubnetsResponse, error) {
		return c.client.ListSubnets(ctx, &networkv1.ListSubnetsRequest{TenantId: tc.tenant, VpcId: vpcID, Name: name, State: stateValue, Limit: limit, Cursor: cursor})
	})
}
func (c *NetworkClient) GetSubnet(ctx context.Context, tenant, actor, id string) (*networkv1.GetSubnetResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.GetSubnetResponse, error) {
		return c.client.GetSubnet(ctx, &networkv1.GetSubnetRequest{TenantId: tc.tenant, SubnetId: id})
	})
}
func (c *NetworkClient) DeleteSubnet(ctx context.Context, tenant, actor, id string) (*networkv1.DeleteSubnetResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.DeleteSubnetResponse, error) {
		return c.client.DeleteSubnet(ctx, &networkv1.DeleteSubnetRequest{TenantId: tc.tenant, SubnetId: id})
	})
}
func (c *NetworkClient) GetVPCSnatBinding(ctx context.Context, tenant, actor, id string) (*networkv1.GetVPCSnatBindingResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.GetVPCSnatBindingResponse, error) {
		return c.egress.GetVPCSnatBinding(ctx, &networkv1.GetVPCSnatBindingRequest{TargetTenantId: tc.tenant, BindingId: id})
	})
}
func (c *NetworkClient) DeleteVPCSnatBinding(ctx context.Context, tenant, actor, id string) (*networkv1.DeleteVPCSnatBindingResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.DeleteVPCSnatBindingResponse, error) {
		return c.egress.DeleteVPCSnatBinding(ctx, &networkv1.DeleteVPCSnatBindingRequest{TargetTenantId: tc.tenant, BindingId: id})
	})
}
func (c *NetworkClient) GetLoadBalancer(ctx context.Context, tenant, actor, id string) (*networkv1.GetLoadBalancerResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.GetLoadBalancerResponse, error) {
		return c.loadBalancers.GetLoadBalancer(ctx, &networkv1.GetLoadBalancerRequest{TargetTenantId: tc.tenant, LoadBalancerId: id})
	})
}
func (c *NetworkClient) DeleteLoadBalancer(ctx context.Context, tenant, actor, id string) (*networkv1.DeleteLoadBalancerResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.DeleteLoadBalancerResponse, error) {
		return c.loadBalancers.DeleteLoadBalancer(ctx, &networkv1.DeleteLoadBalancerRequest{TargetTenantId: tc.tenant, LoadBalancerId: id})
	})
}
func (c *NetworkClient) GetLoadBalancerOperation(ctx context.Context, tenant, actor, id string) (*networkv1.GetLoadBalancerOperationResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.GetLoadBalancerOperationResponse, error) {
		return c.loadBalancers.GetLoadBalancerOperation(ctx, &networkv1.GetLoadBalancerOperationRequest{TargetTenantId: tc.tenant, OperationId: id})
	})
}
func (c *NetworkClient) SetVPCSnatEnabled(ctx context.Context, tenant, actor, bindingID string, enabled bool, expectedVersion int64, key string) (*networkv1.SetVPCSnatEnabledResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.SetVPCSnatEnabledResponse, error) {
		return c.egress.SetVPCSnatEnabled(ctx, &networkv1.SetVPCSnatEnabledRequest{TargetTenantId: tc.tenant, BindingId: bindingID, Enabled: enabled, ExpectedVersion: expectedVersion, IdempotencyKey: key})
	})
}
func (c *NetworkClient) CreateLoadBalancer(ctx context.Context, tenant, actor string, request *networkv1.CreateLoadBalancerRequest) (*networkv1.CreateLoadBalancerResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "missing load balancer request")
	}
	// Clone the intent and replace identity; never mutate the caller's snapshot.
	request = proto.Clone(request).(*networkv1.CreateLoadBalancerRequest)
	request.TargetTenantId = tc.tenant
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.CreateLoadBalancerResponse, error) {
		return c.loadBalancers.CreateLoadBalancer(ctx, request)
	})
}
func (c *NetworkClient) UpdateLoadBalancer(ctx context.Context, tenant, actor string, request *networkv1.UpdateLoadBalancerRequest) (*networkv1.UpdateLoadBalancerResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "missing load balancer request")
	}
	// Clone the intent and replace identity; never mutate the caller's snapshot.
	request = proto.Clone(request).(*networkv1.UpdateLoadBalancerRequest)
	request.TargetTenantId = tc.tenant
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.UpdateLoadBalancerResponse, error) {
		return c.loadBalancers.UpdateLoadBalancer(ctx, request)
	})
}
func (c *NetworkClient) ListLoadBalancers(ctx context.Context, tenant, actor string, request *networkv1.ListLoadBalancersRequest) (*networkv1.ListLoadBalancersResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "missing load balancer request")
	}
	// Clone the intent and replace identity; never mutate the caller's snapshot.
	request = proto.Clone(request).(*networkv1.ListLoadBalancersRequest)
	request.TargetTenantId = tc.tenant
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.ListLoadBalancersResponse, error) {
		return c.loadBalancers.ListLoadBalancers(ctx, request)
	})
}

func (c *NetworkClient) ListVPCCIDRPresets(ctx context.Context, tenant, actor string) (*networkv1.ListVPCCIDRPresetsResponse, error) {
	tc, err := c.trusted(tenant, actor)
	if err != nil {
		return nil, err
	}
	return outCall(c, ctx, tc, func(ctx context.Context) (*networkv1.ListVPCCIDRPresetsResponse, error) {
		return c.client.ListVPCCIDRPresets(ctx, &networkv1.ListVPCCIDRPresetsRequest{TenantId: tc.tenant})
	})
}

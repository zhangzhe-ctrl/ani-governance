package service

import (
	"context"
	"errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/stretchr/testify/require"
	terminalpb "github.com/zhangzhe-ctrl/ani-resource-service/api/instance/terminal/v1"
	instancepb "github.com/zhangzhe-ctrl/ani-resource-service/api/instance/v1"
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	view "go-wind-admin/api/gen/go/sandbox/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/pkg/middleware/auth"
)

const sandboxTestTenant = "11111111-1111-4111-8111-111111111111"
const sandboxTestInstance = "instance-1"
const sandboxTestPreset = "22222222-2222-4222-8222-222222222222"

type sandboxTestAuth struct{ calls []string }

func (a *sandboxTestAuth) Authorize(_ context.Context, _ uint32, _ uint32, path, method string) error {
	a.calls = append(a.calls, method+" "+path)
	return nil
}

type sandboxTenantResolver struct{}

func (sandboxTenantResolver) ResourceTenantID(context.Context, uint32) (string, error) {
	return sandboxTestTenant, nil
}

type sandboxTestParent struct{}

func (sandboxTestParent) Resolve(context.Context, string, string, *instancepb.CreateInstanceData, *instancepb.SchedulingOptions) (*instancepb.CreateSpec, error) {
	return nil, nil
}
func (sandboxTestParent) Create(context.Context, *instancepb.AcceptCreateRequest) (*instancepb.OperationResponse, error) {
	return nil, nil
}
func (sandboxTestParent) Delete(context.Context, *instancepb.AcceptDeleteRequest) (*instancepb.OperationResponse, error) {
	return nil, nil
}
func (sandboxTestParent) Change(context.Context, string, string, *instancepb.ChangeInstanceRequest, bool) (*instancepb.OperationResponse, error) {
	return nil, nil
}
func (sandboxTestParent) Get(context.Context, string, string, string) (*instancepb.Instance, error) {
	return &instancepb.Instance{Id: sandboxTestInstance}, nil
}
func (sandboxTestParent) ResolveInstanceMetricScope(context.Context, string, string, string) (*instancepb.InstanceMetricScope, error) {
	return nil, nil
}
func (sandboxTestParent) List(context.Context, string, string, *instancepb.ListInstancesRequest) (*instancepb.ListInstancesResponse, error) {
	return nil, nil
}
func (sandboxTestParent) Operation(context.Context, string, string, string) (*instancepb.Operation, error) {
	return nil, nil
}
func (sandboxTestParent) Target(context.Context, string, string, string, int64) (*terminalpb.TerminalTarget, error) {
	return nil, nil
}

type sandboxTestClient struct {
	resolved                  *res.ResolveCreateSpecResponse
	command                   *res.AcceptCreateRequest
	stored                    *res.Sandbox
	resolveCalls, acceptCalls int
	resolveErr                error
	getErr                    error
	acceptErr                 error
	acceptLookupErr           error
	winner                    *res.Sandbox
	replayed                  bool
	listRequest               *res.ListSandboxesRequest
}

func (c *sandboxTestClient) ResolveSandbox(context.Context, string, string, *res.ResolveCreateSpecRequest) (*res.ResolveCreateSpecResponse, error) {
	c.resolveCalls++
	return c.resolved, c.resolveErr
}
func (c *sandboxTestClient) AcceptSandboxCreate(_ context.Context, _ string, _ string, r *res.AcceptCreateRequest) (*res.AcceptCreateResponse, error) {
	c.command = r
	c.acceptCalls++
	if c.acceptErr != nil {
		c.stored = c.winner
		c.getErr = c.acceptLookupErr
		return nil, c.acceptErr
	}
	c.stored = &res.Sandbox{Id: r.Context.ResourceId, InstanceId: r.InstanceId, CreateOperationId: r.Context.OperationId, CreateIntentDigest: r.CreateIntentDigest, Snapshot: &res.SandboxSnapshot{Name: r.Spec.Name, ImageReference: r.Spec.ImageReference, PresetId: r.Spec.PresetId, Workspace: r.Spec.Workspace}}
	return &res.AcceptCreateResponse{Ack: &res.OperationAck{OperationId: r.Context.OperationId, ResourceId: r.Context.ResourceId, Accepted: true, Replayed: c.replayed}}, nil
}
func (c *sandboxTestClient) GetSandbox(context.Context, string, string, *res.GetSandboxRequest) (*res.GetSandboxResponse, error) {
	if c.getErr != nil {
		return nil, c.getErr
	}
	if c.stored == nil {
		return nil, status.Error(codes.NotFound, "SANDBOX_NOT_FOUND")
	}
	return &res.GetSandboxResponse{Sandbox: c.stored}, nil
}
func (c *sandboxTestClient) ListSandboxes(_ context.Context, _ string, _ string, r *res.ListSandboxesRequest) (*res.ListSandboxesResponse, error) {
	c.listRequest = r
	return &res.ListSandboxesResponse{}, nil
}
func (c *sandboxTestClient) GetSandboxOperation(context.Context, string, string, *res.GetOperationRequest) (*res.GetOperationResponse, error) {
	return nil, nil
}
func (c *sandboxTestClient) PauseSandbox(context.Context, string, string, *res.PauseRequest) (*res.PauseResponse, error) {
	return nil, nil
}
func (c *sandboxTestClient) ResumeSandbox(context.Context, string, string, *res.ResumeRequest) (*res.ResumeResponse, error) {
	return nil, nil
}
func (c *sandboxTestClient) DeleteSandbox(context.Context, string, string, *res.DeleteRequest) (*res.DeleteResponse, error) {
	return nil, nil
}

func TestSandboxCreateUsesMappedTenantAndResourceDigest(t *testing.T) {
	client := &sandboxTestClient{resolved: &res.ResolveCreateSpecResponse{
		Name: "demo", ImageReference: "registry.example/sandbox@sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ImageDigest: "sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PresetId: sandboxTestPreset,
		PresetCode: "cpu.small", PresetName: "CPU Small", ComputeType: "cpu", CpuMillis: 500, MemoryMib: 512,
		SelectionDigest: "selection", ClusterId: "33333333-3333-4333-8333-333333333333", Namespace: "sandbox",
		RuntimeClass: "kata", ContractVersion: 1, SpecDigest: "spec", Workspace: &res.WorkspaceSpec{MountPath: "/workspace", Size: "10Gi", StorageClass: "fast", ReclaimPolicy: "Delete"},
	}}
	authz := new(sandboxTestAuth)
	s := NewSandboxService(client, sandboxTestParent{}, authz, sandboxTenantResolver{})
	ctx := auth.NewPrincipalContext(context.Background(), &auth.Principal{Type: auth.SubjectUser, ID: 7, TenantID: 42})
	got, err := s.CreateSandbox(ctx, &view.CreateSandboxRequest{Data: &view.CreateSandboxData{InstanceId: sandboxTestInstance, IdempotencyKey: "create-1", Name: "demo", ImageReference: client.resolved.ImageReference, PresetId: sandboxTestPreset}})
	require.NoError(t, err)
	require.True(t, got.Accepted)
	require.Equal(t, sandboxTestTenant, client.command.Context.TenantId)
	require.Equal(t, got.ResourceId, client.command.Context.ResourceId)
	require.Equal(t, got.OperationId, client.command.Context.OperationId)
	wantDigest, err := res.CommandDigest(client.command.Context, sandboxTestInstance, client.resolved, client.command.CreateIntentDigest)
	require.NoError(t, err)
	require.Equal(t, wantDigest, client.command.Context.RequestDigest)
	require.Contains(t, authz.calls, "POST "+data.SandboxCreatePath)
}

func sandboxReplayFixture() (*SandboxService, *sandboxTestClient, context.Context, *view.CreateSandboxRequest) {
	client := &sandboxTestClient{resolved: &res.ResolveCreateSpecResponse{Name: "demo", ImageReference: "registry.example/sandbox@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PresetId: sandboxTestPreset, Workspace: &res.WorkspaceSpec{Size: "10Gi"}}}
	s := NewSandboxService(client, sandboxTestParent{}, new(sandboxTestAuth), sandboxTenantResolver{})
	ctx := auth.NewPrincipalContext(context.Background(), &auth.Principal{Type: auth.SubjectUser, ID: 7, TenantID: 42})
	request := &view.CreateSandboxRequest{Data: &view.CreateSandboxData{InstanceId: sandboxTestInstance, IdempotencyKey: "create-1", Name: "demo", ImageReference: client.resolved.ImageReference, PresetId: sandboxTestPreset}}
	return s, client, ctx, request
}
func TestSandboxCreateReplayUsesFrozenIntent(t *testing.T) {
	s, client, ctx, request := sandboxReplayFixture()
	first, err := s.CreateSandbox(ctx, request)
	require.NoError(t, err)
	require.False(t, first.Replayed)
	require.NotEmpty(t, client.command.CreateIntentDigest)
	client.resolved.PresetName = "renamed preset"
	replay, err := s.CreateSandbox(ctx, request)
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.Equal(t, first.ResourceId, replay.ResourceId)
	require.Equal(t, first.OperationId, replay.OperationId)
	client.resolveErr = errors.New("preset has been disabled or deleted")
	replay, err = s.CreateSandbox(ctx, request)
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.Equal(t, first.OperationId, replay.OperationId)
	require.Equal(t, 1, client.resolveCalls)
	require.Equal(t, 1, client.acceptCalls)
	request.Data.Name = "changed"
	_, err = s.CreateSandbox(ctx, request)
	require.Equal(t, 409, kratoserrors.Code(err))
	request.Data.Name = "demo"
	request.Data.WorkspaceSize = "10Gi"
	_, err = s.CreateSandbox(ctx, request)
	require.Equal(t, 409, kratoserrors.Code(err))
	require.Equal(t, 1, client.resolveCalls)
}
func TestSandboxLegacyCreateReplayUsesFrozenSnapshot(t *testing.T) {
	s, client, ctx, request := sandboxReplayFixture()
	first, err := s.CreateSandbox(ctx, request)
	require.NoError(t, err)
	client.stored.CreateIntentDigest = ""
	client.resolveErr = errors.New("catalog unavailable")
	replay, err := s.CreateSandbox(ctx, request)
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.Equal(t, first.OperationId, replay.OperationId)
	request.Data.WorkspaceSize = "11Gi"
	_, err = s.CreateSandbox(ctx, request)
	require.Equal(t, 409, kratoserrors.Code(err))
	require.Equal(t, 1, client.resolveCalls)
}
func TestSandboxConcurrentCreateReplayAck(t *testing.T) {
	s, client, ctx, request := sandboxReplayFixture()
	client.replayed = true
	got, err := s.CreateSandbox(ctx, request)
	require.NoError(t, err)
	require.True(t, got.Replayed)
}
func TestSandboxServicePaginationDefaultsAndValidation(t *testing.T) {
	s, client, ctx, _ := sandboxReplayFixture()
	_, err := s.ListSandboxes(ctx, &view.ListSandboxesRequest{InstanceId: sandboxTestInstance})
	require.NoError(t, err)
	require.EqualValues(t, 1, client.listRequest.Page)
	require.EqualValues(t, 20, client.listRequest.PageSize)
	for _, pair := range [][2]uint32{{1, 0}, {0, 20}, {1, 101}} {
		_, err = s.ListSandboxes(ctx, &view.ListSandboxesRequest{InstanceId: sandboxTestInstance, Page: &pair[0], PageSize: &pair[1]})
		require.Equal(t, 400, kratoserrors.Code(err))
	}
}

func TestSandboxCreateLookupFailureDoesNotResolve(t *testing.T) {
	s, client, ctx, request := sandboxReplayFixture()
	client.getErr = status.Error(codes.Unavailable, "database unavailable")
	_, err := s.CreateSandbox(ctx, request)
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Zero(t, client.resolveCalls)
	require.Zero(t, client.acceptCalls)
}

func TestSandboxConcurrentCreateCatalogChangeReplaysWinner(t *testing.T) {
	s, client, ctx, request := sandboxReplayFixture()
	intent, err := res.CreateIntentDigest(request.Data.InstanceId, &res.CreateSandboxData{Name: request.Data.Name, ImageReference: request.Data.ImageReference, PresetId: request.Data.PresetId})
	require.NoError(t, err)
	client.winner = &res.Sandbox{Id: stableSandboxID(sandboxTestTenant, "create-1", "resource"), InstanceId: sandboxTestInstance, CreateOperationId: stableSandboxID(sandboxTestTenant, "create-1", "operation"), CreateIntentDigest: intent, Snapshot: &res.SandboxSnapshot{Name: "demo", CpuMillis: 500}}
	client.resolved.CpuMillis = 2000
	client.acceptErr = status.Error(codes.AlreadyExists, "SANDBOX_IDEMPOTENCY_CONFLICT")
	got, err := s.CreateSandbox(ctx, request)
	require.NoError(t, err)
	require.True(t, got.Replayed)
	require.Equal(t, client.winner.CreateOperationId, got.OperationId)
	require.Equal(t, 1, client.resolveCalls)
	require.Equal(t, 1, client.acceptCalls)
	client.stored = nil
	client.winner.CreateIntentDigest = "different"
	_, err = s.CreateSandbox(ctx, request)
	require.Equal(t, 409, kratoserrors.Code(err))
	client.stored = nil
	client.acceptLookupErr = status.Error(codes.Unavailable, "lookup database unavailable")
	_, err = s.CreateSandbox(ctx, request)
	require.Equal(t, codes.Unavailable, status.Code(err))
}

// This client limits the race to the external Resource boundary: the first
// scoped read is absent, then another delivery can become accepted while the
// attempted resolution or acceptance fails.
type sandboxFailedDeliveryClient struct {
	*sandboxTestClient
	stage     string
	failure   error
	winner    *res.Sandbox
	lookupErr error
	cancel    context.CancelFunc
	lookups   []*res.GetSandboxRequest
	tenants   []string
	actors    []string
}

func (c *sandboxFailedDeliveryClient) GetSandbox(_ context.Context, tenant, actor string, r *res.GetSandboxRequest) (*res.GetSandboxResponse, error) {
	c.lookups = append(c.lookups, &res.GetSandboxRequest{TenantId: r.TenantId, InstanceId: r.InstanceId, SandboxId: r.SandboxId})
	c.tenants, c.actors = append(c.tenants, tenant), append(c.actors, actor)
	if len(c.lookups) == 1 {
		return nil, status.Error(codes.NotFound, "SANDBOX_NOT_FOUND")
	}
	if c.lookupErr != nil {
		return nil, c.lookupErr
	}
	if c.winner == nil {
		return nil, status.Error(codes.NotFound, "SANDBOX_NOT_FOUND")
	}
	return &res.GetSandboxResponse{Sandbox: c.winner}, nil
}
func (c *sandboxFailedDeliveryClient) ResolveSandbox(ctx context.Context, tenant, actor string, r *res.ResolveCreateSpecRequest) (*res.ResolveCreateSpecResponse, error) {
	if c.stage != "resolve" && c.stage != "nil_resolve" {
		return c.sandboxTestClient.ResolveSandbox(ctx, tenant, actor, r)
	}
	c.resolveCalls++
	if c.cancel != nil {
		c.cancel()
	}
	return nil, c.failure
}
func (c *sandboxFailedDeliveryClient) AcceptSandboxCreate(ctx context.Context, tenant, actor string, r *res.AcceptCreateRequest) (*res.AcceptCreateResponse, error) {
	if c.stage != "accept" && c.stage != "nil_ack" {
		return c.sandboxTestClient.AcceptSandboxCreate(ctx, tenant, actor, r)
	}
	c.acceptCalls++
	if c.cancel != nil {
		c.cancel()
	}
	return nil, c.failure
}

func failedSandboxDelivery(t *testing.T, stage string, failure error) (*SandboxService, *sandboxFailedDeliveryClient, context.Context, *view.CreateSandboxRequest) {
	t.Helper()
	s, base, ctx, request := sandboxReplayFixture()
	intent, err := res.CreateIntentDigest(request.Data.InstanceId, &res.CreateSandboxData{Name: request.Data.Name, ImageReference: request.Data.ImageReference, PresetId: request.Data.PresetId})
	require.NoError(t, err)
	c := &sandboxFailedDeliveryClient{sandboxTestClient: base, stage: stage, failure: failure,
		winner: &res.Sandbox{TenantId: sandboxTestTenant, Id: stableSandboxID(sandboxTestTenant, request.Data.IdempotencyKey, "resource"), InstanceId: request.Data.InstanceId, CreateOperationId: stableSandboxID(sandboxTestTenant, request.Data.IdempotencyKey, "operation"), CreateIntentDigest: intent}}
	s.client = c
	return s, c, ctx, request
}

func TestSandboxFailedDeliveryReplaysPersistedWinner(t *testing.T) {
	for _, tc := range []struct {
		name, stage string
		failure     error
	}{
		{"preset_disabled", "resolve", status.Error(codes.FailedPrecondition, "COMPUTE_PRESET_DISABLED")},
		{"preset_deleted", "resolve", status.Error(codes.NotFound, "COMPUTE_PRESET_NOT_FOUND")},
		{"catalog_unavailable", "resolve", status.Error(codes.Unavailable, "catalog unavailable")},
		{"nil_resolution", "nil_resolve", nil},
		{"changed_deployment_policy", "accept", status.Error(codes.InvalidArgument, "INVALID_SANDBOX_REQUEST")},
		{"accept_response_lost", "accept", status.Error(codes.Unavailable, "accept response lost")},
		{"accept_conflict", "accept", status.Error(codes.AlreadyExists, "SANDBOX_IDEMPOTENCY_CONFLICT")},
		{"nil_acceptance", "nil_ack", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, client, ctx, request := failedSandboxDelivery(t, tc.stage, tc.failure)
			got, err := s.CreateSandbox(ctx, request)
			require.NoError(t, err)
			require.True(t, got.Accepted)
			require.True(t, got.Replayed)
			require.Equal(t, client.winner.Id, got.ResourceId)
			require.Equal(t, client.winner.CreateOperationId, got.OperationId)
			require.Len(t, client.lookups, 2)
			for i, lookup := range client.lookups {
				require.Equal(t, sandboxTestTenant, client.tenants[i])
				require.Equal(t, sandboxTestTenant, lookup.TenantId)
				require.Equal(t, "governance:user:7", client.actors[i])
				require.Equal(t, request.Data.InstanceId, lookup.InstanceId)
				require.Equal(t, got.ResourceId, lookup.SandboxId)
			}
			require.Equal(t, 1, client.resolveCalls)
			if tc.stage == "resolve" || tc.stage == "nil_resolve" {
				require.Zero(t, client.acceptCalls)
			} else {
				require.Equal(t, 1, client.acceptCalls)
			}
		})
	}
}

func TestSandboxFailedDeliveryDoesNotInventAcceptance(t *testing.T) {
	for _, stage := range []string{"resolve", "accept"} {
		t.Run(stage, func(t *testing.T) {
			original := status.Error(codes.Unavailable, "original dependency failure")
			t.Run("still_absent", func(t *testing.T) {
				s, client, ctx, request := failedSandboxDelivery(t, stage, original)
				client.winner = nil
				got, err := s.CreateSandbox(ctx, request)
				require.Nil(t, got)
				require.Equal(t, original, err)
				require.Len(t, client.lookups, 2)
			})
			t.Run("different_intent", func(t *testing.T) {
				s, client, ctx, request := failedSandboxDelivery(t, stage, original)
				client.winner.CreateIntentDigest = "different-intent"
				got, err := s.CreateSandbox(ctx, request)
				require.Nil(t, got)
				require.Equal(t, 409, kratoserrors.Code(err))
				require.Equal(t, "SANDBOX_IDEMPOTENCY_CONFLICT", kratoserrors.Reason(err))
			})
			t.Run("lookup_unavailable", func(t *testing.T) {
				s, client, ctx, request := failedSandboxDelivery(t, stage, original)
				client.lookupErr = status.Error(codes.Unavailable, "accepted-record lookup failed")
				got, err := s.CreateSandbox(ctx, request)
				require.Nil(t, got)
				require.Equal(t, client.lookupErr, err)
			})
			t.Run("wrong_parent", func(t *testing.T) {
				s, client, ctx, request := failedSandboxDelivery(t, stage, original)
				client.winner.InstanceId = "other-parent"
				got, err := s.CreateSandbox(ctx, request)
				require.Nil(t, got)
				require.Equal(t, 503, kratoserrors.Code(err))
				require.Equal(t, "SANDBOX_INVALID_RESPONSE", kratoserrors.Reason(err))
				require.Len(t, client.lookups, 2)
			})
			t.Run("foreign_tenant_resource", func(t *testing.T) {
				s, client, ctx, request := failedSandboxDelivery(t, stage, original)
				client.winner.Id = stableSandboxID("99999999-9999-4999-8999-999999999999", request.Data.IdempotencyKey, "resource")
				got, err := s.CreateSandbox(ctx, request)
				require.Nil(t, got)
				require.Equal(t, 503, kratoserrors.Code(err))
				require.Equal(t, "SANDBOX_INVALID_RESPONSE", kratoserrors.Reason(err))
				require.Len(t, client.lookups, 2)
			})
		})
	}
}

func TestSandboxFailedDeliveryDoesNotLookupAfterCancellation(t *testing.T) {
	for _, stage := range []string{"resolve", "accept"} {
		t.Run(stage, func(t *testing.T) {
			original := status.Error(codes.Canceled, "request canceled")
			s, client, parent, request := failedSandboxDelivery(t, stage, original)
			ctx, cancel := context.WithCancel(parent)
			defer cancel()
			client.cancel = cancel
			got, err := s.CreateSandbox(ctx, request)
			require.Nil(t, got)
			require.Equal(t, original, err)
			require.Len(t, client.lookups, 1)
		})
	}
}

func TestSandboxEmptyDeliveryResponseWithoutWinnerRemainsFailure(t *testing.T) {
	for _, tc := range []struct{ stage, reason string }{{"nil_resolve", "SANDBOX_INVALID_RESPONSE"}, {"nil_ack", "SANDBOX_INVALID_ACK"}} {
		t.Run(tc.stage, func(t *testing.T) {
			s, client, ctx, request := failedSandboxDelivery(t, tc.stage, nil)
			client.winner = nil
			got, err := s.CreateSandbox(ctx, request)
			require.Nil(t, got)
			require.Equal(t, 503, kratoserrors.Code(err))
			require.Equal(t, tc.reason, kratoserrors.Reason(err))
			require.Len(t, client.lookups, 2)
		})
	}
}

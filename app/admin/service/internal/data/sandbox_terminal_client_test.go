package data

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	terminal "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/terminal/v1"
	sandbox "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type terminalResolverRecorder struct {
	terminal.SandboxTerminalServiceClient
	request  *terminal.ResolveTerminalSessionRequest
	metadata metadata.MD
}

func (r *terminalResolverRecorder) ResolveTerminalSession(ctx context.Context, req *terminal.ResolveTerminalSessionRequest, _ ...grpc.CallOption) (*terminal.SandboxTerminalBinding, error) {
	r.request = req
	r.metadata, _ = metadata.FromOutgoingContext(ctx)
	return &terminal.SandboxTerminalBinding{TenantId: req.TenantId, SandboxId: req.SandboxId}, nil
}

type sandboxAdapterStub struct{ sandbox.SandboxServiceClient }

func TestSandboxTerminalClientRebuildsTrustedMetadata(t *testing.T) {
	const tenantID = "11111111-1111-4111-8111-111111111111"
	const sandboxID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	const actor = "governance:user:7"
	recorder := &terminalResolverRecorder{}
	client := &InstanceClient{sandbox: sandboxAdapterStub{}, sandboxTerminal: recorder}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "external-secret", "x-ani-tenant-id", "attacker", "x-ani-actor", "attacker"))
	binding, err := client.ResolveSandboxTerminal(ctx, tenantID, actor, sandboxID)
	require.NoError(t, err)
	require.Equal(t, tenantID, binding.TenantId)
	require.Equal(t, sandboxID, binding.SandboxId)
	require.Equal(t, tenantID, recorder.request.TenantId)
	require.Equal(t, sandboxID, recorder.request.SandboxId)
	require.Equal(t, []string{tenantID}, recorder.metadata.Get("x-ani-tenant-id"))
	require.Equal(t, []string{actor}, recorder.metadata.Get("x-ani-actor"))
	require.Empty(t, recorder.metadata.Get("authorization"))
	requestIDs := recorder.metadata.Get("x-ani-request-id")
	require.Len(t, requestIDs, 1)
	_, err = uuid.Parse(requestIDs[0])
	require.NoError(t, err)
}

func TestSandboxTerminalClientRejectsInvalidCaller(t *testing.T) {
	const tenantID = "11111111-1111-4111-8111-111111111111"
	recorder := &terminalResolverRecorder{}
	client := &InstanceClient{sandbox: sandboxAdapterStub{}, sandboxTerminal: recorder}
	for _, tc := range []struct{ tenant, actor string }{
		{"not-a-tenant", "governance:user:7"},
		{tenantID, "governance:user:0"},
		{tenantID, "governance:sandbox-token:7"},
		{tenantID, "governance:user:07"},
	} {
		_, err := client.ResolveSandboxTerminal(context.Background(), tc.tenant, tc.actor, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
		require.Equal(t, codes.Unauthenticated, status.Code(err))
		require.Nil(t, recorder.request)
	}
}

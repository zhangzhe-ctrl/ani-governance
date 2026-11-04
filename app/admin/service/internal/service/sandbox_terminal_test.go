package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	terminal "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/terminal/v1"
	session "github.com/zhangzhe-ctrl/ani-session-gateway/api/gen/session/v1"
	view "go-wind-admin/api/gen/go/sandbox/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/pkg/middleware/auth"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const terminalSandboxID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

type sandboxTerminalOwner struct {
	*sandboxTestClient
	binding           *terminal.SandboxTerminalBinding
	tenant, actor, id string
}

func (o *sandboxTerminalOwner) ResolveSandboxTerminal(_ context.Context, tenant, actor, id string) (*terminal.SandboxTerminalBinding, error) {
	o.tenant, o.actor, o.id = tenant, actor, id
	return o.binding, nil
}

type sandboxTerminalSessions struct {
	request  *session.CreateSessionRequest
	response *session.CreateSessionResponse
}

func (s *sandboxTerminalSessions) CreateSession(_ context.Context, r *session.CreateSessionRequest) (*session.CreateSessionResponse, error) {
	s.request = r
	if s.response != nil {
		return s.response, nil
	}
	return &session.CreateSessionResponse{SessionId: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", ConnectUrl: "wss://example.test/api/v1/realtime/sessions/cccccccc-cccc-4ccc-8ccc-cccccccccccc?ticket=test", ExpiresAt: timestamppb.New(time.Unix(2000000000, 0))}, nil
}

func TestSandboxTerminalRejectsInvalidTicketURLAndExpiry(t *testing.T) {
	id := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	validURL := "wss://example.test/api/v1/realtime/sessions/" + id + "?ticket=test"
	for name, response := range map[string]*session.CreateSessionResponse{
		"https":          {SessionId: id, ConnectUrl: "https://example.test/api/v1/realtime/sessions/" + id + "?ticket=test", ExpiresAt: timestamppb.New(time.Unix(2000000000, 0))},
		"missing ticket": {SessionId: id, ConnectUrl: "ws://example.test/api/v1/realtime/sessions/" + id, ExpiresAt: timestamppb.New(time.Unix(2000000000, 0))},
		"wrong session":  {SessionId: id, ConnectUrl: "ws://example.test/sessions/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa?ticket=test", ExpiresAt: timestamppb.New(time.Unix(2000000000, 0))},
		"expired":        {SessionId: id, ConnectUrl: validURL, ExpiresAt: timestamppb.New(time.Unix(1, 0))},
	} {
		t.Run(name, func(t *testing.T) {
			svc, _, sessions, ctx, request := sandboxTerminalFixture()
			sessions.response = response
			_, err := svc.ExecSandbox(ctx, request)
			require.Error(t, err)
		})
	}
}

func sandboxTerminalFixture() (*SandboxService, *sandboxTerminalOwner, *sandboxTerminalSessions, context.Context, *view.ExecSandboxRequest) {
	owner := &sandboxTerminalOwner{sandboxTestClient: new(sandboxTestClient), binding: &terminal.SandboxTerminalBinding{
		TenantId: sandboxTestTenant, SandboxId: terminalSandboxID, Version: 2,
		ClusterId: "33333333-3333-4333-8333-333333333333", Namespace: "tenant-" + sandboxTestTenant,
		NamespaceUid: "namespace-uid", PodName: "sandbox-" + terminalSandboxID, PodUid: "pod-uid", ContainerName: "sandbox", Shell: "/bin/sh",
	}}
	sessions := new(sandboxTerminalSessions)
	svc := NewSandboxService(owner, new(sandboxTestAuth), sandboxTenantResolver{}, nil)
	svc.SetTerminalSessionClient(sessions)
	ctx := auth.NewPrincipalContext(context.Background(), &auth.Principal{Type: auth.SubjectUser, ID: 7, TenantID: 42})
	request := &view.ExecSandboxRequest{Data: &view.ExecSandboxData{SandboxId: terminalSandboxID, IdempotencyKey: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", ExpectedVersion: 2, Rows: 24, Cols: 80}}
	return svc, owner, sessions, ctx, request
}

func TestSandboxTerminalUsesIndependentIdentity(t *testing.T) {
	svc, owner, sessions, ctx, request := sandboxTerminalFixture()
	result, err := svc.ExecSandbox(ctx, request)
	require.NoError(t, err)
	require.Equal(t, "cccccccc-cccc-4ccc-8ccc-cccccccccccc", result.SessionId)
	require.Contains(t, result.WsUrl, "ticket=test")
	require.Equal(t, sandboxTestTenant, owner.tenant)
	require.Equal(t, "governance:user:7", owner.actor)
	require.Equal(t, terminalSandboxID, owner.id)
	require.Empty(t, sessions.request.Target.InstanceId)
	require.Equal(t, session.WorkloadKind_WORKLOAD_KIND_SANDBOX, sessions.request.Target.WorkloadKind)
	require.Equal(t, terminalSandboxID, sessions.request.Target.SandboxBinding.SandboxId)
	require.NotEqual(t, request.Data.IdempotencyKey, sessions.request.IdempotencyKey)
	key := sessions.request.IdempotencyKey
	_, err = svc.ExecSandbox(ctx, request)
	require.NoError(t, err)
	require.Equal(t, key, sessions.request.IdempotencyKey)
	require.Equal(t, "sandbox", sessions.request.GetExec().Container)
	require.True(t, sessions.request.GetExec().Tty)
	require.Contains(t, svc.authorization.(*sandboxTestAuth).calls, "POST "+data.SandboxExecPath)
}

func TestSandboxTerminalRejectsStaleOrForeignBindings(t *testing.T) {
	for name, mutate := range map[string]func(*terminal.SandboxTerminalBinding){
		"tenant":        func(b *terminal.SandboxTerminalBinding) { b.TenantId = "11111111-1111-4111-8111-222222222222" },
		"sandbox":       func(b *terminal.SandboxTerminalBinding) { b.SandboxId = "aaaaaaaa-aaaa-4aaa-8aaa-bbbbbbbbbbbb" },
		"version":       func(b *terminal.SandboxTerminalBinding) { b.Version++ },
		"namespace":     func(b *terminal.SandboxTerminalBinding) { b.Namespace = "other" },
		"namespace_uid": func(b *terminal.SandboxTerminalBinding) { b.NamespaceUid = "" },
		"pod":           func(b *terminal.SandboxTerminalBinding) { b.PodName = "other" },
		"pod_uid":       func(b *terminal.SandboxTerminalBinding) { b.PodUid = "" },
		"container":     func(b *terminal.SandboxTerminalBinding) { b.ContainerName = "workspace" },
		"shell":         func(b *terminal.SandboxTerminalBinding) { b.Shell = "/bin/bash" },
	} {
		t.Run(name, func(t *testing.T) {
			svc, owner, sessions, ctx, request := sandboxTerminalFixture()
			mutate(owner.binding)
			_, err := svc.ExecSandbox(ctx, request)
			require.Error(t, err)
			require.Nil(t, sessions.request)
		})
	}
}

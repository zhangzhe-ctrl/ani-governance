package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/middleware"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/stretchr/testify/require"
	terminal "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/terminal/v1"
	session "github.com/zhangzhe-ctrl/ani-session-gateway/api/gen/session/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/service"
	"go-wind-admin/pkg/middleware/auth"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type sandboxTerminalHTTPOwner struct{ *sandboxHTTPResource }

func (o *sandboxTerminalHTTPOwner) ResolveSandboxTerminal(_ context.Context, tenant, actor, id string) (*terminal.SandboxTerminalBinding, error) {
	o.calls++
	return &terminal.SandboxTerminalBinding{TenantId: tenant, SandboxId: id, Version: 1, ClusterId: "44444444-4444-4444-8444-444444444444", Namespace: "tenant-" + tenant, NamespaceUid: "namespace-uid", PodName: "sandbox-" + id, PodUid: "pod-uid", ContainerName: "sandbox", Shell: "/bin/sh"}, nil
}

type sandboxTerminalHTTPSessions struct{ request *session.CreateSessionRequest }

func (s *sandboxTerminalHTTPSessions) CreateSession(_ context.Context, r *session.CreateSessionRequest) (*session.CreateSessionResponse, error) {
	s.request = r
	return &session.CreateSessionResponse{SessionId: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", ConnectUrl: "wss://example.test/api/v1/realtime/sessions/cccccccc-cccc-4ccc-8ccc-cccccccccccc?ticket=test", ExpiresAt: timestamppb.New(time.Unix(2000000000, 0))}, nil
}

type sandboxTerminalHTTPAuth struct{ denied bool }

func (a *sandboxTerminalHTTPAuth) Authorize(_ context.Context, _, _ uint32, path, method string) error {
	if a.denied || path != data.SandboxExecPath || method != "POST" {
		return data.ErrActionAuthorizationDenied
	}
	return nil
}

func TestSandboxTerminalHTTPPathAndAuthorization(t *testing.T) {
	owner := &sandboxTerminalHTTPOwner{sandboxHTTPResource: new(sandboxHTTPResource)}
	authorizer := new(sandboxTerminalHTTPAuth)
	sessions := new(sandboxTerminalHTTPSessions)
	svc := service.NewSandboxService(owner, authorizer, sandboxHTTPTenants{}, sandboxHTTPAdmission{})
	svc.SetTerminalSessionClient(sessions)
	server := khttp.NewServer(khttp.Middleware(func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, r any) (any, error) {
			return next(auth.NewPrincipalContext(ctx, &auth.Principal{Type: auth.SubjectUser, ID: 9, TenantID: 7}), r)
		}
	}))
	RegisterSandboxHTTPServer(server, svc)
	path := "/api/v1/sandbox/" + sandboxHTTPResourceID + "/exec"
	call := func(body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		require.Equal(t, want, w.Code, w.Body.String())
		return w
	}
	body := `{"data":{"idempotency_key":"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb","expected_version":"1","rows":24,"cols":80}}`
	w := call(body, 200)
	require.Contains(t, w.Body.String(), `"session_id":"cccccccc-cccc-4ccc-8ccc-cccccccccccc"`)
	require.Equal(t, sandboxHTTPResourceID, sessions.request.Target.SandboxBinding.SandboxId)
	require.Empty(t, sessions.request.Target.InstanceId)
	sessions.request = nil
	call(`{"data":{"sandbox_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","idempotency_key":"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb","expected_version":"1","rows":24,"cols":80}}`, 400)
	require.Nil(t, sessions.request)
	authorizer.denied = true
	call(body, 403)
	require.Nil(t, sessions.request)
}

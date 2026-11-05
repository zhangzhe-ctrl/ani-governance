package logging

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/stretchr/testify/require"
	admin "go-wind-admin/api/gen/go/admin/service/v1"
	auditpb "go-wind-admin/api/gen/go/audit/service/v1"
	sandboxv1 "go-wind-admin/api/gen/go/sandbox/service/v1"
	"google.golang.org/protobuf/proto"
)

func TestInstanceConfigurationAuditRedactsEnvironmentValues(t *testing.T) {
	env := newAuditServer(t)
	env.fire(http.MethodPost, "/case/5?value=private-env", map[string]string{
		"X-Test-Operation": admin.OperationInstanceServiceCreateInstance,
		"Content-Type":     "application/json",
		"Referer":          "https://example.test/?value=private-env",
	}, `{"data":{"env":[{"name":"TOKEN","value":"private-env"}]}}`, "127.0.0.1:1234")
	require.Len(t, env.capture.api, 1)
	record := env.capture.api[0]
	require.Equal(t, "[redacted]", record.GetRequestBody())
	require.Empty(t, record.GetReferer())
	require.Equal(t, "/case/5", record.GetRequestUri())
	require.Empty(t, record.GetResponse())
}

func TestComputePresetAuditKeepsQueryAndBodyRedacted(t *testing.T) {
	env := newAuditServer(t)
	env.fire(http.MethodPost, "/case/5?name=private-preset", map[string]string{
		"X-Test-Operation": admin.OperationComputePresetServiceAdminCreatePreset,
		"Content-Type":     "application/json",
		"Referer":          "https://example.test/?name=private-preset",
	}, `{"data":{"name":"private-preset"}}`, "127.0.0.1:1234")
	require.Len(t, env.capture.api, 1)
	record := env.capture.api[0]
	require.Equal(t, "[redacted]", record.GetRequestBody())
	require.Empty(t, record.GetReferer())
	require.Equal(t, "/case/5", record.GetRequestUri())
}

func TestVMConsoleAuditDoesNotRetainTicketOrBody(t *testing.T) {
	env := newAuditServer(t)
	env.fire(http.MethodPost, "/case/5?ticket=private-vnc-ticket", map[string]string{"X-Test-Operation": "/admin.service.v1.VMService/CreateVMConsoleSession", "Content-Type": "application/json", "Referer": "https://example.test/?ticket=private-vnc-ticket"}, `{"data":{"idempotencyKey":"private-session-key"}}`, "127.0.0.1:1234")
	require.Len(t, env.capture.api, 1)
	record := env.capture.api[0]
	require.Equal(t, "[redacted]", record.GetRequestBody())
	require.Empty(t, record.GetReferer())
	require.Equal(t, "/case/5", record.GetRequestUri())
	require.Empty(t, record.GetResponse())
}

func TestSandboxTerminalAuditDoesNotRetainTicketOrBody(t *testing.T) {
	env := newAuditServer(t)
	env.fire(http.MethodPost, "/case/5?ticket=private-ticket", map[string]string{"X-Test-Operation": admin.OperationSandboxServiceExecSandbox, "Content-Type": "application/json", "Referer": "https://example.test/?ticket=private-ticket"}, `{"data":{"idempotency_key":"private"}}`, "127.0.0.1:1234")
	require.Len(t, env.capture.api, 1)
	record := env.capture.api[0]
	require.Equal(t, "[redacted]", record.GetRequestBody())
	require.Empty(t, record.GetReferer())
	require.Equal(t, "/case/5", record.GetRequestUri())
	require.Empty(t, record.GetResponse())
}

func TestSandboxTerminalAuditAndResponseRedaction(t *testing.T) {
	const fakeURL = "wss://example.test/sessions/session?ticket=private-ticket"
	const path = "/api/v1/sandbox/00000000-0000-4000-8000-000000000001/exec"
	var record *auditpb.ApiAuditLog
	var gotSnapshot bool
	srv := khttp.NewServer(khttp.Middleware(Server(WithWriteApiLogFunc(func(_ context.Context, in *auditpb.ApiAuditLog) error {
		record = in
		return nil
	}))))
	srv.Route("/").POST(path, func(ctx khttp.Context) error {
		khttp.SetOperation(ctx, admin.OperationSandboxServiceExecSandbox)
		out, err := ctx.Middleware(func(c context.Context, _ interface{}) (interface{}, error) {
			gotSnapshot = c.Value(reqBodyKey{}) != nil
			return &sandboxv1.ExecSandboxResponse{SessionId: "session", WsUrl: fakeURL}, nil
		})(ctx, &sandboxv1.ExecSandboxRequest{})
		if err != nil {
			return err
		}
		return ctx.Result(http.StatusOK, out)
	})
	r := httptest.NewRequest(http.MethodPost, path+"?ticket=private-query", strings.NewReader(`{"data":{"idempotency_key":"private-key"}}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Referer", "https://example.test/?ticket=private-referer")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), fakeURL, "the client must still receive its connection URL")
	require.False(t, gotSnapshot)
	require.NotNil(t, record)
	require.Equal(t, "[redacted]", record.GetRequestBody())
	require.Equal(t, path, record.GetRequestUri())
	require.Empty(t, record.GetReferer())
	require.Empty(t, record.GetResponse())
	original := &sandboxv1.ExecSandboxResponse{SessionId: "session", WsUrl: fakeURL}
	redacted := proto.Clone(original).(*sandboxv1.ExecSandboxResponse)
	redacted.Redact()
	require.Empty(t, redacted.WsUrl)
	require.Equal(t, fakeURL, original.WsUrl)
}

func TestSandboxCodeRunAuditDoesNotRetainCode(t *testing.T) {
	env := newAuditServer(t)
	env.fire(http.MethodPost, "/case/5?code=private-code", map[string]string{"X-Test-Operation": admin.OperationSandboxServiceRunSandboxCode, "Content-Type": "application/json", "Referer": "https://example.test/?code=private-code"}, `{"data":{"code":"private-code","stdin":"private-input"}}`, "127.0.0.1:1234")
	require.Len(t, env.capture.api, 1)
	record := env.capture.api[0]
	require.Equal(t, "[redacted]", record.GetRequestBody())
	require.Empty(t, record.GetReferer())
	require.Equal(t, "/case/5", record.GetRequestUri())
}

func TestSandboxFileAuditDoesNotRetainContentsOrPath(t *testing.T) {
	env := newAuditServer(t)
	env.fire(http.MethodPost, "/case/5?path=private-file", map[string]string{"X-Test-Operation": admin.OperationSandboxServiceWriteSandboxFile, "Content-Type": "application/json", "Referer": "https://example.test/?path=private-file"}, `{"data":{"content_base64":"c2VjcmV0"}}`, "127.0.0.1:1234")
	require.Len(t, env.capture.api, 1)
	record := env.capture.api[0]
	require.Equal(t, "[redacted]", record.GetRequestBody())
	require.Empty(t, record.GetReferer())
	require.Equal(t, "/case/5", record.GetRequestUri())
}

func TestSandboxTokenAuditAndRedactorNeverKeepToken(t *testing.T) {
	env := newAuditServer(t)
	env.fire(http.MethodPost, "/case/5?private=token-key", map[string]string{"X-Test-Operation": admin.OperationSandboxServiceCreateSandboxToken, "Content-Type": "application/json", "Referer": "https://example.test/?private=token-key"}, `{"data":{"idempotency_key":"private"}}`, "127.0.0.1:1234")
	require.Len(t, env.capture.api, 1)
	record := env.capture.api[0]
	require.Equal(t, "[redacted]", record.GetRequestBody())
	require.Empty(t, record.GetReferer())
	require.Empty(t, record.GetResponse())
	response := &sandboxv1.SandboxAccessToken{Token: "ani.sbx.secret"}
	response.Redact()
	require.Empty(t, response.Token)
}

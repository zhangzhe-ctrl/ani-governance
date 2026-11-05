package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/middleware"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/stretchr/testify/require"
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/service"
	"go-wind-admin/pkg/middleware/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const pendingHTTPOperationID = "55555555-5555-4555-8555-555555555555"

type pendingHTTPOwner struct {
	*sandboxHTTPResource
	err error
}

func (o *pendingHTTPOwner) GetSandboxOperation(_ context.Context, _, _ string, _ *res.GetOperationRequest) (*res.GetOperationResponse, error) {
	return nil, o.err
}

type pendingHTTPAdmission struct {
	sandboxHTTPAdmission
	calls int
}

func (a *pendingHTTPAdmission) FindPendingSandboxOperation(_ context.Context, tenant uint32, resourceTenant, operation string) (*data.SandboxPendingOperation, error) {
	if tenant != 7 || resourceTenant != "33333333-3333-4333-8333-333333333333" || operation != pendingHTTPOperationID {
		panic("untrusted journal scope")
	}
	a.calls++
	return &data.SandboxPendingOperation{TenantID: tenant, ResourceTenantID: resourceTenant, SandboxID: sandboxHTTPResourceID, OperationID: operation, CommandKind: "create", CreatedAt: time.Date(2026, 11, 4, 2, 3, 4, 0, time.UTC)}, nil
}
func TestPendingSandboxOperationHTTP(t *testing.T) {
	owner := &pendingHTTPOwner{sandboxHTTPResource: new(sandboxHTTPResource), err: status.Error(codes.NotFound, "SANDBOX_NOT_FOUND")}
	authorization := new(sandboxHTTPAuthorization)
	admission := new(pendingHTTPAdmission)
	server := khttp.NewServer(khttp.Middleware(func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, r any) (any, error) {
			return next(auth.NewPrincipalContext(ctx, &auth.Principal{Type: auth.SubjectUser, ID: 9, TenantID: 7}), r)
		}
	}))
	RegisterSandboxHTTPServer(server, service.NewSandboxService(owner, authorization, sandboxHTTPTenants{}, admission))
	get := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		server.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/sandbox/operations/"+pendingHTTPOperationID, nil))
		return w
	}
	w := get()
	require.Equal(t, 200, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "unknown", body["state"])
	require.Equal(t, "SANDBOX_OWNER_OUTCOME_UNKNOWN", body["reason"])
	require.Equal(t, sandboxHTTPResourceID, body["sandbox_id"])
	require.NotContains(t, w.Body.String(), "command")
	require.NotContains(t, w.Body.String(), "actor")
	require.Equal(t, 1, admission.calls)
	owner.err = status.Error(codes.Unavailable, "private upstream details")
	w = get()
	require.Equal(t, 503, w.Code)
	require.NotContains(t, w.Body.String(), "private upstream details")
	require.Equal(t, 1, admission.calls)
	authorization.denied = true
	w = get()
	require.Equal(t, 403, w.Code)
	require.Equal(t, 1, admission.calls)
}

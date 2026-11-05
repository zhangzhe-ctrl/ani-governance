package server

import (
	"context"
	"net/http/httptest"
	"testing"

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

type securityEventsOwner struct {
	*sandboxHTTPResource
	query  *res.ListSandboxSecurityEventsRequest
	result *res.ListSandboxSecurityEventsResponse
	err    error
}

func (o *securityEventsOwner) ListSandboxSecurityEvents(_ context.Context, tenant, actor string, r *res.ListSandboxSecurityEventsRequest) (*res.ListSandboxSecurityEventsResponse, error) {
	if tenant != "33333333-3333-4333-8333-333333333333" || actor != "governance:user:9" || r.TenantId != tenant {
		panic("untrusted security events scope")
	}
	o.query = r
	return o.result, o.err
}

type securityEventsAuthorizer struct{ denied bool }

func (a *securityEventsAuthorizer) Authorize(_ context.Context, _, _ uint32, path, method string) error {
	if a.denied || path != data.SandboxSecurityEventsPath || method != "GET" {
		return data.ErrActionAuthorizationDenied
	}
	return nil
}

func TestSandboxSecurityEventsHTTP(t *testing.T) {
	owner := &securityEventsOwner{sandboxHTTPResource: new(sandboxHTTPResource), result: &res.ListSandboxSecurityEventsResponse{Total: 2, Source: "kubernetes_pod_warnings", Items: []*res.SandboxSecurityEvent{{Id: "event-1", OccurredAt: "2026-11-03T10:30:00Z", Severity: "warning", EventType: "kubernetes_warning", Description: "Container operation failed"}}}}
	authorizer := new(securityEventsAuthorizer)
	s := khttp.NewServer(khttp.Middleware(func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, r any) (any, error) {
			return next(auth.NewPrincipalContext(ctx, &auth.Principal{Type: auth.SubjectUser, ID: 9, TenantID: 7}), r)
		}
	}))
	RegisterSandboxHTTPServer(s, service.NewSandboxService(owner, authorizer, sandboxHTTPTenants{}, sandboxHTTPAdmission{}))
	path := "/api/v1/sandbox/" + sandboxHTTPResourceID + "/security-events"
	get := func(suffix string, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", path+suffix, nil))
		require.Equal(t, want, w.Code, w.Body.String())
		return w
	}
	w := get("?page=2&pageSize=1&severity=warning", 503)
	require.EqualValues(t, 2, owner.query.Page)
	require.EqualValues(t, 1, owner.query.PageSize)
	require.Equal(t, "warning", owner.query.Severity)
	require.Contains(t, w.Body.String(), "SANDBOX_SECURITY_EVENT_SOURCE_UNAVAILABLE")
	owner.err = status.Error(codes.Unavailable, "SANDBOX_SECURITY_EVENT_SOURCE_UNAVAILABLE")
	w = get("", 503)
	require.Contains(t, w.Body.String(), "SANDBOX_SECURITY_EVENT_SOURCE_UNAVAILABLE")
	owner.err = status.Error(codes.NotFound, "SANDBOX_NOT_FOUND")
	get("", 404)
	owner.err = nil
	for _, q := range []string{"?page=0", "?pageSize=101", "?page=1&page=2", "?severity=error", "?severity=", "?tenant_id=x"} {
		owner.query = nil
		get(q, 400)
		require.Nil(t, owner.query)
	}
	authorizer.denied = true
	owner.query = nil
	get("", 403)
	require.Nil(t, owner.query)
	authorizer.denied = false
	owner.result = &res.ListSandboxSecurityEventsResponse{Source: "unknown"}
	get("", 503)
	owner.result = &res.ListSandboxSecurityEventsResponse{Source: "kubernetes_pod_warnings", Total: 7}
	get("?severity=critical", 503)
	owner.result = &res.ListSandboxSecurityEventsResponse{Source: "kubernetes_pod_warnings", Total: 1, Items: []*res.SandboxSecurityEvent{{Id: "one", OccurredAt: "2026-11-03T10:30:00Z", Severity: "warning", EventType: "kubernetes_warning", Description: "safe"}}}
	get("?page=2&pageSize=1", 503)
}

package server

import (
	"context"
	"encoding/json"
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

type sandboxEventsOwner struct {
	*sandboxHTTPResource
	query  *res.ListSandboxEventsRequest
	err    error
	result *res.ListSandboxEventsResponse
}

func (o *sandboxEventsOwner) ListSandboxEvents(_ context.Context, tenant, actor string, r *res.ListSandboxEventsRequest) (*res.ListSandboxEventsResponse, error) {
	if tenant != "33333333-3333-4333-8333-333333333333" || actor != "governance:user:9" || r.TenantId != tenant {
		panic("untrusted event scope")
	}
	o.query = r
	if o.err != nil {
		return nil, o.err
	}
	if o.result != nil {
		return o.result, nil
	}
	return &res.ListSandboxEventsResponse{Total: 1, Scope: "retained_events_for_recorded_pods", Items: []*res.SandboxEvent{{Id: "event-1", OccurredAt: "2026-11-03T10:30:00Z", Type: "Normal", Reason: "Started", Message: "Container started"}}}, nil
}

type sandboxEventsAuthorizer struct{ denied bool }

func (a *sandboxEventsAuthorizer) Authorize(_ context.Context, _, _ uint32, path, method string) error {
	if a.denied || path != data.SandboxEventsPath || method != "GET" {
		return data.ErrActionAuthorizationDenied
	}
	return nil
}

func TestSandboxEventsHTTP(t *testing.T) {
	owner := &sandboxEventsOwner{sandboxHTTPResource: new(sandboxHTTPResource)}
	authorizer := new(sandboxEventsAuthorizer)
	s := khttp.NewServer(khttp.Middleware(func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, r any) (any, error) {
			return next(auth.NewPrincipalContext(ctx, &auth.Principal{Type: auth.SubjectUser, ID: 9, TenantID: 7}), r)
		}
	}))
	RegisterSandboxHTTPServer(s, service.NewSandboxService(owner, authorizer, sandboxHTTPTenants{}, sandboxHTTPAdmission{}))
	path := "/api/v1/sandbox/" + sandboxHTTPResourceID + "/events"
	get := func(suffix string, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", path+suffix, nil))
		require.Equal(t, want, w.Code, w.Body.String())
		return w
	}
	w := get("?page=1&pageSize=2", 200)
	require.EqualValues(t, 1, owner.query.Page)
	require.EqualValues(t, 2, owner.query.PageSize)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "1", body["total"])
	require.Equal(t, "retained_events_for_recorded_pods", body["scope"])
	require.Equal(t, false, body["history_complete_before_migration"])
	item := body["items"].([]any)[0].(map[string]any)
	require.Equal(t, "2026-11-03T10:30:00Z", item["occurred_at"])
	require.Equal(t, "Normal", item["type"])
	require.Equal(t, "Started", item["reason"])
	require.Equal(t, "Container started", item["message"])
	for _, q := range []string{"?page=0", "?pageSize=101", "?page=1&page=2", "?tenant_id=x", "?page_size=2"} {
		owner.query = nil
		get(q, 400)
		require.Nil(t, owner.query)
	}
	authorizer.denied = true
	owner.query = nil
	get("", 403)
	require.Nil(t, owner.query)
	authorizer.denied = false
	owner.err = status.Error(codes.FailedPrecondition, "SANDBOX_DATA_PLANE_UNAVAILABLE")
	get("", 409)
	owner.err = status.Error(codes.NotFound, "SANDBOX_NOT_FOUND")
	get("", 404)
	owner.err = nil
	owner.result = &res.ListSandboxEventsResponse{Items: []*res.SandboxEvent{{Id: "event-1", OccurredAt: "invalid", Type: "Normal", Reason: "Started", Message: "Container started"}}, Total: 1, Scope: "retained_events_for_recorded_pods"}
	get("", 503)
	owner.result = &res.ListSandboxEventsResponse{Items: []*res.SandboxEvent{}, Total: 0}
	get("", 503) // An old Resource cannot identify the events' coverage.
	owner.result = &res.ListSandboxEventsResponse{Items: []*res.SandboxEvent{}, Total: 0, Scope: "retained_events_for_recorded_pods", HistoryCompleteBeforeMigration: true}
	get("", 503)
}

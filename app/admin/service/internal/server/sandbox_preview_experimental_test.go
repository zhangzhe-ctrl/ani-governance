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
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/service"
	"go-wind-admin/pkg/middleware/auth"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type previewHTTPResource struct {
	*sandboxHTTPResource
	open      *res.OpenPreviewRequest
	list      *res.ListPreviewsRequest
	close     *res.ClosePreviewRequest
	operation *res.GetPreviewOperationRequest
}

func (o *previewHTTPResource) OpenSandboxPreview(_ context.Context, tenant, actor string, r *res.OpenPreviewRequest) (*res.OpenPreviewResponse, error) {
	if tenant != r.TenantId || actor != "governance:user:9" {
		panic("invalid delegated identity")
	}
	o.open = r
	return &res.OpenPreviewResponse{OperationId: r.OperationId, Preview: &res.Preview{PreviewId: r.PreviewId, SandboxId: r.SandboxId, Port: r.Port, Protocol: r.Protocol, State: "queued", Desired: "open", ExpiresAt: timestamppb.New(time.Now().Add(time.Hour))}}, nil
}
func (o *previewHTTPResource) CloseSandboxPreview(_ context.Context, tenant, actor string, r *res.ClosePreviewRequest) (*res.ClosePreviewResponse, error) {
	if tenant != r.TenantId || actor != "governance:user:9" {
		panic("invalid delegated identity")
	}
	o.close = r
	return &res.ClosePreviewResponse{OperationId: r.OperationId, Preview: &res.Preview{PreviewId: r.PreviewId, SandboxId: r.SandboxId, Port: 8080, Protocol: "http", State: "closing", Desired: "closed", ExpiresAt: timestamppb.New(time.Now().Add(time.Hour))}}, nil
}
func (o *previewHTTPResource) ListSandboxPreviews(_ context.Context, tenant, actor string, r *res.ListPreviewsRequest) (*res.ListPreviewsResponse, error) {
	if tenant != r.TenantId || actor != "governance:user:9" {
		panic("invalid delegated identity")
	}
	o.list = r
	return &res.ListPreviewsResponse{Items: []*res.Preview{}, Total: 0}, nil
}
func (o *previewHTTPResource) GetSandboxPreviewOperation(_ context.Context, tenant, actor string, r *res.GetPreviewOperationRequest) (*res.GetPreviewOperationResponse, error) {
	if tenant != r.TenantId || actor != "governance:user:9" {
		panic("invalid delegated identity")
	}
	o.operation = r
	return &res.GetPreviewOperationResponse{OperationId: r.OperationId, PreviewId: "55555555-5555-4555-8555-555555555555", Action: "close", State: "running", CreatedAt: timestamppb.New(time.Now())}, nil
}

type previewHTTPAuthorizer struct{ deny bool }

func (a *previewHTTPAuthorizer) Authorize(_ context.Context, _, _ uint32, path, method string) error {
	if a.deny || !((path == data.SandboxPreviewOpenPath || path == data.SandboxPreviewClosePath) && method == "POST" || (path == data.SandboxPreviewListPath || path == data.SandboxPreviewOperationPath) && method == "GET") {
		return data.ErrActionAuthorizationDenied
	}
	return nil
}
func TestSandboxPreviewHTTP(t *testing.T) {
	owner := &previewHTTPResource{sandboxHTTPResource: new(sandboxHTTPResource)}
	authz := new(previewHTTPAuthorizer)
	s := khttp.NewServer(khttp.Middleware(func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, r any) (any, error) {
			return next(auth.NewPrincipalContext(ctx, &auth.Principal{Type: auth.SubjectUser, ID: 9, TenantID: 7}), r)
		}
	}))
	RegisterSandboxHTTPServer(s, service.NewSandboxService(owner, authz, sandboxHTTPTenants{}, sandboxHTTPAdmission{}))
	base := "/api/v1/sandbox/" + sandboxHTTPResourceID + "/ports"
	post := func(body string, want int) {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", base, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		s.ServeHTTP(w, req)
		require.Equal(t, want, w.Code, w.Body.String())
	}
	post(`{"data":{"idempotency_key":"key","expected_version":"1","port":8080,"protocol":"http"}}`, 200)
	require.NotNil(t, owner.open)
	require.Equal(t, sandboxHTTPResourceID, owner.open.SandboxId)
	owner.open = nil
	post(`{"data":{"sandbox_id":"44444444-4444-4444-8444-444444444444","idempotency_key":"key","expected_version":"1","port":8080,"protocol":"http"}}`, 400)
	require.Nil(t, owner.open)
	authz.deny = true
	post(`{"data":{"idempotency_key":"key","expected_version":"1","port":8080,"protocol":"http"}}`, 403)
	require.Nil(t, owner.open)
	authz.deny = false
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", base+"?page=2&pageSize=3", nil))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.EqualValues(t, 2, owner.list.Page)
	require.EqualValues(t, 3, owner.list.PageSize)
	owner.list = nil
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", base+"?page=1&page=2", nil))
	require.Equal(t, 400, w.Code)
	require.Nil(t, owner.list)
	previewID := "55555555-5555-4555-8555-555555555555"
	closePath := base + "/" + previewID + ":close"
	w = httptest.NewRecorder()
	req := httptest.NewRequest("POST", closePath, strings.NewReader(`{"data":{"idempotency_key":"close","expected_version":"1"}}`))
	req.Header.Set("Content-Type", "application/json")
	s.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, previewID, owner.close.PreviewId)
	owner.close = nil
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", closePath, strings.NewReader(`{"data":{"preview_id":"44444444-4444-4444-8444-444444444444","idempotency_key":"close","expected_version":"1"}}`))
	req.Header.Set("Content-Type", "application/json")
	s.ServeHTTP(w, req)
	require.Equal(t, 400, w.Code, w.Body.String())
	require.Nil(t, owner.close)
	opID := "66666666-6666-4666-8666-666666666666"
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/sandbox/"+sandboxHTTPResourceID+"/port-operations/"+opID, nil))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, opID, owner.operation.OperationId)
}

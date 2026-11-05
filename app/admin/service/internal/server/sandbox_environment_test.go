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

type sandboxEnvironmentOwner struct {
	*sandboxHTTPResource
	query *res.ListEnvironmentVariablesRequest
	err   error
}

func (o *sandboxEnvironmentOwner) ListSandboxEnvironment(_ context.Context, tenant, actor string, r *res.ListEnvironmentVariablesRequest) (*res.ListEnvironmentVariablesResponse, error) {
	if tenant != "33333333-3333-4333-8333-333333333333" || actor != "governance:user:9" || r.TenantId != tenant {
		panic("untrusted environment scope")
	}
	o.query = r
	if o.err != nil {
		return nil, o.err
	}
	return &res.ListEnvironmentVariablesResponse{Total: 1, Items: []*res.SandboxEnvironmentVariable{{Name: "ANI_SANDBOX_POD_UID", Source: "downward_api", FieldRef: "metadata.uid"}}}, nil
}

type sandboxEnvironmentAuthorizer struct{ denied bool }

func (a *sandboxEnvironmentAuthorizer) Authorize(_ context.Context, _, _ uint32, path, method string) error {
	if a.denied || path != data.SandboxEnvironmentPath || method != "GET" {
		return data.ErrActionAuthorizationDenied
	}
	return nil
}

func TestSandboxEnvironmentHTTP(t *testing.T) {
	owner := &sandboxEnvironmentOwner{sandboxHTTPResource: new(sandboxHTTPResource)}
	authorizer := new(sandboxEnvironmentAuthorizer)
	s := khttp.NewServer(khttp.Middleware(func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, r any) (any, error) {
			return next(auth.NewPrincipalContext(ctx, &auth.Principal{Type: auth.SubjectUser, ID: 9, TenantID: 7}), r)
		}
	}))
	RegisterSandboxHTTPServer(s, service.NewSandboxService(owner, authorizer, sandboxHTTPTenants{}, sandboxHTTPAdmission{}))
	path := "/api/v1/sandbox/" + sandboxHTTPResourceID + "/environment"
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
	item := body["items"].([]any)[0].(map[string]any)
	require.Equal(t, "ANI_SANDBOX_POD_UID", item["name"])
	require.Equal(t, "downward_api", item["source"])
	require.Equal(t, "metadata.uid", item["field_ref"])
	require.NotContains(t, item, "value")
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
}

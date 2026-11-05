package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/go-kratos/kratos/v2/middleware"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/stretchr/testify/require"
	"go-wind-admin/app/admin/service/internal/service"
	"go-wind-admin/pkg/middleware/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSandboxFileListMissingDirectoryIsNotAnEmptyPage(t *testing.T) {
	owner := &sandboxHTTPResource{failure: status.Error(codes.NotFound, "SANDBOX_NOT_FOUND")}
	server := khttp.NewServer(khttp.Middleware(func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, request any) (any, error) {
			return next(auth.NewPrincipalContext(ctx, &auth.Principal{Type: auth.SubjectUser, ID: 9, TenantID: 7}), request)
		}
	}))
	RegisterSandboxHTTPServer(server, service.NewSandboxService(owner, new(sandboxHTTPAuthorization), sandboxHTTPTenants{}, sandboxHTTPAdmission{}))
	request := httptest.NewRequest("GET", "/api/v1/sandbox/"+sandboxHTTPResourceID+"/files?path=missing&page=1&pageSize=20", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	require.Equal(t, 404, response.Code, response.Body.String())
	require.Equal(t, "missing", owner.fileQuery.Path)
	require.EqualValues(t, 1, owner.fileQuery.Page)
	var body map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.NotContains(t, body, "items")
}

package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/middleware"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/stretchr/testify/require"
)

// The deliberately denying middleware proves every public query enters the
// authentication chain before service work. It is not an authenticated-flow test.
func TestModelDevQueryRoutesEnterAuthentication(t *testing.T) {
	testModelDevAuthenticatedRoutes(t, []string{
		"/admin/v1/modeldev/executions/11111111-2222-4333-8444-555555555555",
		"/admin/v1/modeldev/executions/11111111-2222-4333-8444-555555555555/artifacts",
		"/admin/v1/modeldev/artifacts/22222222-3333-4444-8555-666666666666/content",
	})
}

func TestModelDevListAndLogRoutesEnterAuthentication(t *testing.T) {
	testModelDevAuthenticatedRoutes(t, []string{
		"/admin/v1/modeldev/executions",
		"/admin/v1/modeldev/executions/11111111-2222-4333-8444-555555555555/logs",
	})
}

func testModelDevAuthenticatedRoutes(t *testing.T, paths []string) {
	t.Helper()
	server := khttp.NewServer(khttp.Middleware(func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			return nil, errors.Unauthorized("INVALID_LOGIN", "user login required")
		}
	}))
	registerModelDevHTTP(server, nil)
	web := httptest.NewServer(server)
	t.Cleanup(web.Close)
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			response, err := web.Client().Get(web.URL + path)
			require.NoError(t, err)
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, http.StatusUnauthorized, response.StatusCode, "CPU09_QUERY_ROUTE_NOT_IMPLEMENTED")
			require.Contains(t, string(body), "INVALID_LOGIN")
			require.Equal(t, "no-store", response.Header.Get("Cache-Control"))
		})
	}
}

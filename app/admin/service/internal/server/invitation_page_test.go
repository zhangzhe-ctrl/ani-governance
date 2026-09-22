package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-kratos/kratos/v2/transport/http"
	"github.com/stretchr/testify/require"
	conf "github.com/tx7do/kratos-bootstrap/api/gen/go/conf/v1"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	"go-wind-admin/app/admin/service/internal/service"
	"go-wind-admin/pkg/authorizer"
)

func TestInvitationPage(t *testing.T) {
	srv := http.NewServer()
	registerInvitationPage(srv)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest("GET", service.InvitationPath, nil))
	require.Equal(t, 200, rr.Code)
	require.Equal(t, "no-store", rr.Header().Get("Cache-Control"))
	require.Equal(t, "no-referrer", rr.Header().Get("Referrer-Policy"))
	require.Contains(t, rr.Header().Get("Content-Security-Policy"), "script-src 'sha256-")
	require.Contains(t, rr.Body.String(), "history.replaceState")
	require.Contains(t, rr.Body.String(), "设置密码并激活")
}

func TestInvitationPublicSelector(t *testing.T) {
	bctx := bootstrap.NewContextWithParam(context.Background(), nil, &conf.Bootstrap{Authz: &conf.Authorization{Type: "noop"}}, bLogger.NopLogger())
	ms := NewRestMiddleware(bctx, nil, nil, nil, authorizer.NewAuthorizer(bctx, nil), nil, nil, nil, nil, nil, nil)
	// Exercise the actual auth selector constructed for the production server.
	srv := http.NewServer(http.Middleware(ms[len(ms)-1]))
	adminV1.RegisterAuthenticationServiceHTTPServer(srv, &service.AuthenticationService{})
	adminV1.RegisterUserServiceHTTPServer(srv, &service.UserService{})
	for _, test := range []struct {
		path   string
		status int
	}{
		{service.InvitationPath, 400}, // reaches request validation without login
		{"/admin/v1/users", 401},      // creation remains authenticated
	} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("POST", test.path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		srv.ServeHTTP(rr, req)
		require.Equal(t, test.status, rr.Code, rr.Body.String())
	}
}

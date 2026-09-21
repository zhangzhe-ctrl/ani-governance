package logging

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
)

func TestInvitationAuditRedactsSecrets(t *testing.T) {
	env := newAuditServer(t)
	env.fire(http.MethodPost, "/case/5?token=secret-token", map[string]string{
		"X-Test-Operation": adminV1.OperationAuthenticationServiceAcceptInvitation,
		"Content-Type":     "application/json",
		"Referer":          "https://example.com/?token=secret-token",
	}, `{"token":"secret-token","password":"Password123!"}`, "127.0.0.1:1234")
	require.Len(t, env.capture.api, 1)
	record := env.capture.api[0]
	require.Equal(t, "[redacted]", record.GetRequestBody())
	require.Empty(t, record.GetReferer())
	require.Equal(t, "/case/5", record.GetRequestUri())
}

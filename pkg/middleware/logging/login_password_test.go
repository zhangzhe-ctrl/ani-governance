package logging

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
)

func TestPasswordLoginAuditDoesNotPersistPassword(t *testing.T) {
	for _, operation := range []string{
		adminV1.OperationAuthenticationServicePasswordLogin,
		adminV1.OperationAuthenticationServicePlatformPasswordLogin,
	} {
		t.Run(operation, func(t *testing.T) {
			env := newAuditServer(t)
			env.fire(http.MethodPost, "/case/5", map[string]string{
				"X-Test-Operation": operation,
				"Content-Type":     "application/json",
			}, `{"username":"manager","password":"Login-secret@2026"}`, "127.0.0.1:1234")
			require.Len(t, env.capture.login, 1)
			require.NotContains(t, env.capture.login[0].String(), "Login-secret@2026")
			require.Empty(t, env.capture.api)
			require.Empty(t, env.capture.operation)
			require.Empty(t, env.capture.permission)
			require.Empty(t, env.capture.dataAccess)
		})
	}
}

package logging

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestAuditRejectsUnverifiedJWTClaims(t *testing.T) {
	env := newAuditServer(t)
	token := mintTestToken(t)
	parts := strings.Split(token, ".")
	parts[2] = strings.Repeat("A", 43)
	env.fire("GET", "/case/5", map[string]string{"X-Test-Operation": "/demo.v1.GadgetService/Get", "Authorization": "Bearer " + strings.Join(parts, ".")}, "", "127.0.0.1:1234")
	require.Len(t, env.capture.api, 1)
	require.Zero(t, env.capture.api[0].GetUserId())
	require.Empty(t, env.capture.api[0].GetSubjectType())
	require.Zero(t, env.capture.api[0].GetSubjectId())
}
func TestAuditRecordsVerifiedKeyWithoutUser(t *testing.T) {
	env := newAuditServer(t)
	env.fire("GET", "/case/5", map[string]string{"X-Test-Operation": "/admin.service.v1.NetworkService/GetVPC", "X-Test-Key-Identity": "verified", "X-Signature": strings.Repeat("f", 64)}, "", "127.0.0.1:1234")
	require.Len(t, env.capture.api, 1)
	rec := env.capture.api[0]
	require.Equal(t, "api_key", rec.GetSubjectType())
	require.Equal(t, uint32(42), rec.GetSubjectId())
	require.Equal(t, uint32(7), rec.GetTenantId())
	require.Zero(t, rec.GetUserId())
	require.Empty(t, rec.GetRequestHeader())
	require.Empty(t, rec.GetResponse())
	require.Equal(t, "[redacted]", rec.GetRequestBody())
}

package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-kratos/kratos/v2/middleware"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/stretchr/testify/require"
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	"go-wind-admin/app/admin/service/internal/service"
	"go-wind-admin/pkg/middleware/auth"
)

type egressIntentOwner struct {
	*sandboxHTTPResource
	source     *res.GetSandboxEgressResponse
	omitIntent bool
}

func (o *egressIntentOwner) ResolveSandbox(_ context.Context, tenant, _ string, r *res.ResolveCreateSpecRequest) (*res.ResolveCreateSpecResponse, error) {
	if r.TenantId != tenant {
		panic("bad tenant")
	}
	out := &res.ResolveCreateSpecResponse{Name: r.Data.Name, ImageReference: r.Data.ImageReference, PresetId: r.Data.PresetId, Workspace: &res.WorkspaceSpec{Size: "10Gi"}}
	if !o.omitIntent {
		out.EgressIntent = r.Data.EgressIntent
	}
	return out, nil
}
func (o *egressIntentOwner) GetSandboxEgress(_ context.Context, tenant, _ string, r *res.GetSandboxEgressRequest) (*res.GetSandboxEgressResponse, error) {
	if r.TenantId != tenant {
		panic("bad tenant")
	}
	return o.source, nil
}
func TestSandboxEgressIntentCreateAndReadHTTP(t *testing.T) {
	owner := &egressIntentOwner{sandboxHTTPResource: new(sandboxHTTPResource)}
	authorizer := new(sandboxHTTPAuthorization)
	server := khttp.NewServer(khttp.Middleware(func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, r any) (any, error) {
			return next(auth.NewPrincipalContext(ctx, &auth.Principal{Type: auth.SubjectUser, ID: 9, TenantID: 7}), r)
		}
	}))
	RegisterSandboxHTTPServer(server, service.NewSandboxService(owner, authorizer, sandboxHTTPTenants{}, sandboxHTTPAdmission{}))
	body := `{"data":{"name":"demo","image_reference":"registry/image:latest","preset_id":"33333333-3333-4333-8333-333333333333","idempotency_key":"one","egress_intent":{"network_egress_policy":"internet"}}}`
	send := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		q := httptest.NewRequest(method, path, strings.NewReader(body))
		if method == "POST" {
			q.Header.Set("Content-Type", "application/json")
		}
		server.ServeHTTP(w, q)
		return w
	}
	restricted := strings.Replace(body, `"internet"`, `"allowlist"`, 1)
	w := send("POST", "/api/v1/sandbox", restricted)
	require.Equal(t, 501, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "SANDBOX_EGRESS_ENFORCEMENT_UNSUPPORTED")
	require.Nil(t, owner.create)
	w = send("POST", "/api/v1/sandbox", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotNil(t, owner.create)
	require.Equal(t, "internet", owner.create.Spec.EgressIntent.NetworkEgressPolicy)
	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, "not_applied_by_sandbox", result["intent_enforcement"])
	owner.source = &res.GetSandboxEgressResponse{Source: "unavailable", Declaration: "unknown", Enforcement: "unknown", HostAllowlistKnown: true, HostAllowlistStatus: "known", IntentEnforcement: "not_applied_by_sandbox", Intent: &res.SandboxEgressIntent{NetworkEgressPolicy: "allowlist", EgressAllowlist: []string{}}}
	w = send("GET", "/api/v1/sandbox/"+sandboxHTTPResourceID+"/egress", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, "known", result["host_allowlist_status"])
	require.Equal(t, "unknown", result["declaration"])
	owner.omitIntent = true
	owner.created = nil
	w = send("POST", "/api/v1/sandbox", body)
	require.Equal(t, 503, w.Code, w.Body.String())
}

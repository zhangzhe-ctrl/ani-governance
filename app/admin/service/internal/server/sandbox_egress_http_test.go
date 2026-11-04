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

type sandboxEgressOwner struct {
	*sandboxHTTPResource
	query  *res.GetSandboxEgressRequest
	result *res.GetSandboxEgressResponse
	err    error
}

func (o *sandboxEgressOwner) GetSandboxEgress(_ context.Context, tenant, actor string, req *res.GetSandboxEgressRequest) (*res.GetSandboxEgressResponse, error) {
	if tenant != "33333333-3333-4333-8333-333333333333" || actor != "governance:user:9" || req.TenantId != tenant {
		panic("untrusted egress scope")
	}
	o.query = req
	return o.result, o.err
}

type sandboxEgressAuthorizer struct{ permission, sandboxPackage bool }

func (a *sandboxEgressAuthorizer) Authorize(_ context.Context, _, _ uint32, path, method string) error {
	if !a.permission || !a.sandboxPackage || path != data.SandboxEgressPath || method != "GET" {
		return data.ErrActionAuthorizationDenied
	}
	return nil
}
func validEgressHTTPResponse() *res.GetSandboxEgressResponse {
	return &res.GetSandboxEgressResponse{Source: "kubernetes_network_policy", Declaration: "restricted", Enforcement: "unknown", HostAllowlistStatus: "unknown", Isolated: true, Rules: []*res.SandboxEgressRule{{
		Peers: []*res.SandboxEgressPeer{
			{NamespaceSelector: &res.SandboxEgressSelector{MatchLabels: map[string]string{"name": "services"}}, PodSelector: &res.SandboxEgressSelector{MatchExpressions: []*res.SandboxEgressLabelExpression{{Key: "app", Operator: "In", Values: []string{"dns"}}}}},
			{IpBlock: &res.SandboxEgressIPBlock{Cidr: "192.0.2.0/24", Except: []string{"192.0.2.128/25"}}},
		},
		Ports: []*res.SandboxEgressPort{{Protocol: "UDP", PortType: "number", Port: "53"}, {Protocol: "TCP", PortType: "name", Port: "https"}},
	}}}
}
func TestSandboxEgressHTTP(t *testing.T) {
	owner := &sandboxEgressOwner{sandboxHTTPResource: new(sandboxHTTPResource), result: validEgressHTTPResponse()}
	authorizer := &sandboxEgressAuthorizer{permission: true, sandboxPackage: true}
	srv := khttp.NewServer(khttp.Middleware(func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, r any) (any, error) {
			return next(auth.NewPrincipalContext(ctx, &auth.Principal{Type: auth.SubjectUser, ID: 9, TenantID: 7}), r)
		}
	}))
	RegisterSandboxHTTPServer(srv, service.NewSandboxService(owner, authorizer, sandboxHTTPTenants{}, sandboxHTTPAdmission{}))
	path := "/api/v1/sandbox/" + sandboxHTTPResourceID + "/egress"
	get := func(target string, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, httptest.NewRequest("GET", target, nil))
		require.Equal(t, want, w.Code, w.Body.String())
		return w
	}
	w := get(path, 200)
	require.Equal(t, sandboxHTTPResourceID, owner.query.SandboxId)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "kubernetes_network_policy", body["source"])
	require.Equal(t, "restricted", body["declaration"])
	require.Equal(t, "unknown", body["enforcement"])
	require.Equal(t, "unknown", body["host_allowlist_status"])
	require.Equal(t, false, body["host_allowlist_known"])
	require.Equal(t, true, body["isolated"])
	rules := body["rules"].([]any)
	require.Len(t, rules, 1)
	rule := rules[0].(map[string]any)
	peers := rule["peers"].([]any)
	require.Len(t, peers, 2)
	selectorPeer := peers[0].(map[string]any)
	require.Equal(t, "services", selectorPeer["namespace_selector"].(map[string]any)["match_labels"].(map[string]any)["name"])
	expr := selectorPeer["pod_selector"].(map[string]any)["match_expressions"].([]any)[0].(map[string]any)
	require.Equal(t, "app", expr["key"])
	require.Equal(t, "In", expr["operator"])
	require.Equal(t, []any{"dns"}, expr["values"])
	block := peers[1].(map[string]any)["ip_block"].(map[string]any)
	require.Equal(t, "192.0.2.0/24", block["cidr"])
	require.Equal(t, []any{"192.0.2.128/25"}, block["except"])
	ports := rule["ports"].([]any)
	require.Len(t, ports, 2)
	require.Equal(t, "UDP", ports[0].(map[string]any)["protocol"])
	require.Equal(t, "number", ports[0].(map[string]any)["port_type"])
	require.Equal(t, "53", ports[0].(map[string]any)["port"])
	require.Equal(t, "name", ports[1].(map[string]any)["port_type"])
	require.Equal(t, "https", ports[1].(map[string]any)["port"])
	for _, q := range []string{"?tenant_id=x", "?page=1", "?sandbox_id=other"} {
		owner.query = nil
		get(path+q, 400)
		require.Nil(t, owner.query)
	}
	owner.query = nil
	get("/api/v1/sandbox/not-a-uuid/egress", 400)
	require.Nil(t, owner.query)
	for _, mode := range []string{"permission", "sandbox package"} {
		t.Run("missing "+mode, func(t *testing.T) {
			authorizer.permission, authorizer.sandboxPackage = mode != "permission", mode != "sandbox package"
			owner.query = nil
			get(path, 403)
			require.Nil(t, owner.query)
		})
	}
	authorizer.permission, authorizer.sandboxPackage = true, true
	owner.err = status.Error(codes.FailedPrecondition, "SANDBOX_DATA_PLANE_UNAVAILABLE")
	get(path, 409)
	owner.err = status.Error(codes.Unavailable, "private-upstream")
	w = get(path, 503)
	require.NotContains(t, w.Body.String(), "private-upstream")
	owner.err = nil

	for name, bad := range map[string]func(*res.GetSandboxEgressResponse){
		"missing source":      func(r *res.GetSandboxEgressResponse) { r.Source = "" },
		"forged enforcement":  func(r *res.GetSandboxEgressResponse) { r.Enforcement = "enforced" },
		"forged host known":   func(r *res.GetSandboxEgressResponse) { r.HostAllowlistKnown = true },
		"forged host status":  func(r *res.GetSandboxEgressResponse) { r.HostAllowlistStatus = "known" },
		"unknown declaration": func(r *res.GetSandboxEgressResponse) { r.Declaration = "foo" },
		"nil rule":            func(r *res.GetSandboxEgressResponse) { r.Rules = []*res.SandboxEgressRule{nil} },
		"nil peer":            func(r *res.GetSandboxEgressResponse) { r.Rules[0].Peers = []*res.SandboxEgressPeer{nil} },
		"nil port":            func(r *res.GetSandboxEgressResponse) { r.Rules[0].Ports = []*res.SandboxEgressPort{nil} },
		"nil selector expression": func(r *res.GetSandboxEgressResponse) {
			r.Rules[0].Peers[0].PodSelector.MatchExpressions = []*res.SandboxEgressLabelExpression{nil}
		},
		"empty selector expression key": func(r *res.GetSandboxEgressResponse) { r.Rules[0].Peers[0].PodSelector.MatchExpressions[0].Key = "" },
		"invalid selector operator": func(r *res.GetSandboxEgressResponse) {
			r.Rules[0].Peers[0].PodSelector.MatchExpressions[0].Operator = "invalid"
		},
		"empty peer": func(r *res.GetSandboxEgressResponse) { r.Rules[0].Peers[0] = &res.SandboxEgressPeer{} },
		"IP block mixed with selector": func(r *res.GetSandboxEgressResponse) {
			r.Rules[0].Peers[0].IpBlock = &res.SandboxEgressIPBlock{Cidr: "192.0.2.0/24"}
		},
		"invalid CIDR":          func(r *res.GetSandboxEgressResponse) { r.Rules[0].Peers[1].IpBlock.Cidr = "not-a-network" },
		"invalid port protocol": func(r *res.GetSandboxEgressResponse) { r.Rules[0].Ports[0].Protocol = "HTTP" },
		"invalid numeric port":  func(r *res.GetSandboxEgressResponse) { r.Rules[0].Ports[0].Port = "65536" },
		"inconsistent deny all": func(r *res.GetSandboxEgressResponse) { r.Declaration = "deny_all" },
	} {
		t.Run(name, func(t *testing.T) {
			owner.result = validEgressHTTPResponse()
			bad(owner.result)
			get(path, 503)
		})
	}
}

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-kratos/kratos/v2/middleware"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/stretchr/testify/require"
	admin "go-wind-admin/api/gen/go/admin/service/v1"
	view "go-wind-admin/api/gen/go/inference/service/v1"
	"go-wind-admin/app/admin/service/internal/service"
)

func TestInferenceQueriesRegisteredBehindMiddleware(t *testing.T) {
	called := 0
	srv := kratoshttp.NewServer(kratoshttp.Middleware(func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			called++
			return &view.InferenceAcceptance{ResourceId: "probe"}, nil
		}
	}))
	RegisterInferenceHTTPServer(srv, service.NewInferenceService(nil))
	for _, path := range []string{"/api/v1/inference/services", "/api/v1/inference/services/service-1", "/api/v1/inference/operations/operation-1"} {
		response := httptest.NewRecorder()
		srv.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, response.Code, path)
	}
	require.Equal(t, 3, called)
}

func TestInferenceDeletePathResourceIDUsesTopLevelRouteVariable(t *testing.T) {
	vars := url.Values{}
	vars.Set("resource_id", "service-1")
	require.Equal(t, "service-1", inferenceDeletePathResourceID(vars))
	vars = url.Values{}
	vars.Set("resource_id", "service-2")
	require.Equal(t, "service-2", inferenceDeletePathResourceID(vars))
}

func TestInferencePublicHTTPRejectsTrustedFieldsAndAmbiguousJSON(t *testing.T) {
	valid := `{"idempotency_key":"20000000-0000-4000-8000-000000000001","name":"wire","model_version_id":"m","served_model_name":"wire","engine_image":"image","engine_type":"vllm","command":"server","cpu":"1","memory":"1Gi","replicas":1,"gpu":{"cluster_id":"c","pool_id":"p","profile_id":"v","profile_version":"1"}}`
	request := httptest.NewRequest("POST", "/api/v1/inference/services", strings.NewReader(valid))
	request.Header.Set("Content-Type", "application/json")
	require.NoError(t, decodeInferenceHTTP(request, new(view.CreateInferenceRequest)))
	for _, body := range []string{`{"data":{"name":"legacy-envelope"}}`, `{"tenant_id":"forged"}`, `{"gpu_owner_attachment":{}}`, `{"original_charges":[]}`, `{"plan":{}}`, `{"name":"a","name":"b"}`, `{} {}`} {
		request = httptest.NewRequest("POST", "/api/v1/inference/services", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		require.Error(t, decodeInferenceHTTP(request, new(view.CreateInferenceRequest)))
	}
	request = httptest.NewRequest("POST", "/api/v1/inference/services?tenant_id=forged", strings.NewReader(valid))
	request.Header.Set("Content-Type", "application/json")
	require.Error(t, decodeInferenceHTTP(request, new(view.CreateInferenceRequest)))
	request = httptest.NewRequest("POST", "/api/v1/inference/services", strings.NewReader(valid))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Ani-Tenant-Id", "forged")
	require.Error(t, decodeInferenceHTTP(request, new(view.CreateInferenceRequest)))
}

func TestInferenceDeleteHTTPHeaderAndPath(t *testing.T) {
	called := 0
	var got *view.DeleteInferenceRequest
	srv := kratoshttp.NewServer(kratoshttp.Middleware(func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			called++
			got = req.(*view.DeleteInferenceRequest)
			return &view.InferenceAcceptance{ResourceId: "service-1"}, nil
		}
	}))
	RegisterInferenceHTTPServer(srv, service.NewInferenceService(nil))
	request := httptest.NewRequest("DELETE", "/api/v1/inference/services/service-1", nil)
	request.Header.Set("Idempotency-Key", "delete-1")
	recorder := httptest.NewRecorder()
	srv.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusAccepted, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, called)
	require.Equal(t, "service-1", got.ResourceId)
	require.Equal(t, "delete-1", got.IdempotencyKey)
	for _, tc := range []struct {
		path, body string
		keys       []string
		forged     bool
	}{
		{"/api/v1/inference/services/service-1", "", nil, false},
		{"/api/v1/inference/services/service-1", "", []string{"a", "b"}, false},
		{"/api/v1/inference/services/service-1?expected_generation=1", "", []string{"a"}, false},
		{"/api/v1/inference/services/service-1?idempotency_key=a", "", []string{"a"}, false},
		{"/api/v1/inference/services/service-1", `{"expected_generation":"1"}`, []string{"a"}, false},
		{"/api/v1/inference/services/service-1", "", []string{"a"}, true},
	} {
		r := httptest.NewRequest("DELETE", tc.path, strings.NewReader(tc.body))
		for _, key := range tc.keys {
			r.Header.Add("Idempotency-Key", key)
		}
		if tc.forged {
			r.Header.Set("X-Ani-Tenant-Id", "forged")
		}
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		require.Equal(t, 1, called)
	}
	web := httptest.NewServer(srv)
	defer web.Close()
	client, err := kratoshttp.NewClient(context.Background(), kratoshttp.WithEndpoint(web.URL))
	require.NoError(t, err)
	defer client.Close()
	_, err = admin.NewInferenceServiceHTTPClient(client).DeleteInference(context.Background(), &view.DeleteInferenceRequest{ResourceId: "service-1", IdempotencyKey: "generated-key"})
	require.NoError(t, err)
	require.Equal(t, "generated-key", got.IdempotencyKey)
}

func TestInferenceHTTPAcceptsCanonicalRawPath(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/v1/inference/services/service-1:update", strings.NewReader(`{"idempotency_key":"20000000-0000-4000-8000-000000000001"}`))
	request.URL.RawPath = request.URL.Path
	request.Header.Set("Content-Type", "application/json")
	require.NoError(t, decodeInferenceHTTP(request, new(view.UpdateInferenceRequest)))
}

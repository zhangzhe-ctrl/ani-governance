package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	view "go-wind-admin/api/gen/go/inference/service/v1"
)

func TestInferencePublicHTTPRejectsTrustedFieldsAndAmbiguousJSON(t *testing.T) {
	valid := `{"data":{"idempotency_key":"20000000-0000-4000-8000-000000000001","name":"wire","model_version_id":"m","replicas":1,"resource":{"gpu":{"cluster_id":"c","pool_id":"p","profile_id":"v","profile_version":"1","replicas":1,"devices_per_replica":1,"container_name":"kserve-container"}},"engine":{"type":"vllm","image":"image","command":["server"]}}}`
	request := httptest.NewRequest("POST", "/api/v1/inference/services", strings.NewReader(valid))
	request.Header.Set("Content-Type", "application/json")
	require.NoError(t, decodeInferenceHTTP(request, new(view.CreateInferenceRequest)))
	for _, body := range []string{`{"data":{"tenant_id":"forged"}}`, `{"data":{"gpu_owner_attachment":{}}}`, `{"data":{"original_charges":[]}}`, `{"data":{"plan":{}}}`, `{"data":{"name":"a","name":"b"}}`, `{"data":{}} {"data":{}}`} {
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

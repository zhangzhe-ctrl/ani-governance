package server

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQuotaInferenceOwnerRegistrationUsesOnlyConfiguredExactDNS(t *testing.T) {
	t.Setenv("ANI_QUOTA_ENABLED", "true")
	t.Setenv("ANI_QUOTA_INTERNAL_ADDR", "127.0.0.1:0")
	t.Setenv("ANI_QUOTA_CA_FILE", "ca")
	t.Setenv("ANI_QUOTA_CERT_FILE", "cert")
	t.Setenv("ANI_QUOTA_KEY_FILE", "key")
	t.Setenv("ANI_QUOTA_INFERENCE_DNS_SAN", "")
	validate := func() error { config := QuotaInternalConfigFromEnv(); return config.Validate() }
	require.Error(t, validate(), "missing production SAN stays fail-closed")
	for _, name := range []string{"*.example.internal", " name", "spiffe://ani.internal/service/ani-inference", "127.0.0.1", "name."} {
		t.Setenv("ANI_QUOTA_INFERENCE_DNS_SAN", name)
		require.Error(t, validate())
	}
	t.Setenv("ANI_QUOTA_INFERENCE_DNS_SAN", "inference.client.test.internal")
	cfg := QuotaInternalConfigFromEnv()
	require.NoError(t, cfg.Validate())
	require.Equal(t, map[string]string{"inference.client.test.internal": "ani-inference"}, cfg.CertOwnerMap)
}

package data

import (
	"testing"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/stretchr/testify/require"
	quotapb "go-wind-admin/api/gen/go/quota/service/v1"
)

// Keep the observed public contract fixed while its constructors move to quota Proto.
func TestQuotaPublicErrorContract(t *testing.T) {
	cases := []struct {
		name, reason string
		code         int32
		newError     func(string) error
	}{
		{"invalid", "INVALID_QUOTA_REQUEST", 400, func(msg string) error { return quotapb.ErrorInvalidQuotaRequest("%s", msg) }},
		{"idempotency", "IDEMPOTENCY_CONFLICT", 409, func(msg string) error { return quotapb.ErrorIdempotencyConflict("%s", msg) }},
		{"exceeded", "QUOTA_EXCEEDED", 409, func(msg string) error { return quotapb.ErrorQuotaExceeded("%s", msg) }},
		{"not configured", "QUOTA_NOT_CONFIGURED", 403, func(msg string) error { return quotapb.ErrorQuotaNotConfigured("%s", msg) }},
		{"adapter", "QUOTA_ADAPTER_UNAVAILABLE", 503, func(msg string) error { return quotapb.ErrorQuotaAdapterUnavailable("%s", msg) }},
		{"admission", "QUOTA_ADMISSION_DENIED", 403, func(msg string) error { return quotapb.ErrorQuotaAdmissionDenied("%s", msg) }},
		{"storage", "QUOTA_STORAGE_UNAVAILABLE", 503, func(msg string) error { return quotapb.ErrorQuotaStorageUnavailable("%s", msg) }},
		{"history", "QUOTA_HISTORY_PRESENT", 409, func(msg string) error { return quotapb.ErrorQuotaHistoryPresent("%s", msg) }},
		{"not found", "QUOTA_NOT_FOUND", 404, func(msg string) error { return quotapb.ErrorQuotaNotFound("%s", msg) }},
		{"internal", "QUOTA_INTERNAL", 500, func(msg string) error { return quotapb.ErrorQuotaInternal("%s", msg) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const msg = "中文 50% remaining: %s"
			got := kratoserrors.FromError(tc.newError(msg))
			require.Equal(t, tc.code, got.Code)
			require.Equal(t, tc.reason, got.Reason)
			require.Equal(t, msg, got.Message)
			require.Empty(t, got.Metadata)
		})
	}
}

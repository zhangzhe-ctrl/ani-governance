package quotaerrors

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestQuotaReleaseGRPCErrorContract(t *testing.T) {
	cases := []struct {
		name, reason string
		code         codes.Code
		newError     func(string) error
	}{
		{"invalid", "INVALID_QUOTA_REQUEST", codes.InvalidArgument, ReleaseInvalid},
		{"unauthenticated", "QUOTA_RELEASE_DENIED", codes.Unauthenticated, ReleaseUnauthenticated},
		{"permission", "QUOTA_RELEASE_DENIED", codes.PermissionDenied, ReleasePermissionDenied},
		{"conflict", "QUOTA_RELEASE_CONFLICT", codes.FailedPrecondition, ReleaseConflict},
		{"not found", "QUOTA_RELEASE_NOT_FOUND", codes.NotFound, ReleaseNotFound},
		{"unavailable", "QUOTA_RELEASE_UNAVAILABLE", codes.Unavailable, ReleaseUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const msg = "中文 50% remaining: %s"
			got := status.Convert(tc.newError(msg))
			require.Equal(t, tc.code, got.Code())
			require.Equal(t, tc.reason+": "+msg, got.Message())
			require.Empty(t, got.Details())
		})
	}
}

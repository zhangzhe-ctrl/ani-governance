package server

import (
	"testing"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRestrictedSandboxEgressRejectionIsPublic(t *testing.T) {
	err := sandboxHTTPError(status.Error(codes.Unimplemented, "SANDBOX_EGRESS_ENFORCEMENT_UNSUPPORTED"))
	converted := kratoserrors.FromError(err)
	require.EqualValues(t, 501, converted.Code)
	require.Equal(t, "SANDBOX_EGRESS_ENFORCEMENT_UNSUPPORTED", converted.Reason)

	unconfirmed := kratoserrors.FromError(sandboxHTTPError(status.Error(codes.Unavailable, "SANDBOX_EGRESS_OUTCOME_UNCONFIRMED")))
	require.EqualValues(t, 503, unconfirmed.Code)
	require.Equal(t, "SANDBOX_EGRESS_OUTCOME_UNCONFIRMED", unconfirmed.Reason)
}

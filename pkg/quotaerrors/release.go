// Package quotaerrors holds the explicit internal gRPC status mapping for quota
// release. Public HTTP reasons and constructors are generated from quota Proto.
package quotaerrors

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	quotapb "go-wind-admin/api/gen/go/quota/service/v1"
)

func releaseStatus(code codes.Code, reason, msg string) error {
	return status.Error(code, reason+": "+msg)
}

func ReleaseInvalid(msg string) error {
	return releaseStatus(codes.InvalidArgument, quotapb.QuotaErrorReason_INVALID_QUOTA_REQUEST.String(), msg)
}

func ReleaseUnauthenticated(msg string) error {
	return releaseStatus(codes.Unauthenticated, "QUOTA_RELEASE_DENIED", msg)
}

func ReleasePermissionDenied(msg string) error {
	return releaseStatus(codes.PermissionDenied, "QUOTA_RELEASE_DENIED", msg)
}

func ReleaseConflict(msg string) error {
	return releaseStatus(codes.FailedPrecondition, "QUOTA_RELEASE_CONFLICT", msg)
}

func ReleaseNotFound(msg string) error {
	return releaseStatus(codes.NotFound, "QUOTA_RELEASE_NOT_FOUND", msg)
}

func ReleaseUnavailable(msg string) error {
	return releaseStatus(codes.Unavailable, "QUOTA_RELEASE_UNAVAILABLE", msg)
}

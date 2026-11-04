package service

import (
	"context"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	view "go-wind-admin/api/gen/go/sandbox/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type sandboxSecurityEventsClient interface {
	ListSandboxSecurityEvents(context.Context, string, string, *res.ListSandboxSecurityEventsRequest) (*res.ListSandboxSecurityEventsResponse, error)
}

// ListSandboxSecurityEvents fails closed until a verified Sandbox-scoped
// security event source is available. In mixed deployments an older Resource
// may return Pod Warning events; those are never presented as security events.
func (s *SandboxService) ListSandboxSecurityEvents(ctx context.Context, r *view.ListSandboxSecurityEventsRequest) (*view.ListSandboxSecurityEventsResponse, error) {
	if r == nil || !sandboxUUID(r.SandboxId) || (r.Severity != "" && r.Severity != "info" && r.Severity != "warning" && r.Severity != "critical") {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid sandbox identity or severity")
	}
	page, size, err := resolveComputePage(r.Page, r.PageSize)
	if err != nil {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid page or pageSize")
	}
	tenant, actor, err := s.scope(ctx, data.SandboxSecurityEventsPath, "GET")
	if err != nil {
		return nil, err
	}
	client, ok := s.client.(sandboxSecurityEventsClient)
	if !ok {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "sandbox security events are not configured")
	}
	_, err = client.ListSandboxSecurityEvents(ctx, tenant, actor, &res.ListSandboxSecurityEventsRequest{TenantId: tenant, SandboxId: r.SandboxId, Page: page, PageSize: size, Severity: r.Severity})
	if err != nil {
		if status.Code(err) == codes.Unavailable && status.Convert(err).Message() == "SANDBOX_SECURITY_EVENT_SOURCE_UNAVAILABLE" {
			return nil, kratoserrors.ServiceUnavailable("SANDBOX_SECURITY_EVENT_SOURCE_UNAVAILABLE", "sandbox security event source unavailable")
		}
		return nil, err
	}
	return nil, kratoserrors.ServiceUnavailable("SANDBOX_SECURITY_EVENT_SOURCE_UNAVAILABLE", "sandbox security event source unavailable")
}

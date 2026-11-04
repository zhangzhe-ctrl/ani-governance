package service

import (
	"context"
	"time"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	view "go-wind-admin/api/gen/go/sandbox/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
)

type sandboxEventsClient interface {
	ListSandboxEvents(context.Context, string, string, *res.ListSandboxEventsRequest) (*res.ListSandboxEventsResponse, error)
}

func (s *SandboxService) ListSandboxEvents(ctx context.Context, r *view.ListSandboxEventsRequest) (*view.ListSandboxEventsResponse, error) {
	if r == nil || !sandboxUUID(r.SandboxId) {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid sandbox identity")
	}
	page, size, err := resolveComputePage(r.Page, r.PageSize)
	if err != nil {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid page or pageSize")
	}
	tenant, actor, err := s.scope(ctx, data.SandboxEventsPath, "GET")
	if err != nil {
		return nil, err
	}
	client, ok := s.client.(sandboxEventsClient)
	if !ok {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "sandbox events are not configured")
	}
	out, err := client.ListSandboxEvents(ctx, tenant, actor, &res.ListSandboxEventsRequest{TenantId: tenant, SandboxId: r.SandboxId, Page: page, PageSize: size})
	if err != nil {
		return nil, err
	}
	if out == nil || out.Scope != "retained_events_for_recorded_pods" || out.HistoryCompleteBeforeMigration || out.Total < 0 || int64(len(out.Items)) > out.Total || uint32(len(out.Items)) > size {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_RESPONSE", "invalid sandbox events")
	}
	result := &view.ListSandboxEventsResponse{Items: []*view.SandboxEvent{}, Total: out.Total, Scope: out.Scope, HistoryCompleteBeforeMigration: false}
	for _, item := range out.Items {
		if item == nil || item.Id == "" || item.Type == "" || item.Reason == "" || item.Message == "" {
			return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_RESPONSE", "invalid sandbox event")
		}
		if _, err := time.Parse(time.RFC3339Nano, item.OccurredAt); err != nil {
			return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_RESPONSE", "invalid sandbox event time")
		}
		result.Items = append(result.Items, &view.SandboxEvent{Id: item.Id, OccurredAt: item.OccurredAt, Type: item.Type, Reason: item.Reason, Message: item.Message})
	}
	return result, nil
}

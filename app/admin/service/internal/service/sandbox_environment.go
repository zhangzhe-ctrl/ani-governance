package service

import (
	"context"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	view "go-wind-admin/api/gen/go/sandbox/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
)

type sandboxEnvironmentClient interface {
	ListSandboxEnvironment(context.Context, string, string, *res.ListEnvironmentVariablesRequest) (*res.ListEnvironmentVariablesResponse, error)
}

func (s *SandboxService) ListSandboxEnvironment(ctx context.Context, r *view.ListSandboxEnvironmentRequest) (*view.ListSandboxEnvironmentResponse, error) {
	if r == nil || !sandboxUUID(r.SandboxId) {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid sandbox identity")
	}
	page, size, err := resolveComputePage(r.Page, r.PageSize)
	if err != nil {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid page or pageSize")
	}
	tenant, actor, err := s.scope(ctx, data.SandboxEnvironmentPath, "GET")
	if err != nil {
		return nil, err
	}
	client, ok := s.client.(sandboxEnvironmentClient)
	if !ok {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "sandbox environment is not configured")
	}
	out, err := client.ListSandboxEnvironment(ctx, tenant, actor, &res.ListEnvironmentVariablesRequest{TenantId: tenant, SandboxId: r.SandboxId, Page: page, PageSize: size})
	if err != nil {
		return nil, err
	}
	if out == nil || out.Total < 0 || int64(len(out.Items)) > out.Total || uint32(len(out.Items)) > size {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_RESPONSE", "invalid sandbox environment")
	}
	result := &view.ListSandboxEnvironmentResponse{Items: []*view.SandboxEnvironmentVariable{}, Total: out.Total}
	for _, item := range out.Items {
		if item == nil || item.Name == "" || item.Source == "" {
			return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_RESPONSE", "invalid sandbox environment variable")
		}
		result.Items = append(result.Items, &view.SandboxEnvironmentVariable{Name: item.Name, Source: item.Source, SecretRef: item.SecretRef, FieldRef: item.FieldRef})
	}
	return result, nil
}

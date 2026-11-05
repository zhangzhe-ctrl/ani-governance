package service

import (
	"context"
	"net"
	"net/url"
	"strconv"
	"strings"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	view "go-wind-admin/api/gen/go/sandbox/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
)

type sandboxPreviewClient interface {
	OpenSandboxPreview(context.Context, string, string, *res.OpenPreviewRequest) (*res.OpenPreviewResponse, error)
	CloseSandboxPreview(context.Context, string, string, *res.ClosePreviewRequest) (*res.ClosePreviewResponse, error)
	ListSandboxPreviews(context.Context, string, string, *res.ListPreviewsRequest) (*res.ListPreviewsResponse, error)
	GetSandboxPreviewOperation(context.Context, string, string, *res.GetPreviewOperationRequest) (*res.GetPreviewOperationResponse, error)
}

func mapSandboxPreview(in *res.Preview, sandbox string) (*view.SandboxPreview, error) {
	bad := func() (*view.SandboxPreview, error) {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_RESPONSE", "invalid sandbox preview")
	}
	if in == nil || !sandboxUUID(in.PreviewId) || in.SandboxId != sandbox || in.Port < 1 || in.Port > 65535 || (in.Protocol != "http" && in.Protocol != "tcp") || in.ExpiresAt == nil || in.ExpiresAt.CheckValid() != nil {
		return bad()
	}
	switch in.State {
	case "queued", "opening", "ready", "closing", "closed", "unknown":
	default:
		return bad()
	}
	if in.Desired != "open" && in.Desired != "closed" {
		return bad()
	}
	if in.State == "ready" && (in.Address == "" || in.Desired != "open") {
		return bad()
	}
	if in.Address != "" {
		u, err := url.Parse(in.Address)
		if err != nil || u.Scheme != in.Protocol || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || in.State != "ready" || in.Desired != "open" {
			return bad()
		}
		host, port, err := net.SplitHostPort(u.Host)
		n, e := strconv.Atoi(port)
		if err != nil || e != nil || net.ParseIP(host) == nil || n < 1 || n > 65535 {
			return bad()
		}
	}
	return &view.SandboxPreview{PreviewId: in.PreviewId, SandboxId: in.SandboxId, Port: in.Port, Protocol: in.Protocol, State: in.State, Reason: in.Reason, Address: in.Address, Desired: in.Desired, ExpiresAt: in.ExpiresAt, Replayed: in.Replayed}, nil
}
func (s *SandboxService) OpenSandboxPreview(ctx context.Context, r *view.OpenSandboxPreviewRequest) (*view.SandboxPreviewAcceptance, error) {
	if r == nil || r.Data == nil || !sandboxUUID(r.Data.SandboxId) || r.Data.ExpectedVersion < 1 || r.Data.Port < 1 || r.Data.Port > 65535 || (r.Data.Protocol != "http" && r.Data.Protocol != "tcp") || (r.Data.TtlSeconds != 0 && (r.Data.TtlSeconds < 60 || r.Data.TtlSeconds > 3600)) {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid preview")
	}
	if err := sandboxKey(r.Data.IdempotencyKey); err != nil {
		return nil, err
	}
	tenant, actor, err := s.scope(ctx, data.SandboxPreviewOpenPath, "POST")
	if err != nil {
		return nil, err
	}
	c, ok := s.client.(sandboxPreviewClient)
	if !ok {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "sandbox preview unavailable")
	}
	id := stableSandboxID(tenant, r.Data.IdempotencyKey, "preview:"+r.Data.SandboxId)
	op := stableSandboxID(tenant, r.Data.IdempotencyKey, "preview-open:"+r.Data.SandboxId)
	out, err := c.OpenSandboxPreview(ctx, tenant, actor, &res.OpenPreviewRequest{TenantId: tenant, SandboxId: r.Data.SandboxId, PreviewId: id, OperationId: op, IdempotencyKey: r.Data.IdempotencyKey, ExpectedVersion: r.Data.ExpectedVersion, Port: r.Data.Port, Protocol: r.Data.Protocol, TtlSeconds: r.Data.TtlSeconds})
	if err != nil {
		return nil, err
	}
	if out == nil || out.OperationId != op {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_RESPONSE", "invalid preview operation")
	}
	p, err := mapSandboxPreview(out.Preview, r.Data.SandboxId)
	if err != nil {
		return nil, err
	}
	if p.PreviewId != id {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_RESPONSE", "invalid preview identity")
	}
	return &view.SandboxPreviewAcceptance{Preview: p, OperationId: out.OperationId}, nil
}
func (s *SandboxService) CloseSandboxPreview(ctx context.Context, r *view.CloseSandboxPreviewRequest) (*view.SandboxPreviewAcceptance, error) {
	if r == nil || r.Data == nil || !sandboxUUID(r.Data.SandboxId) || !sandboxUUID(r.Data.PreviewId) || r.Data.ExpectedVersion < 1 {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid preview")
	}
	if err := sandboxKey(r.Data.IdempotencyKey); err != nil {
		return nil, err
	}
	tenant, actor, err := s.scope(ctx, data.SandboxPreviewClosePath, "POST")
	if err != nil {
		return nil, err
	}
	c, ok := s.client.(sandboxPreviewClient)
	if !ok {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "sandbox preview unavailable")
	}
	op := stableSandboxID(tenant, r.Data.IdempotencyKey, "preview-close:"+r.Data.SandboxId+":"+r.Data.PreviewId)
	out, err := c.CloseSandboxPreview(ctx, tenant, actor, &res.ClosePreviewRequest{TenantId: tenant, SandboxId: r.Data.SandboxId, PreviewId: r.Data.PreviewId, OperationId: op, IdempotencyKey: r.Data.IdempotencyKey, ExpectedVersion: r.Data.ExpectedVersion})
	if err != nil {
		return nil, err
	}
	if out == nil || out.OperationId != op {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_RESPONSE", "invalid preview operation")
	}
	p, err := mapSandboxPreview(out.Preview, r.Data.SandboxId)
	if err != nil {
		return nil, err
	}
	if p.PreviewId != r.Data.PreviewId || p.Address != "" {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_RESPONSE", "invalid preview closure")
	}
	return &view.SandboxPreviewAcceptance{Preview: p, OperationId: out.OperationId}, nil
}
func (s *SandboxService) ListSandboxPreviews(ctx context.Context, r *view.ListSandboxPreviewsRequest) (*view.ListSandboxPreviewsResponse, error) {
	if r == nil || !sandboxUUID(r.SandboxId) {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid sandbox")
	}
	page, size, err := resolveComputePage(r.Page, r.PageSize)
	if err != nil {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid pagination")
	}
	tenant, actor, err := s.scope(ctx, data.SandboxPreviewListPath, "GET")
	if err != nil {
		return nil, err
	}
	c, ok := s.client.(sandboxPreviewClient)
	if !ok {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "sandbox preview unavailable")
	}
	out, err := c.ListSandboxPreviews(ctx, tenant, actor, &res.ListPreviewsRequest{TenantId: tenant, SandboxId: r.SandboxId, Page: page, PageSize: size})
	if err != nil {
		return nil, err
	}
	if out == nil || out.Total < 0 || int64(len(out.Items)) > out.Total || uint32(len(out.Items)) > size {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_RESPONSE", "invalid preview list")
	}
	result := &view.ListSandboxPreviewsResponse{Items: []*view.SandboxPreview{}, Total: out.Total}
	for _, item := range out.Items {
		p, e := mapSandboxPreview(item, r.SandboxId)
		if e != nil {
			return nil, e
		}
		result.Items = append(result.Items, p)
	}
	return result, nil
}
func (s *SandboxService) GetSandboxPreviewOperation(ctx context.Context, r *view.GetSandboxPreviewOperationRequest) (*view.SandboxPreviewOperation, error) {
	if r == nil || !sandboxUUID(r.SandboxId) || !sandboxUUID(r.OperationId) {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid operation")
	}
	tenant, actor, err := s.scope(ctx, data.SandboxPreviewOperationPath, "GET")
	if err != nil {
		return nil, err
	}
	c, ok := s.client.(sandboxPreviewClient)
	if !ok {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "sandbox preview unavailable")
	}
	o, err := c.GetSandboxPreviewOperation(ctx, tenant, actor, &res.GetPreviewOperationRequest{TenantId: tenant, SandboxId: r.SandboxId, OperationId: r.OperationId})
	if err != nil {
		return nil, err
	}
	if o == nil || o.OperationId != r.OperationId || !sandboxUUID(o.PreviewId) || (o.Action != "open" && o.Action != "close") || !strings.Contains("|queued|running|succeeded|failed|unknown|", "|"+o.State+"|") || o.CreatedAt == nil || o.CreatedAt.CheckValid() != nil || o.CompletedAt != nil && o.CompletedAt.CheckValid() != nil {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_RESPONSE", "invalid preview operation")
	}
	return &view.SandboxPreviewOperation{OperationId: o.OperationId, PreviewId: o.PreviewId, Action: o.Action, State: o.State, Reason: o.Reason, CreatedAt: o.CreatedAt, CompletedAt: o.CompletedAt}, nil
}

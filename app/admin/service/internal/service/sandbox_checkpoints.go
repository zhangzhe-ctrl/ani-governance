package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	view "go-wind-admin/api/gen/go/sandbox/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
)

type sandboxCheckpointsClient interface {
	ListSandboxCheckpoints(context.Context, string, string, *res.ListSandboxCheckpointsRequest) (*res.ListSandboxCheckpointsResponse, error)
}

type sandboxCheckpointCreateClient interface {
	CreateSandboxCheckpoint(context.Context, string, string, *res.CreateSandboxCheckpointRequest) (*res.CreateSandboxCheckpointResponse, error)
}

type sandboxCheckpointRestoreClient interface {
	RestoreSandboxCheckpoint(context.Context, string, string, *res.RestoreSandboxCheckpointRequest) (*res.RestoreSandboxCheckpointResponse, error)
}

func (s *SandboxService) RestoreSandboxCheckpoint(ctx context.Context, r *view.RestoreSandboxCheckpointRequest) (*view.SandboxAcceptance, error) {
	if r == nil || r.Data == nil || !sandboxUUID(r.Data.SandboxId) || !sandboxUUID(r.Data.CheckpointId) || r.Data.ExpectedVersion < 1 || sandboxKey(r.Data.IdempotencyKey) != nil || !utf8.ValidString(r.Data.IdempotencyKey) || strings.ContainsRune(r.Data.IdempotencyKey, 0) {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid checkpoint restore request")
	}
	tenant, actor, err := s.scope(ctx, data.SandboxCheckpointRestorePath, "POST")
	if err != nil {
		return nil, err
	}
	client, ok := s.client.(sandboxCheckpointRestoreClient)
	if !ok {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "checkpoint restore is not configured")
	}
	out, err := client.RestoreSandboxCheckpoint(ctx, tenant, actor, &res.RestoreSandboxCheckpointRequest{TenantId: tenant, SandboxId: r.Data.SandboxId, CheckpointId: r.Data.CheckpointId, IdempotencyKey: r.Data.IdempotencyKey, ExpectedVersion: r.Data.ExpectedVersion})
	if err != nil {
		return nil, err
	}
	if out == nil || out.Ack == nil || !out.Ack.Accepted || out.Ack.ResourceId != r.Data.SandboxId || !sandboxUUID(out.Ack.OperationId) {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_ACK", "invalid restore acceptance")
	}
	return &view.SandboxAcceptance{ResourceId: out.Ack.ResourceId, OperationId: out.Ack.OperationId, Accepted: true, Replayed: out.Ack.Replayed}, nil
}

func (s *SandboxService) CreateSandboxCheckpoint(ctx context.Context, r *view.CreateSandboxCheckpointRequest) (*view.CreateSandboxCheckpointResponse, error) {
	if r == nil || r.Data == nil || !sandboxUUID(r.Data.SandboxId) || r.Data.ExpectedVersion < 1 ||
		r.Data.Name == "" || len([]rune(r.Data.Name)) > 128 || !utf8.ValidString(r.Data.Name) || strings.ContainsRune(r.Data.Name, 0) ||
		sandboxKey(r.Data.IdempotencyKey) != nil || !utf8.ValidString(r.Data.IdempotencyKey) || strings.ContainsRune(r.Data.IdempotencyKey, 0) {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid checkpoint create request")
	}
	tenant, actor, err := s.scope(ctx, data.SandboxCheckpointCreatePath, "POST")
	if err != nil {
		return nil, err
	}
	client, ok := s.client.(sandboxCheckpointCreateClient)
	if !ok {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "checkpoint creation is not configured")
	}
	out, err := client.CreateSandboxCheckpoint(ctx, tenant, actor, &res.CreateSandboxCheckpointRequest{TenantId: tenant, SandboxId: r.Data.SandboxId, Name: r.Data.Name, IdempotencyKey: r.Data.IdempotencyKey, ExpectedVersion: r.Data.ExpectedVersion})
	if err != nil {
		return nil, err
	}
	if out == nil || !sandboxUUID(out.CheckpointId) || !sandboxUUID(out.OperationId) || (out.State != "accepted" && out.State != "running" && out.State != "unknown" && out.State != "failed" && out.State != "ready") {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_RESPONSE", "invalid checkpoint acceptance")
	}
	return &view.CreateSandboxCheckpointResponse{CheckpointId: out.CheckpointId, OperationId: out.OperationId, State: out.State, Replayed: out.Replayed}, nil
}

func (s *SandboxService) ListSandboxCheckpoints(ctx context.Context, r *view.ListSandboxCheckpointsRequest) (*view.ListSandboxCheckpointsResponse, error) {
	if r == nil || !sandboxUUID(r.SandboxId) {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid sandbox identity")
	}
	page, size, err := resolveComputePage(r.Page, r.PageSize)
	if err != nil {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid page or pageSize")
	}
	tenant, actor, err := s.scope(ctx, data.SandboxCheckpointsPath, "GET")
	if err != nil {
		return nil, err
	}
	client, ok := s.client.(sandboxCheckpointsClient)
	if !ok {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "sandbox checkpoints are not configured")
	}
	out, err := client.ListSandboxCheckpoints(ctx, tenant, actor, &res.ListSandboxCheckpointsRequest{TenantId: tenant, SandboxId: r.SandboxId, Page: page, PageSize: size})
	if err != nil {
		return nil, err
	}
	invalid := func() (*view.ListSandboxCheckpointsResponse, error) {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_RESPONSE", "invalid sandbox checkpoints")
	}
	if out == nil || out.Source != "sandbox_checkpoint_ledger" || out.Total < 0 || int64(len(out.Items)) > out.Total || uint32(len(out.Items)) > size {
		return invalid()
	}
	start := uint64(page-1) * uint64(size)
	expected := uint64(0)
	if start < uint64(out.Total) {
		expected = uint64(out.Total) - start
		if expected > uint64(size) {
			expected = uint64(size)
		}
	}
	if uint64(len(out.Items)) != expected {
		return invalid()
	}
	result := &view.ListSandboxCheckpointsResponse{Items: []*view.SandboxCheckpoint{}, Total: out.Total, Source: out.Source}
	for _, item := range out.Items {
		if item == nil || !sandboxUUID(item.Id) || item.Name == "" || item.Scope != "filesystem_crash_consistent" || item.SizeKnown || item.SizeBytes != 0 {
			return invalid()
		}
		if item.Status != "accepted" && item.Status != "running" && item.Status != "unknown" && item.Status != "failed" && item.Status != "ready" {
			return invalid()
		}
		switch item.ProviderStatus {
		case "ready", "not_observed", "missing", "identity_unknown", "identity_mismatch", "deleting", "not_ready":
		default:
			return invalid()
		}
		if item.Status == "ready" && item.ProviderStatus != "ready" {
			return invalid()
		}
		if _, err := time.Parse(time.RFC3339Nano, item.CreatedAt); err != nil {
			return invalid()
		}
		result.Items = append(result.Items, &view.SandboxCheckpoint{Id: item.Id, Name: item.Name, Status: item.Status, Reason: sandboxPublicOperationReason(item.Reason), Scope: item.Scope, SizeBytes: 0, SizeKnown: false, CreatedAt: item.CreatedAt, ProviderStatus: item.ProviderStatus})
	}
	return result, nil
}

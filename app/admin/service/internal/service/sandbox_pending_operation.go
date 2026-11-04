package service

import (
	"context"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	view "go-wind-admin/api/gen/go/sandbox/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// A durable Gov intent is evidence of an attempted delivery, not proof that
// Resource accepted or rejected it. Only Resource NotFound may invoke this
// fallback, after route authorization and trusted tenant mapping.
type sandboxPendingOperationReader interface {
	FindPendingSandboxOperation(context.Context, uint32, string, string) (*data.SandboxPendingOperation, error)
}

func (s *SandboxService) pendingSandboxOperation(ctx context.Context, tenant, operationID string, ownerNotFound error) (*view.SandboxOperation, error) {
	reader, ok := s.admission.(sandboxPendingOperationReader)
	if !ok || !sandboxUUID(operationID) || !sandboxUUID(tenant) {
		return nil, ownerNotFound
	}
	principal, err := gpuPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	item, err := reader.FindPendingSandboxOperation(ctx, principal.TenantID, tenant, operationID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, ownerNotFound
	}
	if item.TenantID != principal.TenantID || item.ResourceTenantID != tenant || item.OperationID != operationID || !sandboxUUID(item.SandboxID) || item.CreatedAt.IsZero() {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_INTENT", "sandbox intent identity mismatch")
	}
	action := item.CommandKind
	if action == "" {
		action = "create"
	}
	if action != "create" && action != "checkpoint_clone" {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_INTENT", "invalid sandbox command kind")
	}
	createdAt := timestamppb.New(item.CreatedAt)
	if err := createdAt.CheckValid(); err != nil {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_INVALID_INTENT", "invalid sandbox intent time")
	}
	return &view.SandboxOperation{Id: operationID, SandboxId: item.SandboxID, Action: action, State: "unknown", Reason: "SANDBOX_OWNER_OUTCOME_UNKNOWN", CreatedAt: createdAt}, nil
}

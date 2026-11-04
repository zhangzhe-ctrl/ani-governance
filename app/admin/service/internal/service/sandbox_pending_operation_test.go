package service

import (
	"context"
	"testing"
	"time"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/stretchr/testify/require"
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	view "go-wind-admin/api/gen/go/sandbox/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/pkg/middleware/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const pendingOperationID = "55555555-5555-4555-8555-555555555555"
const pendingSandboxID = "66666666-6666-4666-8666-666666666666"

type pendingOperationOwner struct {
	*sandboxTestClient
	result       *res.GetOperationResponse
	err          error
	calls        int
	secondResult *res.GetOperationResponse
	secondErr    error
}

func (c *pendingOperationOwner) GetSandboxOperation(_ context.Context, tenant, actor string, req *res.GetOperationRequest) (*res.GetOperationResponse, error) {
	if tenant != sandboxTestTenant || actor != "governance:user:7" || req.TenantId != tenant || req.OperationId != pendingOperationID {
		panic("incorrect trusted owner scope")
	}
	c.calls++
	if c.calls == 2 && (c.secondResult != nil || c.secondErr != nil) {
		return c.secondResult, c.secondErr
	}
	return c.result, c.err
}

type pendingOperationLedger struct {
	*sandboxTestAdmission
	item  *data.SandboxPendingOperation
	err   error
	calls int
}

func (l *pendingOperationLedger) FindPendingSandboxOperation(_ context.Context, tenant uint32, resourceTenant, operation string) (*data.SandboxPendingOperation, error) {
	if tenant != 42 || resourceTenant != sandboxTestTenant || operation != pendingOperationID {
		panic("incorrect trusted journal scope")
	}
	l.calls++
	return l.item, l.err
}

type pendingOperationDenied struct{}

func (pendingOperationDenied) Authorize(context.Context, uint32, uint32, string, string) error {
	return data.ErrActionAuthorizationDenied
}

func TestPendingSandboxOperationFallbackPreservesOwnerAndTenantBoundaries(t *testing.T) {
	at := time.Date(2026, 11, 4, 2, 3, 4, 0, time.UTC)
	valid := data.SandboxPendingOperation{TenantID: 42, ResourceTenantID: sandboxTestTenant, SandboxID: pendingSandboxID, OperationID: pendingOperationID, CommandKind: "create", CreatedAt: at}
	owner := &pendingOperationOwner{sandboxTestClient: new(sandboxTestClient), err: status.Error(codes.NotFound, "SANDBOX_NOT_FOUND")}
	ledger := &pendingOperationLedger{sandboxTestAdmission: new(sandboxTestAdmission), item: &valid}
	svc := NewSandboxService(owner, new(sandboxTestAuth), sandboxTenantResolver{}, ledger)
	ctx := auth.NewPrincipalContext(context.Background(), &auth.Principal{Type: auth.SubjectUser, ID: 7, TenantID: 42})
	req := &view.GetSandboxOperationRequest{OperationId: pendingOperationID}
	got, err := svc.GetSandboxOperation(ctx, req)
	require.NoError(t, err)
	require.Equal(t, pendingOperationID, got.Id)
	require.Equal(t, pendingSandboxID, got.SandboxId)
	require.Equal(t, "create", got.Action)
	require.Equal(t, "unknown", got.State)
	require.Equal(t, "SANDBOX_OWNER_OUTCOME_UNKNOWN", got.Reason)
	require.True(t, got.CreatedAt.AsTime().Equal(at))
	require.Nil(t, got.CompletedAt)
	require.Zero(t, got.IntentVersion)
	clone := valid
	clone.CommandKind = "checkpoint_clone"
	ledger.item = &clone
	got, err = svc.GetSandboxOperation(ctx, req)
	require.NoError(t, err)
	require.Equal(t, "checkpoint_clone", got.Action)
	ledger.item = nil
	got, err = svc.GetSandboxOperation(ctx, req)
	require.Nil(t, got)
	require.Equal(t, codes.NotFound, status.Code(err))
	for _, mutate := range []func(*data.SandboxPendingOperation){
		func(x *data.SandboxPendingOperation) { x.TenantID++ },
		func(x *data.SandboxPendingOperation) { x.ResourceTenantID = "99999999-9999-4999-8999-999999999999" },
		func(x *data.SandboxPendingOperation) { x.OperationID = "99999999-9999-4999-8999-999999999999" },
		func(x *data.SandboxPendingOperation) { x.SandboxID = "invalid" },
		func(x *data.SandboxPendingOperation) { x.CommandKind = "unknown" },
		func(x *data.SandboxPendingOperation) { x.CreatedAt = time.Time{} },
	} {
		item := valid
		mutate(&item)
		ledger.item = &item
		got, err = svc.GetSandboxOperation(ctx, req)
		require.Nil(t, got)
		require.EqualValues(t, 503, kratoserrors.Code(err))
	}
	ledger.item = &valid
	before := ledger.calls
	owner.err = status.Error(codes.Unavailable, "owner unavailable")
	_, err = svc.GetSandboxOperation(ctx, req)
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Equal(t, before, ledger.calls, "unavailable must not be translated into absence")
	owner.err = nil
	owner.result = &res.GetOperationResponse{Operation: &res.Operation{Id: pendingOperationID, SandboxId: pendingSandboxID, State: "succeeded"}}
	got, err = svc.GetSandboxOperation(ctx, req)
	require.NoError(t, err)
	require.Equal(t, "succeeded", got.State)
	require.Equal(t, before, ledger.calls, "owner result takes precedence")
	svc.authorization = pendingOperationDenied{}
	calls := owner.calls
	_, err = svc.GetSandboxOperation(ctx, req)
	require.EqualValues(t, 403, kratoserrors.Code(err))
	require.Equal(t, calls, owner.calls)
	require.Equal(t, before, ledger.calls)
}

func TestPendingSandboxOperationRechecksOwnerAfterConcurrentAcknowledgment(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result *res.GetOperationResponse
		err    error
		want   codes.Code
	}{
		{"accepted", &res.GetOperationResponse{Operation: &res.Operation{Id: pendingOperationID, SandboxId: pendingSandboxID, State: "succeeded"}}, nil, codes.OK},
		{"owner_unavailable", nil, status.Error(codes.Unavailable, "owner unavailable"), codes.Unavailable},
		{"still_not_found", nil, status.Error(codes.NotFound, "SANDBOX_NOT_FOUND"), codes.NotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner := &pendingOperationOwner{sandboxTestClient: new(sandboxTestClient), err: status.Error(codes.NotFound, "SANDBOX_NOT_FOUND"), secondResult: tc.result, secondErr: tc.err}
			ledger := &pendingOperationLedger{sandboxTestAdmission: new(sandboxTestAdmission)}
			svc := NewSandboxService(owner, new(sandboxTestAuth), sandboxTenantResolver{}, ledger)
			ctx := auth.NewPrincipalContext(context.Background(), &auth.Principal{Type: auth.SubjectUser, ID: 7, TenantID: 42})
			got, err := svc.GetSandboxOperation(ctx, &view.GetSandboxOperationRequest{OperationId: pendingOperationID})
			require.Equal(t, tc.want, status.Code(err))
			require.Equal(t, 2, owner.calls)
			require.Equal(t, 1, ledger.calls)
			if tc.want == codes.OK {
				require.Equal(t, "succeeded", got.State)
			} else {
				require.Nil(t, got)
			}
		})
	}
}

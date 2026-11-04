package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/middleware"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/stretchr/testify/require"
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/service"
	"go-wind-admin/pkg/middleware/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type checkpointOwner struct {
	*sandboxHTTPResource
	create  *res.CreateSandboxCheckpointRequest
	list    *res.ListSandboxCheckpointsRequest
	result  *res.CreateSandboxCheckpointResponse
	clone   *res.AcceptSandboxCloneRequest
	restore *res.RestoreSandboxCheckpointRequest
}

func TestCheckpointReferencedByCloneReasonIsPublicAndSanitized(t *testing.T) {
	err := sandboxHTTPError(status.Error(codes.FailedPrecondition, "SANDBOX_CHECKPOINT_REFERENCED_BY_CLONE"))
	converted := kratoserrors.FromError(err)
	require.EqualValues(t, 409, converted.Code)
	require.Equal(t, "SANDBOX_CHECKPOINT_REFERENCED_BY_CLONE", converted.Reason)
	require.Equal(t, "sandbox request failed", converted.Message)
	unsafe := sandboxHTTPError(status.Error(codes.FailedPrecondition, "SANDBOX_CHECKPOINT_REFERENCED_BY_CLONE volume=private-csi-handle"))
	require.NotContains(t, unsafe.Error(), "private-csi-handle")
}

func TestCheckpointWFFCReasonIsPublicAndSanitized(t *testing.T) {
	converted := kratoserrors.FromError(sandboxHTTPError(status.Error(codes.Unimplemented, "SANDBOX_TRANSFER_WFFC_UNSUPPORTED")))
	require.EqualValues(t, 501, converted.Code)
	require.Equal(t, "SANDBOX_TRANSFER_WFFC_UNSUPPORTED", converted.Reason)
	unsafe := sandboxHTTPError(status.Error(codes.Unimplemented, "SANDBOX_TRANSFER_WFFC_UNSUPPORTED secret"))
	require.NotContains(t, unsafe.Error(), "secret")
}

func (o *checkpointOwner) CreateSandboxCheckpoint(_ context.Context, tenant, actor string, in *res.CreateSandboxCheckpointRequest) (*res.CreateSandboxCheckpointResponse, error) {
	if tenant != "33333333-3333-4333-8333-333333333333" || actor != "governance:user:9" || in.TenantId != tenant {
		panic("untrusted checkpoint scope")
	}
	o.create = in
	return o.result, nil
}
func (o *checkpointOwner) ListSandboxCheckpoints(_ context.Context, tenant, actor string, in *res.ListSandboxCheckpointsRequest) (*res.ListSandboxCheckpointsResponse, error) {
	if tenant != "33333333-3333-4333-8333-333333333333" || actor != "governance:user:9" || in.TenantId != tenant {
		panic("untrusted checkpoint scope")
	}
	o.list = in
	return &res.ListSandboxCheckpointsResponse{Source: "sandbox_checkpoint_ledger", Total: 0}, nil
}

func (o *checkpointOwner) AcceptSandboxClone(_ context.Context, tenant, actor string, in *res.AcceptSandboxCloneRequest) (*res.AcceptSandboxCloneResponse, error) {
	if tenant != "33333333-3333-4333-8333-333333333333" || actor != "governance:user:9" || in.Context.TenantId != tenant {
		panic("untrusted clone scope")
	}
	o.clone = in
	return &res.AcceptSandboxCloneResponse{Ack: &res.OperationAck{ResourceId: in.Context.ResourceId, OperationId: in.Context.OperationId, Accepted: true}}, nil
}

func (o *checkpointOwner) RestoreSandboxCheckpoint(_ context.Context, tenant, actor string, in *res.RestoreSandboxCheckpointRequest) (*res.RestoreSandboxCheckpointResponse, error) {
	if tenant != "33333333-3333-4333-8333-333333333333" || actor != "governance:user:9" || in.TenantId != tenant {
		panic("untrusted restore scope")
	}
	o.restore = in
	return &res.RestoreSandboxCheckpointResponse{Ack: &res.OperationAck{ResourceId: in.SandboxId, OperationId: "88888888-8888-4888-8888-888888888888", Accepted: true}}, nil
}

type checkpointAuth struct{ denied bool }

func (a *checkpointAuth) Authorize(_ context.Context, _, _ uint32, path, method string) error {
	if a.denied || !((path == data.SandboxCheckpointsPath && method == "GET") || (path == data.SandboxCheckpointCreatePath && method == "POST") || (path == data.SandboxCheckpointClonePath && method == "POST") || (path == data.SandboxCheckpointRestorePath && method == "POST") || (path == data.SandboxCreatePath && method == "POST")) {
		return data.ErrActionAuthorizationDenied
	}
	return nil
}

func TestSandboxCheckpointTransferHTTPIdentities(t *testing.T) {
	owner := &checkpointOwner{sandboxHTTPResource: new(sandboxHTTPResource)}
	authorizer := new(checkpointAuth)
	s := khttp.NewServer(khttp.Middleware(func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, r any) (any, error) {
			return next(auth.NewPrincipalContext(ctx, &auth.Principal{Type: auth.SubjectUser, ID: 9, TenantID: 7}), r)
		}
	}))
	RegisterSandboxHTTPServer(s, service.NewSandboxService(owner, authorizer, sandboxHTTPTenants{}, sandboxHTTPAdmission{}))
	checkpoint := "77777777-7777-4777-8777-777777777777"
	base := "/api/v1/sandbox/" + sandboxHTTPResourceID + "/checkpoints/" + checkpoint
	post := func(path, body string, want int) {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		s.ServeHTTP(w, req)
		require.Equal(t, want, w.Code, w.Body.String())
	}
	post(base+":clone", `{"data":{"name":"copy","preset_id":"22222222-2222-4222-8222-222222222222","image_reference":"registry.example/image:tag","idempotency_key":"clone","expected_version":"2"}}`, 200)
	require.Equal(t, sandboxHTTPResourceID, owner.clone.SourceSandboxId)
	require.Equal(t, checkpoint, owner.clone.CheckpointId)
	require.NotEqual(t, sandboxHTTPResourceID, owner.clone.Context.ResourceId)
	owner.clone = nil
	post(base+":clone", `{"data":{"source_sandbox_id":"11111111-1111-4111-8111-111111111111","name":"copy","preset_id":"22222222-2222-4222-8222-222222222222","image_reference":"registry.example/image:tag","idempotency_key":"clone","expected_version":"2"}}`, 400)
	require.Nil(t, owner.clone)
	post(base+":restore", `{"data":{"idempotency_key":"restore","expected_version":"3"}}`, 200)
	require.Equal(t, sandboxHTTPResourceID, owner.restore.SandboxId)
	require.Equal(t, checkpoint, owner.restore.CheckpointId)
	owner.restore = nil
	post(base+":restore", `{"data":{"checkpoint_id":"11111111-1111-4111-8111-111111111111","idempotency_key":"restore","expected_version":"3"}}`, 400)
	require.Nil(t, owner.restore)
}

func TestSandboxCheckpointsHTTP(t *testing.T) {
	owner := &checkpointOwner{sandboxHTTPResource: new(sandboxHTTPResource), result: &res.CreateSandboxCheckpointResponse{CheckpointId: "77777777-7777-4777-8777-777777777777", OperationId: "88888888-8888-4888-8888-888888888888", State: "accepted"}}
	authorizer := new(checkpointAuth)
	s := khttp.NewServer(khttp.Middleware(func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, r any) (any, error) {
			return next(auth.NewPrincipalContext(ctx, &auth.Principal{Type: auth.SubjectUser, ID: 9, TenantID: 7}), r)
		}
	}))
	RegisterSandboxHTTPServer(s, service.NewSandboxService(owner, authorizer, sandboxHTTPTenants{}, sandboxHTTPAdmission{}))
	path := "/api/v1/sandbox/" + sandboxHTTPResourceID + "/checkpoints"
	call := func(method, suffix, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path+suffix, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		s.ServeHTTP(w, req)
		require.Equal(t, want, w.Code, w.Body.String())
		return w
	}
	w := call("POST", "", `{"data":{"name":"before","idempotency_key":"stable","expected_version":"7"}}`, 200)
	require.Contains(t, w.Body.String(), `"checkpoint_id":"77777777-7777-4777-8777-777777777777"`)
	require.Equal(t, sandboxHTTPResourceID, owner.create.SandboxId)
	require.Equal(t, "stable", owner.create.IdempotencyKey)
	require.EqualValues(t, 7, owner.create.ExpectedVersion)
	owner.result.Replayed = true
	w = call("POST", "", `{"data":{"name":"before","idempotency_key":"stable","expected_version":"7"}}`, 200)
	require.Contains(t, w.Body.String(), `"replayed":true`)
	require.Equal(t, "stable", owner.create.IdempotencyKey)
	owner.create = nil
	call("POST", "", `{"data":{"sandbox_id":"33333333-3333-4333-8333-333333333333","name":"x","idempotency_key":"k","expected_version":"1"}}`, 400)
	call("POST", "", `{"data":{"name":"x","idempotency_key":"k"}}`, 400)
	call("POST", "", `{"data":{"name":"x","idempotency_key":"k","expected_version":"1","tenant_id":"other"}}`, 400)
	require.Nil(t, owner.create)
	authorizer.denied = true
	call("POST", "", `{"data":{"name":"x","idempotency_key":"k","expected_version":"1"}}`, 403)
	require.Nil(t, owner.create)
	authorizer.denied = false
	owner.result = &res.CreateSandboxCheckpointResponse{State: "accepted"}
	call("POST", "", `{"data":{"name":"x","idempotency_key":"k","expected_version":"1"}}`, 503)
	call("GET", "?page=2&pageSize=1", "", 200)
	require.EqualValues(t, 2, owner.list.Page)
	for _, q := range []string{"?page=0", "?pageSize=101", "?page=1&page=2", "?tenant_id=x"} {
		call("GET", q, "", 400)
	}
}

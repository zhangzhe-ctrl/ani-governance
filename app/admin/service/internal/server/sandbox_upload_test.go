package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-kratos/kratos/v2/middleware"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/stretchr/testify/require"
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	"go-wind-admin/app/admin/service/internal/service"
	"go-wind-admin/pkg/middleware/auth"
)

type uploadOwner struct {
	*sandboxHTTPResource
	mutation *res.MutateFilesRequest
	actor    string
}

func (o *uploadOwner) MutateSandboxFiles(_ context.Context, tenant, actor string, r *res.MutateFilesRequest) (*res.FileOperation, error) {
	o.calls++
	o.actor = actor
	o.mutation = r
	return &res.FileOperation{OperationId: r.OperationId, SandboxId: r.SandboxId, State: "succeeded", Result: &res.FileResult{Path: r.Path}}, nil
}

type uploadField struct {
	name, value string
	file        bool
}

func uploadForm(t *testing.T, fields ...uploadField) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for _, f := range fields {
		if f.file {
			p, err := w.CreateFormFile(f.name, "ignored-client-name.bin")
			require.NoError(t, err)
			_, err = p.Write([]byte(f.value))
			require.NoError(t, err)
		} else {
			require.NoError(t, w.WriteField(f.name, f.value))
		}
	}
	require.NoError(t, w.Close())
	return &body, w.FormDataContentType()
}

func TestSandboxMultipartUpload(t *testing.T) {
	owner := &uploadOwner{sandboxHTTPResource: new(sandboxHTTPResource)}
	authorization := new(sandboxHTTPAuthorization)
	server := khttp.NewServer(khttp.Middleware(func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, r any) (any, error) {
			return next(auth.NewPrincipalContext(ctx, &auth.Principal{Type: auth.SubjectUser, ID: 9, TenantID: 7}), r)
		}
	}))
	RegisterSandboxHTTPServer(server, service.NewSandboxService(owner, authorization, sandboxHTTPTenants{}, sandboxHTTPAdmission{}))
	path := "/api/v1/sandbox/" + sandboxHTTPResourceID + "/files"
	fields := []uploadField{{"path", "sub/destination.bin", false}, {"idempotency_key", "key", false}, {"expected_version", "3", false}, {"overwrite", "true", false}, {"file", "\x00\xff\x80hello\n", true}}
	fire := func(fields []uploadField, chunked bool) *httptest.ResponseRecorder {
		t.Helper()
		body, contentType := uploadForm(t, fields...)
		r := httptest.NewRequest("POST", path, body)
		r.Header.Set("Content-Type", contentType)
		if chunked {
			r.ContentLength = -1
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	w := fire(fields, false)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "sub/destination.bin", owner.mutation.Path)
	require.Equal(t, "key", owner.mutation.IdempotencyKey)
	require.EqualValues(t, 3, owner.mutation.ExpectedVersion)
	require.True(t, owner.mutation.Overwrite)
	require.Equal(t, "write", owner.mutation.Action)
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte(fields[4].value)), owner.mutation.ContentBase64)
	require.Equal(t, "33333333-3333-4333-8333-333333333333", owner.mutation.TenantId)
	require.Equal(t, "governance:user:9", owner.actor)
	require.NotContains(t, w.Body.String(), fields[4].value)
	for _, tc := range []struct {
		name    string
		fields  []uploadField
		code    int
		chunked bool
	}{
		{"duplicate path", append(append([]uploadField{}, fields...), uploadField{"path", "evil", false}), 400, false},
		{"duplicate file", append(append([]uploadField{}, fields...), uploadField{"file", "evil", true}), 400, false},
		{"unknown field", append(append([]uploadField{}, fields...), uploadField{"sandbox_id", "evil", false}), 400, false},
		{"missing file", fields[:4], 400, false},
		{"oversize unknown length", append(append([]uploadField{}, fields[:4]...), uploadField{"file", strings.Repeat("x", sandboxUploadFileLimit+1), true}), 413, true},
		{"maximum file", append(append([]uploadField{}, fields[:4]...), uploadField{"file", strings.Repeat("x", sandboxUploadFileLimit), true}), 200, true},
		{"bad overwrite", []uploadField{{"path", "x", false}, {"idempotency_key", "k", false}, {"overwrite", "1", false}, {"file", "x", true}}, 400, false},
		{"bad version", []uploadField{{"path", "x", false}, {"idempotency_key", "k", false}, {"expected_version", "-1", false}, {"file", "x", true}}, 400, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := owner.calls
			w := fire(tc.fields, tc.chunked)
			require.Equal(t, tc.code, w.Code, w.Body.String())
			if tc.code != 200 {
				require.Equal(t, before, owner.calls)
			}
		})
	}
	authorization.denied = true
	body := &sandboxCountingBody{}
	r := httptest.NewRequest("POST", path, nil)
	r.Body = body
	r.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	w = httptest.NewRecorder()
	server.ServeHTTP(w, r)
	require.Equal(t, 403, w.Code, w.Body.String())
	require.Zero(t, body.reads)
}

func TestSandboxMultipartUploadTokenAccess(t *testing.T) {
	verifier := &sandboxTokenVerifierStub{scope: "files", sandbox: sandboxHTTPResourceID}
	owner := &uploadOwner{sandboxHTTPResource: new(sandboxHTTPResource)}
	authorization := new(sandboxHTTPAuthorization)
	server := khttp.NewServer(khttp.Middleware(auth.Server(auth.WithSandboxTokenVerifier(verifier), auth.WithTenantAccessChecker(new(sandboxTokenTenantChecker)))))
	RegisterSandboxHTTPServer(server, service.NewSandboxService(owner, authorization, sandboxHTTPTenants{}, sandboxHTTPAdmission{}))
	fields := []uploadField{{"path", "x", false}, {"idempotency_key", "k", false}, {"file", "abc", true}}
	body, ct := uploadForm(t, fields...)
	r := httptest.NewRequest("POST", "/api/v1/sandbox/"+sandboxHTTPResourceID+"/files", body)
	r.Header.Set("Content-Type", ct)
	r.Header.Set("Authorization", "Bearer ani.sbx.fake.signature")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, r)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "governance:sandbox-token:"+testSandboxTokenID, owner.actor)
	authorization.denied = true
	counting := &sandboxCountingBody{}
	r = httptest.NewRequest("POST", "/api/v1/sandbox/"+sandboxHTTPResourceID+"/files", nil)
	r.Body = counting
	r.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	r.Header.Set("Authorization", "Bearer ani.sbx.fake.signature")
	w = httptest.NewRecorder()
	server.ServeHTTP(w, r)
	require.Equal(t, 403, w.Code, w.Body.String())
	require.Zero(t, counting.reads)
}

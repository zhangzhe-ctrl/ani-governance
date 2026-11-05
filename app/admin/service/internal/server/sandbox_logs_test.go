package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/middleware"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/stretchr/testify/require"
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	view "go-wind-admin/api/gen/go/sandbox/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/service"
	"go-wind-admin/pkg/middleware/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type sandboxLogOwner struct {
	*sandboxHTTPResource
	request *res.StreamSandboxLogsRequest
	open    func(context.Context) (grpc.ServerStreamingClient[res.StreamSandboxLogsResponse], error)
}

func (o *sandboxLogOwner) StreamSandboxLogs(ctx context.Context, tenant, actor string, r *res.StreamSandboxLogsRequest) (grpc.ServerStreamingClient[res.StreamSandboxLogsResponse], error) {
	if tenant != "33333333-3333-4333-8333-333333333333" || actor != "governance:user:9" || r.TenantId != tenant {
		panic("untrusted sandbox log scope")
	}
	o.request = r
	return o.open(ctx)
}

type sandboxLogAuthorizer struct{ denied bool }

func (a *sandboxLogAuthorizer) Authorize(_ context.Context, tenant, actor uint32, path, method string) error {
	if a.denied || tenant != 7 || actor != 9 || path != data.SandboxLogsPath || method != "GET" {
		return data.ErrActionAuthorizationDenied
	}
	return nil
}

type sandboxTestLogStream struct {
	grpc.ClientStream
	ctx    context.Context
	frames <-chan sandboxLogRead
	onRecv func()
}

func (s *sandboxTestLogStream) Recv() (*res.StreamSandboxLogsResponse, error) {
	if s.onRecv != nil {
		s.onRecv()
	}
	select {
	case frame, ok := <-s.frames:
		if !ok {
			return nil, io.EOF
		}
		return frame.frame, frame.err
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}
func sandboxLogFrames(frames ...*res.StreamSandboxLogsResponse) <-chan sandboxLogRead {
	ch := make(chan sandboxLogRead, len(frames))
	for _, f := range frames {
		ch <- sandboxLogRead{frame: f}
	}
	close(ch)
	return ch
}
func sandboxLogTestHTTP(t *testing.T, owner *sandboxLogOwner, authorizer *sandboxLogAuthorizer, limits InstanceLogLimits) *httptest.Server {
	t.Helper()
	s := khttp.NewServer(khttp.Timeout(0), khttp.Middleware(func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, r any) (any, error) {
			return next(auth.NewPrincipalContext(ctx, &auth.Principal{Type: auth.SubjectUser, ID: 9, TenantID: 7}), r)
		}
	}))
	svc := service.NewSandboxService(owner, authorizer, sandboxHTTPTenants{}, sandboxHTTPAdmission{})
	RegisterSandboxLogsHTTP(s, svc, limits)
	actual := httptest.NewServer(s)
	t.Cleanup(actual.Close)
	return actual
}
func TestSandboxLogQueryStrict(t *testing.T) {
	for _, query := range []string{"tailLines=0", "tailLines=01", "tailLines=1001", "tailLines=1&tailLines=2", "follow=TRUE", "follow=", "token=x", "sinceTime=2026-11-03T12:00:00Z", "podUid=p-1", "sinceTime=2026-11-03T12:00:00Z&podUid=p-1&tailLines=2", "bad=%zz"} {
		_, err := decodeSandboxLogQuery(httptest.NewRequest("GET", "/logs?"+query, nil))
		require.Error(t, err, query)
	}
	parsed, err := decodeSandboxLogQuery(httptest.NewRequest("GET", "/logs?sinceTime=2026-11-03T12%3A00%3A00Z&podUid=p-1&follow=false", nil))
	require.NoError(t, err)
	require.Equal(t, "p-1", parsed.PodUid)
	require.Equal(t, "2026-11-03T12:00:00Z", parsed.SinceTime)
	require.False(t, parsed.GetFollow())
	_, err = decodeSandboxLogQuery(httptest.NewRequest("GET", "/logs?", nil))
	require.Error(t, err)
	_, err = decodeSandboxLogQuery(httptest.NewRequest("GET", "/logs", strings.NewReader("x")))
	require.Error(t, err)
}
func TestSandboxLogHTTPReadyResumeAndAuth(t *testing.T) {
	o := &sandboxLogOwner{sandboxHTTPResource: new(sandboxHTTPResource)}
	o.open = func(ctx context.Context) (grpc.ServerStreamingClient[res.StreamSandboxLogsResponse], error) {
		return &sandboxTestLogStream{ctx: ctx, frames: sandboxLogFrames(&res.StreamSandboxLogsResponse{Kind: "READY", PodUid: "p-1"}, &res.StreamSandboxLogsResponse{Kind: "DATA", Data: []byte("2020-11-03T12:00:00Z hello\n")}, &res.StreamSandboxLogsResponse{Kind: "END", Reason: "EOF"})}, nil
	}
	authz := new(sandboxLogAuthorizer)
	actual := sandboxLogTestHTTP(t, o, authz, DefaultInstanceLogLimits())
	path := actual.URL + "/api/v1/sandbox/" + sandboxHTTPResourceID + "/logs"
	resp, err := http.Get(path + "?sinceTime=2020-11-03T12%3A00%3A00Z&podUid=p-1&follow=false")
	require.NoError(t, err)
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "text/event-stream")
	require.Contains(t, string(raw), "event: ready\ndata: {\"pod_uid\":\"p-1\"}")
	require.Contains(t, string(raw), "2020-11-03T12:00:00Z hello")
	require.Contains(t, string(raw), "event: cursor\ndata: {\"pod_uid\":\"p-1\",\"since_time\":\"2020-11-03T12:00:00Z\"}")
	require.Less(t, strings.Index(string(raw), "2020-11-03T12:00:00Z hello"), strings.Index(string(raw), "event: cursor"))
	require.Less(t, strings.Index(string(raw), "event: cursor"), strings.Index(string(raw), "event: end"))
	require.Contains(t, string(raw), "event: end\ndata: {\"reason\":\"EOF\"}")
	require.EqualValues(t, 0, o.request.TailLines)
	require.False(t, o.request.Follow)
	require.Equal(t, "p-1", o.request.PodUid)
	require.Equal(t, "2020-11-03T12:00:00Z", o.request.SinceTime.AsTime().Format(time.RFC3339))
	authz.denied = true
	o.request = nil
	resp, err = http.Get(path)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, 403, resp.StatusCode)
	require.Nil(t, o.request)
	authz.denied = false
	resp, err = http.Get(path + "?sinceTime=2026-11-03T12%3A00%3A00Z&podUid=another")
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, 503, resp.StatusCode)
	require.NotContains(t, resp.Header.Get("Content-Type"), "text/event-stream")
}

func TestSandboxLogHTTPCursorRequiresCompleteDeliveredLine(t *testing.T) {
	o := &sandboxLogOwner{sandboxHTTPResource: new(sandboxHTTPResource)}
	o.open = func(ctx context.Context) (grpc.ServerStreamingClient[res.StreamSandboxLogsResponse], error) {
		return &sandboxTestLogStream{ctx: ctx, frames: sandboxLogFrames(
			&res.StreamSandboxLogsResponse{Kind: "READY", PodUid: "p-1"},
			&res.StreamSandboxLogsResponse{Kind: "DATA", Data: append([]byte("2020-11-03T12:00:00.123Z fi"), 0xe4, 0xb8)},
			&res.StreamSandboxLogsResponse{Kind: "DATA", Data: append([]byte{0xad}, []byte("rst\n2020-11-03T12:00:01Z par")...)},
			&res.StreamSandboxLogsResponse{Kind: "END", Reason: "BYTE_LIMIT"},
		)}, nil
	}
	actual := sandboxLogTestHTTP(t, o, new(sandboxLogAuthorizer), DefaultInstanceLogLimits())
	resp, err := actual.Client().Get(actual.URL + "/api/v1/sandbox/" + sandboxHTTPResourceID + "/logs?follow=false")
	require.NoError(t, err)
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)
	response := string(raw)
	require.Contains(t, response, "\"since_time\":\"2020-11-03T12:00:00.123Z\"")
	require.NotContains(t, response, "\"since_time\":\"2020-11-03T12:00:01Z\"")
	require.Contains(t, response, "中rst")
	require.Less(t, strings.Index(response, "rst\\n"), strings.Index(response, "event: cursor"))
	require.Contains(t, response, "\"reason\":\"BYTE_LIMIT\"")
}

func TestSandboxLogHTTPCursorDoesNotSkipEarlierPartialLine(t *testing.T) {
	o := &sandboxLogOwner{sandboxHTTPResource: new(sandboxHTTPResource)}
	o.open = func(ctx context.Context) (grpc.ServerStreamingClient[res.StreamSandboxLogsResponse], error) {
		return &sandboxTestLogStream{ctx: ctx, frames: sandboxLogFrames(
			&res.StreamSandboxLogsResponse{Kind: "READY", PodUid: "p-1"},
			&res.StreamSandboxLogsResponse{Kind: "DATA", Data: []byte("2020-11-03T12:00:03Z complete\n2020-11-03T12:00:01Z partial")},
			&res.StreamSandboxLogsResponse{Kind: "END", Reason: "BYTE_LIMIT"},
		)}, nil
	}
	actual := sandboxLogTestHTTP(t, o, new(sandboxLogAuthorizer), DefaultInstanceLogLimits())
	resp, err := actual.Client().Get(actual.URL + "/api/v1/sandbox/" + sandboxHTTPResourceID + "/logs?follow=false")
	require.NoError(t, err)
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)
	require.Contains(t, string(raw), "partial")
	require.Contains(t, string(raw), "\"since_time\":\"2020-11-03T12:00:01Z\"")
	require.NotContains(t, string(raw), "\"since_time\":\"2020-11-03T12:00:03Z\"")
}

func TestSandboxLogHTTPCursorRetractsAcrossFrames(t *testing.T) {
	o := &sandboxLogOwner{sandboxHTTPResource: new(sandboxHTTPResource)}
	o.open = func(ctx context.Context) (grpc.ServerStreamingClient[res.StreamSandboxLogsResponse], error) {
		return &sandboxTestLogStream{ctx: ctx, frames: sandboxLogFrames(
			&res.StreamSandboxLogsResponse{Kind: "READY", PodUid: "p-1"},
			&res.StreamSandboxLogsResponse{Kind: "DATA", Data: []byte("2020-11-03T12:00:03Z complete\n")},
			&res.StreamSandboxLogsResponse{Kind: "DATA", Data: []byte("2020-11-03T12:00:01Z partial")},
			&res.StreamSandboxLogsResponse{Kind: "DATA", Data: []byte("\n2020-11-03T12:00:04Z later\n")},
			&res.StreamSandboxLogsResponse{Kind: "END", Reason: "EOF"},
		)}, nil
	}
	actual := sandboxLogTestHTTP(t, o, new(sandboxLogAuthorizer), DefaultInstanceLogLimits())
	resp, err := actual.Client().Get(actual.URL + "/api/v1/sandbox/" + sandboxHTTPResourceID + "/logs?follow=false")
	require.NoError(t, err)
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)
	response := string(raw)
	first := strings.Index(response, "\"since_time\":\"2020-11-03T12:00:03Z\"")
	second := strings.Index(response, "\"since_time\":\"2020-11-03T12:00:01Z\"")
	require.GreaterOrEqual(t, first, 0)
	require.Greater(t, second, first)
	require.NotContains(t, response, "\"since_time\":\"2020-11-03T12:00:04Z\"")
}

func TestSandboxLogHTTPCursorInvalidation(t *testing.T) {
	for _, text := range []string{"invalid timestamp\n", "2999-01-01T00:00:00Z future\n"} {
		o := &sandboxLogOwner{sandboxHTTPResource: new(sandboxHTTPResource)}
		o.open = func(ctx context.Context) (grpc.ServerStreamingClient[res.StreamSandboxLogsResponse], error) {
			return &sandboxTestLogStream{ctx: ctx, frames: sandboxLogFrames(
				&res.StreamSandboxLogsResponse{Kind: "READY", PodUid: "p-1"},
				&res.StreamSandboxLogsResponse{Kind: "DATA", Data: []byte("2020-11-03T12:00:03Z complete\n")},
				&res.StreamSandboxLogsResponse{Kind: "DATA", Data: []byte(text)},
				&res.StreamSandboxLogsResponse{Kind: "DATA", Data: []byte("2020-11-03T12:00:04Z later\n")},
				&res.StreamSandboxLogsResponse{Kind: "END", Reason: "EOF"},
			)}, nil
		}
		actual := sandboxLogTestHTTP(t, o, new(sandboxLogAuthorizer), DefaultInstanceLogLimits())
		resp, err := actual.Client().Get(actual.URL + "/api/v1/sandbox/" + sandboxHTTPResourceID + "/logs?follow=false")
		require.NoError(t, err)
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		require.NoError(t, err)
		response := string(raw)
		require.Contains(t, response, "\"since_time\":\"2020-11-03T12:00:03Z\"")
		require.Contains(t, response, "event: cursor_invalidated\ndata: {\"pod_uid\":\"p-1\",\"reason\":\"TIMESTAMP_UNAVAILABLE\"}")
		require.Greater(t, strings.Index(response, "event: cursor_invalidated"), strings.Index(response, "event: cursor\n"))
		require.NotContains(t, response, "\"since_time\":\"2020-11-03T12:00:04Z\"")
	}
}

func TestSandboxLogHTTPResumedCursorInvalidatesFirstBadLine(t *testing.T) {
	o := &sandboxLogOwner{sandboxHTTPResource: new(sandboxHTTPResource)}
	o.open = func(ctx context.Context) (grpc.ServerStreamingClient[res.StreamSandboxLogsResponse], error) {
		return &sandboxTestLogStream{ctx: ctx, frames: sandboxLogFrames(
			&res.StreamSandboxLogsResponse{Kind: "READY", PodUid: "p-1"},
			&res.StreamSandboxLogsResponse{Kind: "DATA", Data: []byte("invalid timestamp\n")},
			&res.StreamSandboxLogsResponse{Kind: "END", Reason: "EOF"},
		)}, nil
	}
	actual := sandboxLogTestHTTP(t, o, new(sandboxLogAuthorizer), DefaultInstanceLogLimits())
	resp, err := actual.Client().Get(actual.URL + "/api/v1/sandbox/" + sandboxHTTPResourceID + "/logs?sinceTime=2020-11-03T12%3A00%3A03Z&podUid=p-1&follow=false")
	require.NoError(t, err)
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)
	require.Contains(t, string(raw), "event: cursor_invalidated\ndata: {\"pod_uid\":\"p-1\",\"reason\":\"TIMESTAMP_UNAVAILABLE\"}")
}

func TestSandboxLogRejectsOversizedFrameMetadata(t *testing.T) {
	for _, frame := range []*res.StreamSandboxLogsResponse{
		{Kind: "DATA", Reason: strings.Repeat("x", 10000)},
		{Kind: "DATA", PodUid: strings.Repeat("x", 10000)},
		{Kind: strings.Repeat("x", 10000)},
	} {
		o := &sandboxLogOwner{sandboxHTTPResource: new(sandboxHTTPResource)}
		o.open = func(ctx context.Context) (grpc.ServerStreamingClient[res.StreamSandboxLogsResponse], error) {
			return &sandboxTestLogStream{ctx: ctx, frames: sandboxLogFrames(
				&res.StreamSandboxLogsResponse{Kind: "READY", PodUid: "p-1"}, frame,
			)}, nil
		}
		actual := sandboxLogTestHTTP(t, o, new(sandboxLogAuthorizer), DefaultInstanceLogLimits())
		resp, err := actual.Client().Get(actual.URL + "/api/v1/sandbox/" + sandboxHTTPResourceID + "/logs?follow=false")
		require.NoError(t, err)
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		require.NoError(t, err)
		require.Contains(t, string(raw), "SANDBOX_LOG_UPSTREAM_PROTOCOL")
		require.NotContains(t, string(raw), strings.Repeat("x", 100))
	}
}

func TestSandboxLogAcceptsMaximumDataFrame(t *testing.T) {
	o := &sandboxLogOwner{sandboxHTTPResource: new(sandboxHTTPResource)}
	o.open = func(ctx context.Context) (grpc.ServerStreamingClient[res.StreamSandboxLogsResponse], error) {
		return &sandboxTestLogStream{ctx: ctx, frames: sandboxLogFrames(
			&res.StreamSandboxLogsResponse{Kind: "READY", PodUid: "p-1"},
			&res.StreamSandboxLogsResponse{Kind: "DATA", Data: []byte(strings.Repeat("x", 16*1024))},
			&res.StreamSandboxLogsResponse{Kind: "END", Reason: "EOF"},
		)}, nil
	}
	actual := sandboxLogTestHTTP(t, o, new(sandboxLogAuthorizer), DefaultInstanceLogLimits())
	resp, err := actual.Client().Get(actual.URL + "/api/v1/sandbox/" + sandboxHTTPResourceID + "/logs?follow=false")
	require.NoError(t, err)
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)
	require.Greater(t, strings.Count(string(raw), "event: log"), 1)
	require.Contains(t, string(raw), "\"reason\":\"EOF\"")
}

type failingSandboxLogWriter struct {
	header http.Header
	output strings.Builder
}

func (w *failingSandboxLogWriter) Header() http.Header            { return w.header }
func (*failingSandboxLogWriter) WriteHeader(int)                  {}
func (*failingSandboxLogWriter) SetWriteDeadline(time.Time) error { return nil }
func (*failingSandboxLogWriter) FlushError() error                { return nil }
func (w *failingSandboxLogWriter) Write(raw []byte) (int, error) {
	if strings.HasPrefix(string(raw), "event: log") {
		return 0, io.ErrClosedPipe
	}
	return w.output.Write(raw)
}

func TestSandboxLogCursorNotSentWhenLogWriteFails(t *testing.T) {
	o := &sandboxLogOwner{sandboxHTTPResource: new(sandboxHTTPResource)}
	o.open = func(ctx context.Context) (grpc.ServerStreamingClient[res.StreamSandboxLogsResponse], error) {
		return &sandboxTestLogStream{ctx: ctx, frames: sandboxLogFrames(
			&res.StreamSandboxLogsResponse{Kind: "READY", PodUid: "p-1"},
			&res.StreamSandboxLogsResponse{Kind: "DATA", Data: []byte("2020-11-03T12:00:00Z hello\n")},
			&res.StreamSandboxLogsResponse{Kind: "END", Reason: "EOF"},
		)}, nil
	}
	limits := DefaultInstanceLogLimits()
	guard := &instanceLogHTTP{limits: limits, tenants: map[string]int{}, instances: map[string]int{}}
	svc := service.NewSandboxService(o, new(sandboxLogAuthorizer), sandboxHTTPTenants{}, sandboxHTTPAdmission{})
	h := &sandboxLogHTTP{service: svc, guard: guard}
	ctx := auth.NewPrincipalContext(context.Background(), &auth.Principal{Type: auth.SubjectUser, ID: 9, TenantID: 7})
	w := &failingSandboxLogWriter{header: http.Header{}}
	require.NoError(t, h.stream(ctx, w, &view.StreamSandboxLogsRequest{SandboxId: sandboxHTTPResourceID}))
	require.Contains(t, w.output.String(), "event: ready")
	require.NotContains(t, w.output.String(), "event: cursor")
	require.Zero(t, guard.active)
}
func TestSandboxLogHTTPUpstreamAndCancellation(t *testing.T) {
	o := &sandboxLogOwner{sandboxHTTPResource: new(sandboxHTTPResource)}
	o.open = func(context.Context) (grpc.ServerStreamingClient[res.StreamSandboxLogsResponse], error) {
		return nil, status.Error(codes.FailedPrecondition, "SANDBOX_DATA_PLANE_UNAVAILABLE")
	}
	actual := sandboxLogTestHTTP(t, o, new(sandboxLogAuthorizer), DefaultInstanceLogLimits())
	path := actual.URL + "/api/v1/sandbox/" + sandboxHTTPResourceID + "/logs"
	resp, err := http.Get(path)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, 409, resp.StatusCode)
	canceled := make(chan struct{})
	o.open = func(ctx context.Context) (grpc.ServerStreamingClient[res.StreamSandboxLogsResponse], error) {
		ch := make(chan sandboxLogRead, 1)
		ch <- sandboxLogRead{frame: &res.StreamSandboxLogsResponse{Kind: "READY", PodUid: "p-1"}}
		go func() { <-ctx.Done(); close(canceled) }()
		return &sandboxTestLogStream{ctx: ctx, frames: ch}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, "GET", path, nil)
	require.NoError(t, err)
	resp, err = actual.Client().Do(request)
	require.NoError(t, err)
	cancel()
	resp.Body.Close()
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not inherit cancellation")
	}
}
func TestSandboxLogReadyProtocol(t *testing.T) {
	_ = (*view.StreamSandboxLogsRequest)(nil)
	limits := DefaultInstanceLogLimits()
	limits.OpenTimeout = 30 * time.Millisecond
	o := &sandboxLogOwner{sandboxHTTPResource: new(sandboxHTTPResource)}
	o.open = func(ctx context.Context) (grpc.ServerStreamingClient[res.StreamSandboxLogsResponse], error) {
		return &sandboxTestLogStream{ctx: ctx, frames: make(chan sandboxLogRead)}, nil
	}
	actual := sandboxLogTestHTTP(t, o, new(sandboxLogAuthorizer), limits)
	resp, err := http.Get(actual.URL + "/api/v1/sandbox/" + sandboxHTTPResourceID + "/logs")
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, 503, resp.StatusCode)
	require.NotContains(t, resp.Header.Get("Content-Type"), "text/event-stream")
}

func TestSandboxLogSlowClientCancelsBoundedReader(t *testing.T) {
	limits := DefaultInstanceLogLimits()
	limits.WriteTimeout = 15 * time.Millisecond
	upstream := make(chan context.Context, 1)
	var recvs atomic.Int32
	o := &sandboxLogOwner{sandboxHTTPResource: new(sandboxHTTPResource)}
	o.open = func(ctx context.Context) (grpc.ServerStreamingClient[res.StreamSandboxLogsResponse], error) {
		ch := make(chan sandboxLogRead, 100)
		ch <- sandboxLogRead{frame: &res.StreamSandboxLogsResponse{Kind: "READY", PodUid: "p-1"}}
		for i := 0; i < 99; i++ {
			ch <- sandboxLogRead{frame: &res.StreamSandboxLogsResponse{Kind: "DATA", Data: []byte(strings.Repeat("x", limits.MaxFrameBytes))}}
		}
		upstream <- ctx
		return &sandboxTestLogStream{ctx: ctx, frames: ch, onRecv: func() { recvs.Add(1) }}, nil
	}
	guard := &instanceLogHTTP{limits: limits, tenants: map[string]int{}, instances: map[string]int{}}
	svc := service.NewSandboxService(o, new(sandboxLogAuthorizer), sandboxHTTPTenants{}, sandboxHTTPAdmission{})
	h := &sandboxLogHTTP{service: svc, guard: guard}
	ctx := auth.NewPrincipalContext(context.Background(), &auth.Principal{Type: auth.SubjectUser, ID: 9, TenantID: 7})
	require.NoError(t, h.stream(ctx, &stalledLogWriter{header: http.Header{}}, &view.StreamSandboxLogsRequest{SandboxId: sandboxHTTPResourceID}))
	streamContext := <-upstream
	select {
	case <-streamContext.Done():
	case <-time.After(time.Second):
		t.Fatal("slow client leaked upstream")
	}
	require.LessOrEqual(t, recvs.Load(), int32(12))
	require.Zero(t, guard.active)
}

func TestSandboxLogConnectionLimit(t *testing.T) {
	limits := DefaultInstanceLogLimits()
	limits.InstanceConnections = 1
	o := &sandboxLogOwner{sandboxHTTPResource: new(sandboxHTTPResource)}
	o.open = func(ctx context.Context) (grpc.ServerStreamingClient[res.StreamSandboxLogsResponse], error) {
		ch := make(chan sandboxLogRead, 1)
		ch <- sandboxLogRead{frame: &res.StreamSandboxLogsResponse{Kind: "READY", PodUid: "p-1"}}
		return &sandboxTestLogStream{ctx: ctx, frames: ch}, nil
	}
	actual := sandboxLogTestHTTP(t, o, new(sandboxLogAuthorizer), limits)
	path := actual.URL + "/api/v1/sandbox/" + sandboxHTTPResourceID + "/logs"
	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	require.NoError(t, err)
	first, err := actual.Client().Do(request)
	require.NoError(t, err)
	require.Equal(t, 200, first.StatusCode)
	second, err := actual.Client().Get(path)
	require.NoError(t, err)
	second.Body.Close()
	require.Equal(t, 429, second.StatusCode)
	cancel()
	first.Body.Close()
}

func TestNewRestServerRegistersSandboxLogs(t *testing.T) {
	sandbox := service.NewSandboxService(nil, nil, nil, nil)
	srv := newRestServerWithSwagger(t, false, sandbox)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/sandbox/"+sandboxHTTPResourceID+"/logs", nil))
	require.Equal(t, http.StatusUnauthorized, w.Code, "logs must reach the authentication middleware")
}

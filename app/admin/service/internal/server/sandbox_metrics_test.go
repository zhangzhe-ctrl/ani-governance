package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/middleware"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/stretchr/testify/require"
	pb "github.com/zhangzhe-ctrl/ani-observability-service/api/observability/monitoring/v1"
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/service"
	"go-wind-admin/pkg/middleware/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type sandboxMetricsOwner struct {
	*sandboxHTTPResource
	history    []*res.SandboxPodHistory
	historyErr error
}

func (o *sandboxMetricsOwner) ListSandboxPodHistory(_ context.Context, tenant, actor string, r *res.ListSandboxPodHistoryRequest) (*res.ListSandboxPodHistoryResponse, error) {
	if tenant != r.TenantId || actor != "governance:user:9" {
		panic("untrusted sandbox metric history scope")
	}
	return &res.ListSandboxPodHistoryResponse{Pods: o.history}, o.historyErr
}

type sandboxMetricsEndpoint struct {
	t                       *testing.T
	tenant, cluster, podUID string
	duplicateReceive        atomic.Bool
	historyCount            int
	onHistory               func()
}

func (e *sandboxMetricsEndpoint) series(at time.Time) []*pb.SandboxMetricSeries {
	value := 1.0
	point := func() []*pb.SandboxMetricPoint {
		return []*pb.SandboxMetricPoint{{Timestamp: timestamppb.New(at), Value: &value, Status: "ok"}}
	}
	receive := &pb.SandboxMetricSeries{Name: "network_receive", Unit: "bytes_per_second", Status: "ok", Points: point()}
	if e.duplicateReceive.Load() {
		receive.Status = "unavailable"
		receive.Reason = "AMBIGUOUS_METRIC_SOURCE"
		receive.Points = nil
	}
	return []*pb.SandboxMetricSeries{
		{Name: "cpu", Unit: "cores", Status: "ok", Points: point()}, receive,
		{Name: "network_transmit", Unit: "bytes_per_second", Status: "ok", Points: point()},
		{Name: "memory", Unit: "bytes", Status: "unavailable", Reason: "KATA_MEMORY_SOURCE_UNAVAILABLE"},
	}
}
func (e *sandboxMetricsEndpoint) check(tenant, actor string, target *pb.SandboxTarget) error {
	require.Equal(e.t, e.tenant, tenant)
	require.Equal(e.t, "governance:user:9", actor)
	require.Equal(e.t, tenant, target.TenantId)
	require.Equal(e.t, sandboxHTTPResourceID, target.SandboxId)
	if target.ClusterId != e.cluster {
		return fmt.Errorf("unknown datasource")
	}
	return nil
}
func (e *sandboxMetricsEndpoint) QuerySandboxLatest(_ context.Context, tenant, actor string, r *pb.QuerySandboxLatestRequest) (*pb.QuerySandboxLatestResponse, error) {
	if err := e.check(tenant, actor, r.Target); err != nil {
		return nil, err
	}
	require.Equal(e.t, []string{e.podUID}, r.Target.PodUids)
	return &pb.QuerySandboxLatestResponse{Series: e.series(time.Now())}, nil
}
func (e *sandboxMetricsEndpoint) QuerySandboxRange(_ context.Context, tenant, actor string, r *pb.QuerySandboxRangeRequest) (*pb.QuerySandboxRangeResponse, error) {
	if err := e.check(tenant, actor, r.Target); err != nil {
		return nil, err
	}
	e.historyCount = len(r.Target.PodUids)
	if e.onHistory != nil {
		e.onHistory()
	}
	return &pb.QuerySandboxRangeResponse{Start: r.Start, End: r.End, StepSeconds: r.StepSeconds, Series: e.series(r.End.AsTime())}, nil
}
func TestSandboxMetricsHTTPAuthorizationAndPodHistory(t *testing.T) {
	tenant := "33333333-3333-4333-8333-333333333333"
	cluster := "11111111-1111-4111-8111-111111111111"
	podUID := "44444444-4444-4444-8444-444444444444"
	namespaceUID := "55555555-5555-4555-8555-555555555555"
	endpoint := &sandboxMetricsEndpoint{t: t, tenant: tenant, cluster: cluster, podUID: podUID}
	sandbox := &res.Sandbox{Id: sandboxHTTPResourceID, TenantId: tenant, Version: 1, DesiredState: "running", ObservedState: "running", Snapshot: &res.SandboxSnapshot{ClusterId: cluster, Namespace: "tenant-" + tenant, NamespaceUid: namespaceUID, RuntimeClass: "kata"}, ProviderRefs: &res.ProviderRefs{PodName: "sandbox-" + sandboxHTTPResourceID, PodUid: podUID}}
	owner := &sandboxMetricsOwner{sandboxHTTPResource: &sandboxHTTPResource{created: sandbox}, history: []*res.SandboxPodHistory{{ClusterId: cluster, Namespace: "tenant-" + tenant, NamespaceUid: namespaceUID, PodName: "sandbox-" + sandboxHTTPResourceID, PodUid: podUID, FirstRecordedAt: "2026-11-03T00:00:00Z"}}}
	authorizer := new(sandboxHTTPAuthorization)
	svc := service.NewSandboxService(owner, authorizer, sandboxHTTPTenants{}, sandboxHTTPAdmission{})
	reader := data.NewSandboxMetricsReader(endpoint)
	svc.SetMetricsReader(reader)
	server := khttp.NewServer(khttp.Middleware(func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, r any) (any, error) {
			return next(auth.NewPrincipalContext(ctx, &auth.Principal{Type: auth.SubjectUser, ID: 9, TenantID: 7}), r)
		}
	}))
	RegisterSandboxHTTPServer(server, svc)
	path := "/api/v1/sandbox/" + sandboxHTTPResourceID + "/metrics"
	get := func(p string, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		server.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		require.Equal(t, want, w.Code, w.Body.String())
		return w
	}
	w := get(path+"/latest", 200)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "current_pod_only", body["scope"])
	require.Equal(t, podUID, body["pod_uid"])
	series := body["series"].([]any)
	require.Len(t, series, 4)
	require.Equal(t, "unavailable", series[3].(map[string]any)["status"])
	endpoint.duplicateReceive.Store(true)
	w = get(path+"/latest", 200)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	series = body["series"].([]any)
	require.Equal(t, "ok", series[0].(map[string]any)["status"])
	require.Equal(t, "unavailable", series[1].(map[string]any)["status"])
	require.Equal(t, "AMBIGUOUS_METRIC_SOURCE", series[1].(map[string]any)["reason"])
	require.Equal(t, "ok", series[2].(map[string]any)["status"])
	endpoint.duplicateReceive.Store(false)
	w = get(path+"?step=60", 200)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "known_pods_only", body["scope"])
	// A Pod UID can be recorded while the metrics RPC runs without changing
	// the Sandbox version. The second history read must reject that result.
	initialVersion := owner.created.Version
	recordPod := make(chan struct{})
	recorded := make(chan struct{})
	go func() {
		<-recordPod
		owner.history = append(owner.history, &res.SandboxPodHistory{ClusterId: cluster, Namespace: "tenant-" + tenant, NamespaceUid: namespaceUID, PodName: "sandbox-" + sandboxHTTPResourceID, PodUid: "88888888-8888-4888-8888-888888888888", FirstRecordedAt: "2026-11-03T00:00:00Z"})
		close(recorded)
	}()
	endpoint.onHistory = func() {
		close(recordPod)
		<-recorded
	}
	get(path+"?step=60", 503)
	require.Equal(t, initialVersion, owner.created.Version)
	endpoint.onHistory = nil
	owner.history = owner.history[:1]
	// More than 32 recorded generations must reach the batched source scan;
	// a narrow time range cannot justify discarding the other identities.
	for i := 0; i < 32; i++ {
		owner.history = append(owner.history, &res.SandboxPodHistory{ClusterId: cluster, Namespace: "tenant-" + tenant, NamespaceUid: namespaceUID, PodName: "sandbox-" + sandboxHTTPResourceID, PodUid: fmt.Sprintf("%08x-4444-4444-8444-%012x", i+1, i+1), FirstRecordedAt: "2026-11-03T00:00:00Z"})
	}
	get(path+"?step=60", 200)
	require.Equal(t, 33, endpoint.historyCount)
	owner.history = owner.history[:1]
	owner.historyErr = status.Error(codes.ResourceExhausted, "SANDBOX_POD_HISTORY_TOO_LARGE")
	limitResponse := get(path+"?step=60", 422)
	require.Contains(t, limitResponse.Body.String(), "SANDBOX_METRICS_HISTORY_LIMIT")
	owner.historyErr = nil
	for _, suffix := range []string{"?step=0", "?step=14", "?step=60&step=120", "?tenant_id=other", "/latest?step=60", "?start=2026-11-03T00:00:00.5Z"} {
		get(path+suffix, 400)
	}
	authorizer.denied = true
	before := owner.calls
	get(path+"/latest", 403)
	get(path+"?step=60", 403)
	require.Equal(t, before, owner.calls)
	authorizer.denied = false
	owner.created.DesiredState = "paused"
	get(path+"/latest", 503)
	get(path+"?step=60", 200)
	owner.created.DesiredState = "running"
	owner.created.Snapshot.RuntimeClass = "runc"
	get(path+"/latest", 503)
	get(path+"?step=60", 503)
	owner.created.Snapshot.RuntimeClass = "kata"
	owner.history = nil
	owner.created.Snapshot.ClusterId = "66666666-6666-4666-8666-666666666666"
	get(path+"?step=60", 503)
	owner.created.Snapshot.ClusterId = cluster
	owner.created.Snapshot.Namespace = "tenant-other"
	get(path+"/latest", 503)
	get(path+"?step=60", 503)
}

package service

import (
	"context"
	"time"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	view "go-wind-admin/api/gen/go/sandbox/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func (s *SandboxService) GetSandboxMetrics(ctx context.Context, r *view.GetSandboxMetricsRequest) (*view.SandboxMetricsResponse, error) {
	if r == nil || !sandboxUUID(r.SandboxId) {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid sandbox identity")
	}
	now := time.Now().UTC()
	end := now.Truncate(time.Second)
	if r.End != "" {
		var err error
		end, err = time.Parse(time.RFC3339Nano, r.End)
		if err != nil {
			return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid end")
		}
	}
	start := end.Add(-time.Hour)
	if r.Start != "" {
		var err error
		start, err = time.Parse(time.RFC3339Nano, r.Start)
		if err != nil {
			return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid start")
		}
	}
	step := r.Step
	if step == 0 {
		step = 60
	}
	if start.Nanosecond() != 0 || end.Nanosecond() != 0 || step < 15 || step > 3600 || !end.After(start) || end.Sub(start) > 24*time.Hour || end.Sub(start)/time.Second/time.Duration(step) > 1440 || end.After(now.Add(time.Minute)) {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid metrics range or step")
	}
	return s.sandboxHistoryMetrics(ctx, r.SandboxId, start, end, step)
}

type sandboxPodHistoryClient interface {
	ListSandboxPodHistory(context.Context, string, string, *res.ListSandboxPodHistoryRequest) (*res.ListSandboxPodHistoryResponse, error)
}

func (s *SandboxService) sandboxHistoryMetrics(ctx context.Context, id string, start, end time.Time, step uint32) (*view.SandboxMetricsResponse, error) {
	tenant, actor, err := s.scope(ctx, data.SandboxMetricsPath, "GET")
	if err != nil {
		return nil, err
	}
	if s.metrics == nil {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_METRICS_UNAVAILABLE", "sandbox metrics source unavailable")
	}
	client, ok := s.client.(sandboxPodHistoryClient)
	if !ok {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_METRICS_UNAVAILABLE", "sandbox history unavailable")
	}
	first, err := s.client.GetSandbox(ctx, tenant, actor, &res.GetSandboxRequest{TenantId: tenant, SandboxId: id})
	if err != nil {
		return nil, err
	}
	if first == nil || first.Sandbox == nil || first.Sandbox.Snapshot == nil || first.Sandbox.Id != id || first.Sandbox.TenantId != tenant || first.Sandbox.Version < 1 || !sandboxUUID(first.Sandbox.Snapshot.ClusterId) || first.Sandbox.Snapshot.Namespace != "tenant-"+tenant || !sandboxUUID(first.Sandbox.Snapshot.NamespaceUid) || !sandboxKataRuntime(first.Sandbox.Snapshot.RuntimeClass) {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_METRICS_UNAVAILABLE", "sandbox history scope unavailable")
	}
	history, err := client.ListSandboxPodHistory(ctx, tenant, actor, &res.ListSandboxPodHistoryRequest{TenantId: tenant, SandboxId: id})
	if err != nil {
		if status.Code(err) == codes.ResourceExhausted && status.Convert(err).Message() == "SANDBOX_POD_HISTORY_TOO_LARGE" {
			return nil, kratoserrors.New(422, "SANDBOX_METRICS_HISTORY_LIMIT", "sandbox has more than 1000 recorded Pod identities")
		}
		return nil, err
	}
	if history == nil {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_METRICS_UNAVAILABLE", "sandbox history unavailable")
	}
	if len(history.Pods) > 1000 {
		return nil, kratoserrors.New(422, "SANDBOX_METRICS_HISTORY_LIMIT", "sandbox has more than 1000 recorded Pod identities")
	}
	// Resource may add a discovered Pod UID without incrementing Sandbox
	// version. Keep an independent snapshot for a post-query history fence.
	history = proto.Clone(history).(*res.ListSandboxPodHistoryResponse)
	scopes := make([]data.SandboxMetricScope, 0, len(history.Pods))
	seen := map[string]bool{}
	for _, pod := range history.Pods {
		if pod == nil || pod.ClusterId != first.Sandbox.Snapshot.ClusterId || pod.Namespace != first.Sandbox.Snapshot.Namespace || pod.NamespaceUid != first.Sandbox.Snapshot.NamespaceUid || pod.PodName != "sandbox-"+id || !sandboxUUID(pod.PodUid) || seen[pod.PodUid] {
			return nil, kratoserrors.ServiceUnavailable("SANDBOX_METRICS_UNAVAILABLE", "invalid sandbox history scope")
		}
		seen[pod.PodUid] = true
		// first_recorded_at is not a Pod creation time and cannot trim history.
		if _, err := time.Parse(time.RFC3339Nano, pod.FirstRecordedAt); err != nil {
			return nil, kratoserrors.ServiceUnavailable("SANDBOX_METRICS_UNAVAILABLE", "invalid sandbox history time")
		}
		scopes = append(scopes, data.SandboxMetricScope{Cluster: pod.ClusterId, Namespace: pod.Namespace, Pod: pod.PodName, PodUID: pod.PodUid})
	}
	series, err := s.metrics.History(ctx, tenant, actor, id, first.Sandbox.Snapshot.ClusterId, scopes, start, end, step)
	if err != nil {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_METRICS_UNAVAILABLE", "sandbox historical metrics source unavailable").WithCause(err)
	}
	last, err := s.client.GetSandbox(ctx, tenant, actor, &res.GetSandboxRequest{TenantId: tenant, SandboxId: id})
	if err != nil {
		return nil, err
	}
	if last == nil || last.Sandbox == nil || last.Sandbox.Snapshot == nil || last.Sandbox.Id != id || last.Sandbox.TenantId != tenant || last.Sandbox.Version != first.Sandbox.Version || last.Sandbox.DesiredState != first.Sandbox.DesiredState || last.Sandbox.ObservedState != first.Sandbox.ObservedState || last.Sandbox.Snapshot.ClusterId != first.Sandbox.Snapshot.ClusterId || last.Sandbox.Snapshot.Namespace != first.Sandbox.Snapshot.Namespace || last.Sandbox.Snapshot.NamespaceUid != first.Sandbox.Snapshot.NamespaceUid || last.Sandbox.Snapshot.RuntimeClass != first.Sandbox.Snapshot.RuntimeClass || last.Sandbox.GetProviderRefs().GetPodUid() != first.Sandbox.GetProviderRefs().GetPodUid() {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_METRICS_UNAVAILABLE", "sandbox changed while reading metrics")
	}
	checked, err := client.ListSandboxPodHistory(ctx, tenant, actor, &res.ListSandboxPodHistoryRequest{TenantId: tenant, SandboxId: id})
	if err != nil {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_METRICS_UNAVAILABLE", "sandbox history recheck unavailable").WithCause(err)
	}
	if !sameSandboxPodHistory(history, checked) {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_METRICS_UNAVAILABLE", "sandbox history changed while reading metrics")
	}
	out := &view.SandboxMetricsResponse{Scope: "known_pods_only", Start: start.UTC().Format(time.RFC3339Nano), End: end.UTC().Format(time.RFC3339Nano), Step: step, Series: []*view.SandboxMetricSeries{}, HistoryCompleteBeforeMigration: history.HistoryCompleteBeforeMigration}
	for _, item := range series {
		r := &view.SandboxMetricSeries{Name: item.Name, Unit: item.Unit, Status: item.Status, Reason: item.Reason, Points: []*view.SandboxMetricPoint{}}
		for _, p := range item.Points {
			point := &view.SandboxMetricPoint{Timestamp: p.Timestamp.AsTime().UTC().Format(time.RFC3339Nano), Status: p.Status, Value: p.Value}
			r.Points = append(r.Points, point)
		}
		out.Series = append(out.Series, r)
	}
	return out, nil
}

func sameSandboxPodHistory(first, last *res.ListSandboxPodHistoryResponse) bool {
	if first == nil || last == nil || first.HistoryCompleteBeforeMigration != last.HistoryCompleteBeforeMigration || len(first.Pods) != len(last.Pods) {
		return false
	}
	byUID := make(map[string]*res.SandboxPodHistory, len(first.Pods))
	for _, pod := range first.Pods {
		if pod == nil || pod.PodUid == "" || byUID[pod.PodUid] != nil {
			return false
		}
		byUID[pod.PodUid] = pod
	}
	for _, pod := range last.Pods {
		if pod == nil || byUID[pod.PodUid] == nil || !proto.Equal(byUID[pod.PodUid], pod) {
			return false
		}
		delete(byUID, pod.PodUid)
	}
	return len(byUID) == 0
}
func (s *SandboxService) GetSandboxLatestMetrics(ctx context.Context, r *view.GetSandboxLatestMetricsRequest) (*view.SandboxMetricsResponse, error) {
	if r == nil || !sandboxUUID(r.SandboxId) {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid sandbox identity")
	}
	return s.sandboxMetrics(ctx, r.SandboxId, time.Time{}, time.Now().UTC().Truncate(time.Second), 0, true)
}
func sandboxMetricScope(raw *res.GetSandboxResponse, tenant, id string) (data.SandboxMetricScope, int64, error) {
	if raw == nil || raw.Sandbox == nil || raw.Sandbox.Snapshot == nil || raw.Sandbox.ProviderRefs == nil {
		return data.SandboxMetricScope{}, 0, kratoserrors.ServiceUnavailable("SANDBOX_METRICS_UNAVAILABLE", "sandbox metrics unavailable")
	}
	sb := raw.Sandbox
	if sb.Id != id || sb.TenantId != tenant || sb.Version < 1 || sb.DesiredState != "running" || sb.ObservedState != "running" || sb.Snapshot.Namespace != "tenant-"+tenant || !sandboxUUID(sb.Snapshot.NamespaceUid) || sb.ProviderRefs.PodName != "sandbox-"+id || !sandboxUUID(sb.ProviderRefs.PodUid) || !sandboxUUID(sb.Snapshot.ClusterId) || !sandboxKataRuntime(sb.Snapshot.RuntimeClass) {
		return data.SandboxMetricScope{}, 0, kratoserrors.ServiceUnavailable("SANDBOX_METRICS_UNAVAILABLE", "sandbox metrics unavailable")
	}
	return data.SandboxMetricScope{Cluster: sb.Snapshot.ClusterId, Namespace: sb.Snapshot.Namespace, Pod: sb.ProviderRefs.PodName, PodUID: sb.ProviderRefs.PodUid}, sb.Version, nil
}
func sandboxKataRuntime(runtimeClass string) bool {
	return runtimeClass == "kata" || runtimeClass == "sandbox-kata"
}
func (s *SandboxService) sandboxMetrics(ctx context.Context, id string, start, end time.Time, step uint32, latest bool) (*view.SandboxMetricsResponse, error) {
	path := data.SandboxMetricsPath
	if latest {
		path = data.SandboxMetricsLatestPath
	}
	tenant, actor, err := s.scope(ctx, path, "GET")
	if err != nil {
		return nil, err
	}
	if s.metrics == nil {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_METRICS_UNAVAILABLE", "sandbox metrics source unavailable")
	}
	first, err := s.client.GetSandbox(ctx, tenant, actor, &res.GetSandboxRequest{TenantId: tenant, SandboxId: id})
	if err != nil {
		return nil, err
	}
	scope, version, err := sandboxMetricScope(first, tenant, id)
	if err != nil {
		return nil, err
	}
	series, err := s.metrics.Latest(ctx, tenant, actor, id, scope)
	if err != nil {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_METRICS_UNAVAILABLE", "sandbox metrics source unavailable").WithCause(err)
	}
	last, err := s.client.GetSandbox(ctx, tenant, actor, &res.GetSandboxRequest{TenantId: tenant, SandboxId: id})
	if err != nil {
		return nil, err
	}
	after, afterVersion, err := sandboxMetricScope(last, tenant, id)
	if err != nil {
		return nil, err
	}
	if scope != after || version != afterVersion || first.Sandbox.Snapshot.RuntimeClass != last.Sandbox.Snapshot.RuntimeClass || first.Sandbox.Snapshot.NamespaceUid != last.Sandbox.Snapshot.NamespaceUid {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_METRICS_UNAVAILABLE", "sandbox changed while reading metrics")
	}
	out := &view.SandboxMetricsResponse{Scope: "current_pod_only", PodUid: scope.PodUID, Step: step, Series: []*view.SandboxMetricSeries{}}
	if !latest {
		out.Start = start.UTC().Format(time.RFC3339Nano)
		out.End = end.UTC().Format(time.RFC3339Nano)
	}
	for _, item := range series {
		r := &view.SandboxMetricSeries{Name: item.Name, Unit: item.Unit, Status: item.Status, Reason: item.Reason, Points: []*view.SandboxMetricPoint{}}
		for _, point := range item.Points {
			r.Points = append(r.Points, &view.SandboxMetricPoint{Timestamp: point.Timestamp.AsTime().UTC().Format(time.RFC3339Nano), Value: point.Value, Status: point.Status})
		}
		out.Series = append(out.Series, r)
	}
	return out, nil
}

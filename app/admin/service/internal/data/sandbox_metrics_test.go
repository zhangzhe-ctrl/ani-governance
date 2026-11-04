package data

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	pb "github.com/zhangzhe-ctrl/ani-observability-service/api/observability/monitoring/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const metricTenant = "22222222-2222-4222-8222-222222222222"
const metricSandbox = "33333333-3333-4333-8333-333333333333"
const metricCluster = "11111111-1111-4111-8111-111111111111"
const metricPod = "44444444-4444-4444-8444-444444444444"

type sandboxMetricsTestClient struct {
	latest       *pb.QuerySandboxLatestRequest
	rangeRequest *pb.QuerySandboxRangeRequest
	response     []*pb.SandboxMetricSeries
	err          error
}

func (c *sandboxMetricsTestClient) QuerySandboxLatest(_ context.Context, tenant, actor string, r *pb.QuerySandboxLatestRequest) (*pb.QuerySandboxLatestResponse, error) {
	if tenant != metricTenant || actor != "governance:user:9" {
		panic("untrusted identity")
	}
	c.latest = r
	if c.err != nil {
		return nil, c.err
	}
	return &pb.QuerySandboxLatestResponse{Series: c.response}, nil
}
func (c *sandboxMetricsTestClient) QuerySandboxRange(_ context.Context, tenant, actor string, r *pb.QuerySandboxRangeRequest) (*pb.QuerySandboxRangeResponse, error) {
	if tenant != metricTenant || actor != "governance:user:9" {
		panic("untrusted identity")
	}
	c.rangeRequest = r
	if c.err != nil {
		return nil, c.err
	}
	return &pb.QuerySandboxRangeResponse{Start: r.Start, End: r.End, StepSeconds: r.StepSeconds, Series: c.response}, nil
}
func metricResponse() []*pb.SandboxMetricSeries {
	zero := 0.0
	return []*pb.SandboxMetricSeries{
		{Name: "cpu", Unit: "cores", Status: "ok", Points: []*pb.SandboxMetricPoint{{Timestamp: timestamppb.Now(), Value: &zero, Status: "ok"}}},
		{Name: "network_receive", Unit: "bytes_per_second", Status: "unavailable"},
		{Name: "network_transmit", Unit: "bytes_per_second", Status: "unavailable"},
		{Name: "memory", Unit: "bytes", Status: "unavailable", Reason: "KATA_MEMORY_SOURCE_UNAVAILABLE"},
	}
}
func TestSandboxMetricsReaderPassesOwnerResolvedScopeAndMissingValues(t *testing.T) {
	client := &sandboxMetricsTestClient{response: metricResponse()}
	reader := NewSandboxMetricsReader(client)
	scope := SandboxMetricScope{Cluster: metricCluster, Namespace: "tenant-" + metricTenant, Pod: "sandbox-" + metricSandbox, PodUID: metricPod}
	series, err := reader.Latest(context.Background(), metricTenant, "governance:user:9", metricSandbox, scope)
	if err != nil || client.latest.Target.SandboxId != metricSandbox || client.latest.Target.PodUids[0] != metricPod || series[0].Points[0].Value == nil || *series[0].Points[0].Value != 0 || series[3].Reason != "KATA_MEMORY_SOURCE_UNAVAILABLE" {
		t.Fatalf("incorrect latest: %v %v", series, err)
	}
	end := time.Now().UTC().Truncate(time.Second)
	client.response[0].Points[0].Timestamp = timestamppb.New(end)
	_, err = reader.History(context.Background(), metricTenant, "governance:user:9", metricSandbox, metricCluster, []SandboxMetricScope{scope}, end.Add(-time.Minute), end, 60)
	if err != nil || client.rangeRequest.Target.ClusterId != metricCluster || len(client.rangeRequest.Target.PodUids) != 1 || client.rangeRequest.Target.PodUids[0] != metricPod {
		t.Fatalf("incorrect history scope: %v", err)
	}
}
func TestSandboxMetricsRejectsMalformedUpstream(t *testing.T) {
	for name, mutate := range map[string]func([]*pb.SandboxMetricSeries){
		"duplicate":      func(s []*pb.SandboxMetricSeries) { s[1].Name = "cpu" },
		"wrong unit":     func(s []*pb.SandboxMetricSeries) { s[0].Unit = "bytes" },
		"missing value":  func(s []*pb.SandboxMetricSeries) { s[0].Points[0].Value = nil },
		"nan":            func(s []*pb.SandboxMetricSeries) { v := math.NaN(); s[0].Points[0].Value = &v },
		"foreign status": func(s []*pb.SandboxMetricSeries) { s[0].Status = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			s := metricResponse()
			mutate(s)
			if validateSandboxSeries(s, true, time.Time{}, time.Time{}, 0, time.Now()) == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
}

func TestSandboxMetricsPreservesUpstreamErrors(t *testing.T) {
	upstream := errors.New("upstream failure")
	reader := NewSandboxMetricsReader(&sandboxMetricsTestClient{err: upstream})
	scope := SandboxMetricScope{Cluster: metricCluster, PodUID: metricPod}
	if _, err := reader.Latest(context.Background(), metricTenant, "governance:user:9", metricSandbox, scope); !errors.Is(err, upstream) {
		t.Fatalf("latest lost upstream error: %v", err)
	}
	end := time.Now().UTC().Truncate(time.Second)
	if _, err := reader.History(context.Background(), metricTenant, "governance:user:9", metricSandbox, metricCluster, []SandboxMetricScope{scope}, end.Add(-time.Minute), end, 60); !errors.Is(err, upstream) {
		t.Fatalf("history lost upstream error: %v", err)
	}
}

func TestSandboxMetricsRejectsInvalidTimeGrid(t *testing.T) {
	start := time.Unix(1700000000, 0).UTC()
	end := start.Add(2 * time.Minute)
	point := func(at time.Time) *pb.SandboxMetricPoint {
		v := 1.0
		return &pb.SandboxMetricPoint{Timestamp: timestamppb.New(at), Value: &v, Status: "ok"}
	}
	for _, tc := range []struct {
		name    string
		points  []*pb.SandboxMetricPoint
		wantErr bool
	}{
		{"valid", []*pb.SandboxMetricPoint{point(start), point(start.Add(time.Minute)), point(end)}, false},
		{"before", []*pb.SandboxMetricPoint{point(start.Add(-time.Minute))}, true},
		{"after", []*pb.SandboxMetricPoint{point(end.Add(time.Minute))}, true},
		{"off grid", []*pb.SandboxMetricPoint{point(start.Add(30 * time.Second))}, true},
		{"fractional", []*pb.SandboxMetricPoint{point(start.Add(time.Nanosecond))}, true},
		{"duplicate", []*pb.SandboxMetricPoint{point(start), point(start)}, true},
		{"out of order", []*pb.SandboxMetricPoint{point(end), point(start)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			series := metricResponse()
			series[0].Points = tc.points
			err := validateSandboxSeries(series, false, start, end, 60, time.Time{})
			if (err != nil) != tc.wantErr {
				t.Fatalf("wrong time validation result: %v", err)
			}
		})
	}
}

func TestSandboxLatestRejectsStaleAndFutureSamples(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	for _, tc := range []struct {
		name    string
		at      time.Time
		wantErr bool
	}{
		{"now", now, false}, {"old boundary", now.Add(-5 * time.Minute), false}, {"future boundary", now.Add(time.Minute), false},
		{"stale", now.Add(-5*time.Minute - time.Nanosecond), true}, {"future", now.Add(time.Minute + time.Nanosecond), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			series := metricResponse()
			series[0].Points[0].Timestamp = timestamppb.New(tc.at)
			err := validateSandboxSeries(series, true, time.Time{}, time.Time{}, 0, now)
			if (err != nil) != tc.wantErr {
				t.Fatalf("wrong freshness result: %v", err)
			}
		})
	}
}

type sandboxMetricsRPC struct {
	pb.MonitoringServiceClient
	seen metadata.MD
}

func (f *sandboxMetricsRPC) QuerySandboxLatest(ctx context.Context, _ *pb.QuerySandboxLatestRequest, _ ...grpc.CallOption) (*pb.QuerySandboxLatestResponse, error) {
	f.seen, _ = metadata.FromOutgoingContext(ctx)
	return &pb.QuerySandboxLatestResponse{}, nil
}
func TestSandboxMetricsRebuildsTrustedMetadata(t *testing.T) {
	rpc := &sandboxMetricsRPC{}
	client := &ObservabilityClient{client: rpc}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("x-ani-tenant-id", "forged", "x-ani-actor", "forged"))
	_, err := client.QuerySandboxLatest(ctx, metricTenant, "governance:user:9", &pb.QuerySandboxLatestRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if ids := rpc.seen.Get("x-ani-tenant-id"); len(ids) != 1 || ids[0] != metricTenant {
		t.Fatalf("wrong tenant metadata %v", ids)
	}
	if actors := rpc.seen.Get("x-ani-actor"); len(actors) != 1 || actors[0] != "governance:user:9" {
		t.Fatalf("wrong actor metadata %v", actors)
	}
	if ids := rpc.seen.Get("x-ani-request-id"); len(ids) != 1 || ids[0] == "" {
		t.Fatalf("missing request ID %v", ids)
	}
}

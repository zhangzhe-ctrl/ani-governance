package data

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	pb "github.com/zhangzhe-ctrl/ani-observability-service/api/observability/monitoring/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// SandboxMetricsClient is the trusted observability boundary; Resource remains
// the owner of sandbox identity and Pod history.
type SandboxMetricsClient interface {
	QuerySandboxLatest(context.Context, string, string, *pb.QuerySandboxLatestRequest) (*pb.QuerySandboxLatestResponse, error)
	QuerySandboxRange(context.Context, string, string, *pb.QuerySandboxRangeRequest) (*pb.QuerySandboxRangeResponse, error)
}

type SandboxMetricsReader struct{ client SandboxMetricsClient }
type SandboxMetricScope struct{ Cluster, Namespace, Pod, PodUID string }

func NewSandboxMetricsReader(client SandboxMetricsClient) *SandboxMetricsReader {
	if client == nil {
		return nil
	}
	return &SandboxMetricsReader{client: client}
}

func sandboxObservabilityCall[T any](c *ObservabilityClient, ctx context.Context, tenant, actor string, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	if c == nil || c.client == nil {
		return zero, status.Error(codes.Unavailable, "observability unavailable")
	}
	if !validResourceIdentity(tenant, actor) {
		return zero, status.Error(codes.Unauthenticated, "invalid observability caller")
	}
	call, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	call = metadata.NewOutgoingContext(call, metadata.Pairs("x-ani-tenant-id", tenant, "x-ani-actor", actor, "x-ani-request-id", uuid.NewString()))
	return fn(call)
}

func (c *ObservabilityClient) QuerySandboxLatest(ctx context.Context, tenant, actor string, req *pb.QuerySandboxLatestRequest) (*pb.QuerySandboxLatestResponse, error) {
	return sandboxObservabilityCall(c, ctx, tenant, actor, func(ctx context.Context) (*pb.QuerySandboxLatestResponse, error) {
		return c.client.QuerySandboxLatest(ctx, req)
	})
}

func (c *ObservabilityClient) QuerySandboxRange(ctx context.Context, tenant, actor string, req *pb.QuerySandboxRangeRequest) (*pb.QuerySandboxRangeResponse, error) {
	return sandboxObservabilityCall(c, ctx, tenant, actor, func(ctx context.Context) (*pb.QuerySandboxRangeResponse, error) {
		return c.client.QuerySandboxRange(ctx, req)
	})
}

func (m *SandboxMetricsReader) Latest(ctx context.Context, tenant, actor, sandbox string, scope SandboxMetricScope) ([]*pb.SandboxMetricSeries, error) {
	if m == nil || m.client == nil {
		return nil, errors.New("sandbox metrics unavailable")
	}
	r, err := m.client.QuerySandboxLatest(ctx, tenant, actor, &pb.QuerySandboxLatestRequest{Target: &pb.SandboxTarget{TenantId: tenant, SandboxId: sandbox, ClusterId: scope.Cluster, PodUids: []string{scope.PodUID}}})
	if err != nil {
		return nil, fmt.Errorf("sandbox latest metrics unavailable: %w", err)
	}
	if r == nil {
		return nil, errors.New("sandbox latest metrics unavailable")
	}
	if err := validateSandboxSeries(r.Series, true, time.Time{}, time.Time{}, 0, time.Now()); err != nil {
		return nil, err
	}
	return r.Series, nil
}

func (m *SandboxMetricsReader) History(ctx context.Context, tenant, actor, sandbox, cluster string, scopes []SandboxMetricScope, start, end time.Time, step uint32) ([]*pb.SandboxMetricSeries, error) {
	if m == nil || m.client == nil {
		return nil, errors.New("sandbox metrics unavailable")
	}
	uids := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		uids = append(uids, scope.PodUID)
	}
	r, err := m.client.QuerySandboxRange(ctx, tenant, actor, &pb.QuerySandboxRangeRequest{Target: &pb.SandboxTarget{TenantId: tenant, SandboxId: sandbox, ClusterId: cluster, PodUids: uids}, Start: timestamppb.New(start), End: timestamppb.New(end), StepSeconds: step})
	if err != nil {
		return nil, fmt.Errorf("sandbox historical metrics unavailable: %w", err)
	}
	if r == nil || r.Start == nil || r.End == nil || r.Start.CheckValid() != nil || r.End.CheckValid() != nil || !r.Start.AsTime().Equal(start) || !r.End.AsTime().Equal(end) || r.StepSeconds != step {
		return nil, errors.New("sandbox historical metrics unavailable")
	}
	if err := validateSandboxSeries(r.Series, false, start, end, step, time.Time{}); err != nil {
		return nil, err
	}
	return r.Series, nil
}

func validateSandboxSeries(series []*pb.SandboxMetricSeries, latest bool, start, end time.Time, step uint32, now time.Time) error {
	units := map[string]string{"cpu": "cores", "memory": "bytes", "network_receive": "bytes_per_second", "network_transmit": "bytes_per_second"}
	if len(series) != len(units) || (!latest && (step == 0 || start.Nanosecond() != 0 || end.Nanosecond() != 0 || !end.After(start))) {
		return errors.New("invalid sandbox metric series")
	}
	seen := map[string]bool{}
	for _, s := range series {
		if s == nil || units[s.Name] == "" || units[s.Name] != s.Unit || seen[s.Name] || (s.Status != "ok" && s.Status != "partial" && s.Status != "unavailable") || len(s.Points) > 1441 || (latest && len(s.Points) > 1) {
			return errors.New("invalid sandbox metric series")
		}
		seen[s.Name] = true
		var previous time.Time
		for _, point := range s.Points {
			if point == nil || point.Timestamp == nil || point.Timestamp.CheckValid() != nil || (point.Status != "ok" && point.Status != "unavailable" && point.Status != "ambiguous") || ((point.Status == "ok") != (point.Value != nil)) || (point.Value != nil && (math.IsNaN(*point.Value) || math.IsInf(*point.Value, 0) || *point.Value < 0)) {
				return errors.New("invalid sandbox metric point")
			}
			at := point.Timestamp.AsTime()
			if latest {
				if at.Before(now.Add(-5*time.Minute)) || at.After(now.Add(time.Minute)) {
					return errors.New("stale sandbox metric point")
				}
			} else if at.Nanosecond() != 0 || at.Before(start) || at.After(end) || at.Sub(start)%(time.Duration(step)*time.Second) != 0 || (!previous.IsZero() && !at.After(previous)) {
				return errors.New("invalid sandbox metric grid")
			}
			previous = at
		}
	}
	return nil
}

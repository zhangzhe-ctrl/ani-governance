package service

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	view "go-wind-admin/api/gen/go/sandbox/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type sandboxLogsClient interface {
	StreamSandboxLogs(context.Context, string, string, *res.StreamSandboxLogsRequest) (grpc.ServerStreamingClient[res.StreamSandboxLogsResponse], error)
}

type SandboxLogSession struct {
	TenantID, SandboxID string
	Stream              grpc.ServerStreamingClient[res.StreamSandboxLogsResponse]
}

func (s *SandboxService) OpenSandboxLogs(ctx context.Context, r *view.StreamSandboxLogsRequest) (*SandboxLogSession, error) {
	if r == nil || !sandboxUUID(r.SandboxId) || (r.SinceTime == "") != (r.PodUid == "") || r.SinceTime != "" && r.TailLines != nil {
		return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid log options")
	}
	var since *timestamppb.Timestamp
	if r.SinceTime != "" {
		parsed, err := time.Parse(time.RFC3339Nano, r.SinceTime)
		if err != nil || len(r.PodUid) > 128 || !utf8.ValidString(r.PodUid) || strings.ContainsFunc(r.PodUid, unicode.IsControl) {
			return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid log resume point")
		}
		since = timestamppb.New(parsed)
		if since.CheckValid() != nil {
			return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid log resume time")
		}
	}
	tail, follow := uint32(0), true
	if r.TailLines != nil {
		tail = *r.TailLines
		if tail == 0 || tail > 1000 {
			return nil, kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid log options")
		}
	} else if since == nil {
		tail = 100
	}
	if r.Follow != nil {
		follow = *r.Follow
	}
	tenant, actor, err := s.scope(ctx, data.SandboxLogsPath, "GET")
	if err != nil {
		return nil, err
	}
	client, ok := s.client.(sandboxLogsClient)
	if !ok {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "sandbox logs are not configured")
	}
	stream, err := client.StreamSandboxLogs(ctx, tenant, actor, &res.StreamSandboxLogsRequest{TenantId: tenant, SandboxId: r.SandboxId, TailLines: tail, Follow: follow, SinceTime: since, PodUid: r.PodUid})
	if err != nil {
		return nil, err
	}
	if stream == nil {
		return nil, kratoserrors.ServiceUnavailable("SANDBOX_UNAVAILABLE", "sandbox logs are unavailable")
	}
	return &SandboxLogSession{TenantID: tenant, SandboxID: r.SandboxId, Stream: stream}, nil
}

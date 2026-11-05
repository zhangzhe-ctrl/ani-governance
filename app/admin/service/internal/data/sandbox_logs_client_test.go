package data

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	pb "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type sandboxLogProbe struct {
	pb.UnimplementedSandboxServiceServer
	request  *pb.StreamSandboxLogsRequest
	caller   metadata.MD
	deadline time.Time
}

func (p *sandboxLogProbe) StreamSandboxLogs(r *pb.StreamSandboxLogsRequest, stream grpc.ServerStreamingServer[pb.StreamSandboxLogsResponse]) error {
	p.request = r
	p.caller, _ = metadata.FromIncomingContext(stream.Context())
	p.deadline, _ = stream.Context().Deadline()
	if err := stream.Send(&pb.StreamSandboxLogsResponse{Kind: "READY", PodUid: "pod-1"}); err != nil {
		return err
	}
	return stream.Send(&pb.StreamSandboxLogsResponse{Kind: "END", Reason: "EOF"})
}
func TestSandboxLogsClientPreservesStreamScopeAndDeadline(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	probe := new(sandboxLogProbe)
	pb.RegisterSandboxServiceServer(server, probe)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///sandbox-logs", grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	client := &InstanceClient{sandbox: pb.NewSandboxServiceClient(conn)}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	since := timestamppb.New(time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC))
	request := &pb.StreamSandboxLogsRequest{TenantId: tokenTestTenant, SandboxId: tokenTestSandbox, SinceTime: since, PodUid: "pod-1", Follow: true}
	stream, err := client.StreamSandboxLogs(ctx, tokenTestTenant, "governance:user:9", request)
	require.NoError(t, err)
	first, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "READY", first.Kind)
	end, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "END", end.Kind)
	require.True(t, proto.Equal(request, probe.request))
	require.Equal(t, []string{tokenTestTenant}, probe.caller.Get("x-ani-tenant-id"))
	require.Equal(t, []string{"governance:user:9"}, probe.caller.Get("x-ani-actor"))
	require.NotEmpty(t, probe.caller.Get("x-ani-request-id"))
	require.Greater(t, time.Until(probe.deadline), time.Minute)
	_, err = client.StreamSandboxLogs(ctx, "44444444-4444-4444-8444-444444444444", "governance:user:9", request)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

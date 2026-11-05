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
	"google.golang.org/protobuf/types/known/timestamppb"
)

type sandboxHistoryProbe struct {
	pb.UnimplementedSandboxServiceServer
	request *pb.ListOperationsRequest
	caller  metadata.MD
}

type sandboxEnvironmentProbe struct {
	pb.UnimplementedSandboxServiceServer
	request *pb.ListEnvironmentVariablesRequest
	caller  metadata.MD
}

type sandboxEventsProbe struct {
	pb.UnimplementedSandboxServiceServer
	request *pb.ListSandboxEventsRequest
	caller  metadata.MD
}

func (p *sandboxEventsProbe) ListSandboxEvents(ctx context.Context, request *pb.ListSandboxEventsRequest) (*pb.ListSandboxEventsResponse, error) {
	p.request = request
	p.caller, _ = metadata.FromIncomingContext(ctx)
	return &pb.ListSandboxEventsResponse{Total: 1, Items: []*pb.SandboxEvent{{Id: "event-1", OccurredAt: "2026-11-03T10:30:00Z", Type: "Normal", Reason: "Started", Message: "Container started"}}}, nil
}

func TestSandboxEventsClientForwardsTenantAndPagination(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	probe := new(sandboxEventsProbe)
	pb.RegisterSandboxServiceServer(server, probe)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///sandbox-test", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	client := &InstanceClient{sandbox: pb.NewSandboxServiceClient(conn)}
	request := &pb.ListSandboxEventsRequest{TenantId: tokenTestTenant, SandboxId: tokenTestSandbox, Page: 2, PageSize: 10}
	result, err := client.ListSandboxEvents(context.Background(), tokenTestTenant, "governance:user:9", request)
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Total)
	require.EqualValues(t, 2, probe.request.Page)
	require.EqualValues(t, 10, probe.request.PageSize)
	require.Equal(t, []string{tokenTestTenant}, probe.caller.Get("x-ani-tenant-id"))
	require.Equal(t, []string{"governance:user:9"}, probe.caller.Get("x-ani-actor"))
	require.NotEmpty(t, probe.caller.Get("x-ani-request-id"))
	_, err = client.ListSandboxEvents(context.Background(), "44444444-4444-4444-8444-444444444444", "governance:user:9", request)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func (p *sandboxEnvironmentProbe) ListEnvironmentVariables(ctx context.Context, request *pb.ListEnvironmentVariablesRequest) (*pb.ListEnvironmentVariablesResponse, error) {
	p.request = request
	p.caller, _ = metadata.FromIncomingContext(ctx)
	return &pb.ListEnvironmentVariablesResponse{Total: 1, Items: []*pb.SandboxEnvironmentVariable{{Name: "ANI_SANDBOX_POD_UID", Source: "downward_api", FieldRef: "metadata.uid"}}}, nil
}

func TestSandboxEnvironmentClientForwardsTenantAndPagination(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	probe := new(sandboxEnvironmentProbe)
	pb.RegisterSandboxServiceServer(server, probe)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///sandbox-test", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	client := &InstanceClient{sandbox: pb.NewSandboxServiceClient(conn)}
	request := &pb.ListEnvironmentVariablesRequest{TenantId: tokenTestTenant, SandboxId: tokenTestSandbox, Page: 2, PageSize: 10}
	result, err := client.ListSandboxEnvironment(context.Background(), tokenTestTenant, "governance:user:9", request)
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Total)
	require.EqualValues(t, 2, probe.request.Page)
	require.EqualValues(t, 10, probe.request.PageSize)
	require.Equal(t, []string{tokenTestTenant}, probe.caller.Get("x-ani-tenant-id"))
	require.Equal(t, []string{"governance:user:9"}, probe.caller.Get("x-ani-actor"))
	_, err = client.ListSandboxEnvironment(context.Background(), "44444444-4444-4444-8444-444444444444", "governance:user:9", request)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func (p *sandboxHistoryProbe) ListOperations(ctx context.Context, request *pb.ListOperationsRequest) (*pb.ListOperationsResponse, error) {
	p.request = request
	p.caller, _ = metadata.FromIncomingContext(ctx)
	return &pb.ListOperationsResponse{Total: 4, Items: []*pb.Operation{{Id: tokenTestID, SandboxId: request.SandboxId, Action: "code_run", CreatedAt: timestamppb.New(time.Unix(1000, 0))}}}, nil
}

func TestSandboxHistoryClientForwardsTenantAndPagination(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	probe := new(sandboxHistoryProbe)
	pb.RegisterSandboxServiceServer(server, probe)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///sandbox-test", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	client := &InstanceClient{sandbox: pb.NewSandboxServiceClient(conn)}
	request := &pb.ListOperationsRequest{TenantId: tokenTestTenant, SandboxId: tokenTestSandbox, Page: 2, PageSize: 10}
	result, err := client.ListSandboxOperations(context.Background(), tokenTestTenant, "governance:user:9", request)
	require.NoError(t, err)
	require.EqualValues(t, 4, result.Total)
	require.EqualValues(t, 2, probe.request.Page)
	require.EqualValues(t, 10, probe.request.PageSize)
	require.Equal(t, []string{tokenTestTenant}, probe.caller.Get("x-ani-tenant-id"))
	require.Equal(t, []string{"governance:user:9"}, probe.caller.Get("x-ani-actor"))
	require.NotEmpty(t, probe.caller.Get("x-ani-request-id"))
	_, err = client.ListSandboxOperations(context.Background(), "44444444-4444-4444-8444-444444444444", "governance:user:9", request)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

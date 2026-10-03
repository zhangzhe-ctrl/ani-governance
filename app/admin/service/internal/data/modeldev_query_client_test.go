package data

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	modeldevv1 "github.com/zhangzhe-ctrl/ani-modeldev-service/api/ani/modeldev/v1"
	trainingv1 "github.com/zhangzhe-ctrl/ani-modeldev-service/api/ani/training/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type modelDevQueryPeer struct {
	modeldevv1.UnimplementedModelDevQueryServiceServer
	scope ModelDevResolveScope
	reply *modeldevv1.AuthorizeArtifactDownloadResponse
	error error
}

func (p *modelDevQueryPeer) AuthorizeArtifactDownload(ctx context.Context, in *modeldevv1.AuthorizeArtifactDownloadRequest) (*modeldevv1.AuthorizeArtifactDownloadResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	for key, want := range map[string]string{"x-ani-tenant-id": p.scope.ResourceTenantID, "x-ani-actor": p.scope.Actor, "x-ani-data-scope": "tenant-all", "x-ani-authorized-method": modeldevv1.ModelDevQueryService_AuthorizeArtifactDownload_FullMethodName} {
		values := md.Get(key)
		if len(values) != 1 || values[0] != want {
			return nil, status.Error(codes.Internal, "delegation mismatch")
		}
	}
	if len(md.Get("x-secret-injected")) != 0 || len(md.Get("x-ani-request-id")) != 1 || !modelDevCanonicalUUID(md.Get("x-ani-request-id")[0]) {
		return nil, status.Error(codes.Internal, "inbound metadata leak")
	}
	if p.error != nil {
		return nil, p.error
	}
	return p.reply, nil
}

// This peer substitutes only the external ModelDev RPC response. It proves
// transport/delegation and response validation, not ModelDev business behavior.
func TestModelDevQueryClientRebuildsDelegationAndRejectsUnsafeDownloads(t *testing.T) {
	artifactID := uuid.NewString()
	base := &modeldevv1.AuthorizeArtifactDownloadResponse{Artifact: &modeldevv1.ArtifactView{
		ArtifactId: artifactID, ExecutionId: uuid.NewString(), Filename: "model.pt", Role: trainingv1.FileRole_FILE_ROLE_CHECKPOINT,
		SizeBytes: 4096, Sha256: strings.Repeat("a", 64), DeliveryState: modeldevv1.DeliveryState_DELIVERY_STATE_PUBLISHED, VerifiedAt: timestamppb.Now(),
	}, DownloadUrl: "https://storage.example.test/fixed-version?temporary=not-a-real-secret", ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}
	for _, test := range []struct {
		name   string
		change func(*modeldevv1.AuthorizeArtifactDownloadResponse)
		remote error
		want   codes.Code
	}{
		{name: "current scoped delegation", want: codes.OK},
		{name: "wrong artifact", change: func(r *modeldevv1.AuthorizeArtifactDownloadResponse) { r.Artifact.ArtifactId = uuid.NewString() }, want: codes.Unavailable},
		{name: "unpublished", change: func(r *modeldevv1.AuthorizeArtifactDownloadResponse) {
			r.Artifact.DeliveryState = modeldevv1.DeliveryState_DELIVERY_STATE_PENDING
		}, want: codes.Unavailable},
		{name: "plain HTTP", change: func(r *modeldevv1.AuthorizeArtifactDownloadResponse) {
			r.DownloadUrl = "http://storage.example.test/private"
		}, want: codes.Unavailable},
		{name: "user credentials", change: func(r *modeldevv1.AuthorizeArtifactDownloadResponse) {
			r.DownloadUrl = "https://user:private@storage.example.test/file"
		}, want: codes.Unavailable},
		{name: "expired", change: func(r *modeldevv1.AuthorizeArtifactDownloadResponse) {
			r.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
		}, want: codes.Unavailable},
		{name: "long-lived", change: func(r *modeldevv1.AuthorizeArtifactDownloadResponse) {
			r.ExpiresAt = timestamppb.New(time.Now().Add(time.Hour))
		}, want: codes.Unavailable},
		{name: "hidden response field", change: func(r *modeldevv1.AuthorizeArtifactDownloadResponse) {
			r.Artifact.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
		}, want: codes.Unavailable},
		{name: "private failure", remote: status.Error(codes.Internal, "https://private.example/secret?token=private"), want: codes.Unavailable},
		{name: "not found no metadata", remote: status.Error(codes.NotFound, "private execution tenant metadata"), want: codes.NotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			config, security := modelDevBoundaryTLS(t, []string{"ani-modeldev-service"}, []string{"ani-governance"})
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			config.Address = listener.Addr().String()
			scope := ModelDevResolveScope{ResourceTenantID: uuid.NewString(), Actor: "governance:user:73"}
			reply := proto.Clone(base).(*modeldevv1.AuthorizeArtifactDownloadResponse)
			if test.change != nil {
				test.change(reply)
			}
			server := grpc.NewServer(grpc.Creds(credentials.NewTLS(security)))
			modeldevv1.RegisterModelDevQueryServiceServer(server, &modelDevQueryPeer{scope: scope, reply: reply, error: test.remote})
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(server.Stop)
			client, closeClient, err := NewModelDevClient(config)
			require.NoError(t, err)
			t.Cleanup(closeClient)
			ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("x-ani-tenant-id", uuid.NewString(), "x-ani-actor", "governance:user:1", "x-ani-data-scope", "forged", "x-secret-injected", "private"))
			out, err := client.AuthorizeArtifactDownload(ctx, scope, artifactID)
			require.Equal(t, test.want, status.Code(err))
			if test.want == codes.OK {
				require.True(t, proto.Equal(base, out))
			} else {
				require.Nil(t, out)
				require.NotContains(t, err.Error(), "private")
				require.NotContains(t, err.Error(), "token=")
			}
		})
	}
}

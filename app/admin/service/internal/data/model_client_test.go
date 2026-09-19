package data

import (
	"context"
	"github.com/google/uuid"
	modelv1 "github.com/zhangzhe-ctrl/ani-model-service/api/model/v1"
	identityv1 "go-wind-admin/api/gen/go/identity/service/v1"
	"go-wind-admin/app/admin/service/internal/data/ent/api"
	"go-wind-admin/app/admin/service/internal/data/ent/planmodule"
	"go-wind-admin/app/admin/service/internal/data/enttest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"testing"
	"time"
)

type timedOutModelRPC struct{ modelv1.ModelServiceClient }

func (timedOutModelRPC) ListModels(context.Context, *modelv1.ListModelsRequest, ...grpc.CallOption) (*modelv1.ListModelsResponse, error) {
	return nil, status.Error(codes.DeadlineExceeded, "deadline")
}
func TestModelClientTransportOutage(t *testing.T) {
	for _, state := range []connectivity.State{connectivity.Ready, connectivity.Connecting, connectivity.TransientFailure} {
		c := &ModelClient{client: timedOutModelRPC{}, timeout: time.Second, connectionState: func() connectivity.State { return state }}
		_, err := c.ListModels(context.Background(), "11111111-1111-4111-8111-111111111111", 7, 100, "")
		want := codes.Unavailable
		if state == connectivity.Ready {
			want = codes.DeadlineExceeded
		}
		if status.Code(err) != want {
			t.Fatalf("state %v: got %v want %v", state, err, want)
		}
	}
}

type modelRPCProbe struct {
	modelv1.ModelServiceClient
	t    *testing.T
	wait bool
}

func (p modelRPCProbe) ListModels(ctx context.Context, in *modelv1.ListModelsRequest, _ ...grpc.CallOption) (*modelv1.ListModelsResponse, error) {
	md, _ := metadata.FromOutgoingContext(ctx)
	if len(md.Get("evil")) != 0 || len(md.Get("x-ani-tenant-id")) != 1 || md.Get("x-ani-tenant-id")[0] != in.TenantId || md.Get("x-ani-actor")[0] != "governance:user:7" {
		p.t.Fatalf("metadata not rebuilt: %v", md)
	}
	if _, err := uuid.Parse(md.Get("x-ani-request-id")[0]); err != nil {
		p.t.Fatal(err)
	}
	if _, ok := ctx.Deadline(); !ok {
		p.t.Fatal("missing bounded deadline")
	}
	if p.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &modelv1.ListModelsResponse{}, nil
}
func TestModelClientIdentityAndCancellation(t *testing.T) {
	c := &ModelClient{client: modelRPCProbe{t: t}, timeout: 20 * time.Millisecond}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("evil", "forwarded", "x-ani-tenant-id", "forged"))
	if _, err := c.ListModels(ctx, "11111111-1111-4111-8111-111111111111", 7, 100, ""); err != nil {
		t.Fatal(err)
	}
	c.client = modelRPCProbe{t: t, wait: true}
	if _, err := c.ListModels(ctx, "11111111-1111-4111-8111-111111111111", 7, 100, ""); err != context.DeadlineExceeded {
		t.Fatalf("timeout: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.ListModels(canceled, "11111111-1111-4111-8111-111111111111", 7, 100, ""); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
	if _, _, err := NewModelClient(ModelClientConfig{}); err == nil {
		t.Fatal("missing TLS configuration accepted")
	}
}
func TestResourceTenantUUIDPersistence(t *testing.T) {
	client := enttest.NewEntClientForTest(t)
	ctx := enttest.NewSystemViewerCtx(context.Background())
	a, err := client.Client().Tenant.Create().SetName("a").SetCode("a").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b, err := client.Client().Tenant.Create().SetName("b").SetCode("b").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if a.ResourceTenantID == b.ResourceTenantID {
		t.Fatal("duplicate resource identity")
	}
	if _, err = uuid.Parse(a.ResourceTenantID); err != nil {
		t.Fatal(err)
	}
	if _, err = client.Client().Tenant.UpdateOneID(a.ID).SetName("renamed").Save(ctx); err != nil {
		t.Fatal(err)
	}
	row, err := client.Client().Tenant.Get(ctx, a.ID)
	if err != nil || row.ResourceTenantID != a.ResourceTenantID {
		t.Fatalf("identity changed: %v %v", row, err)
	}
	repo := &TenantRepo{entClient: client}
	if err := repo.Update(ctx, &identityv1.UpdateTenantRequest{Id: a.ID, Data: &identityv1.Tenant{}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"resource_tenant_id"}}}); err == nil {
		t.Fatal("immutable identity mask accepted")
	}
	if got, err := repo.ResourceTenantID(ctx, a.ID); err != nil || got != a.ResourceTenantID {
		t.Fatalf("mapping: %q %v", got, err)
	}
	if _, err := repo.ResourceTenantID(ctx, 0); err == nil {
		t.Fatal("platform mapping accepted")
	}
	if _, err = client.Client().Tenant.Create().SetResourceTenantID(a.ResourceTenantID).Save(ctx); err == nil {
		t.Fatal("duplicate UUID inserted")
	}
	if mapProtoModuleToEnt(identityv1.Module_MODEL) != planmodule.ModuleModel || mapApiBusinessModuleToProto(api.BusinessModuleModel) != identityv1.Module_MODEL {
		t.Fatal("MODEL not recognized by tenant gate")
	}
}

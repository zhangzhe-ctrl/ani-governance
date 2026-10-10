package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	attachment "github.com/zhangzhe-ctrl/ani-accelerator-service/api/gen/go/accelerator/integration/v1"
	inferencev1 "github.com/zhangzhe-ctrl/ani-inference-service/api/inference/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/pkg/middleware/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// This endpoint double verifies the adapter's wire contract only. It does not
// stand in for Inference's business receipt/PG or the software integration gate.
type inferenceWirePeer struct {
	create *inferencev1.CreateInferenceServiceRequest
	delete *inferencev1.DeleteInferenceServiceRequest
	ack    *attachment.DurableOwnerAck
}

func (p *inferenceWirePeer) Create(_ context.Context, r *inferencev1.CreateInferenceServiceRequest) (*inferencev1.OperationResponse, error) {
	p.create = r
	return &inferencev1.OperationResponse{DurableOwnerAck: p.ack}, nil
}
func (p *inferenceWirePeer) Delete(_ context.Context, r *inferencev1.DeleteInferenceServiceRequest) (*inferencev1.OperationResponse, error) {
	p.delete = r
	return &inferencev1.OperationResponse{DurableOwnerAck: p.ack}, nil
}

func inferenceBusiness(t *testing.T) (*inferencev1.CreateInferenceServiceRequest, *QuotaDispatchCommand) {
	t.Helper()
	plan, _, _ := gpuVector(t)
	plan.Request.ContainerName = "kserve-container"
	digest, err := GpuPlanDigest(plan)
	require.NoError(t, err)
	plan.ResolutionDigest = digest
	g := plan.Request
	r := &inferencev1.CreateInferenceServiceRequest{Name: "wire", ModelVersionId: "30000000-0000-4000-8000-000000000001", Replicas: int32(g.Replicas), Resource: &inferencev1.ResourceSpec{Requests: map[string]string{"cpu": "1"}, Limits: map[string]string{"memory": "8Gi"}, Gpu: &inferencev1.GpuRequest{ClusterId: g.ClusterId, PoolId: g.PoolId, ProfileId: g.ProfileId, ProfileVersion: g.ProfileVersion, Replicas: g.Replicas, DevicesPerReplica: g.DevicesPerReplica, ContainerName: g.ContainerName}}, Engine: &inferencev1.EngineSpec{Type: "vllm", Image: "test/image@sha256:fixture", Command: []string{"server"}, Args: []string{"--fixture"}}}
	business, err := inferencev1.CanonicalBusinessPayload(r)
	require.NoError(t, err)
	businessDigest, err := inferencev1.BusinessPayloadDigest(r)
	require.NoError(t, err)
	canonical := GpuCanonical{SchemaVersion: 2, GpuRequest: plan.Request, GpuPlan: plan, BusinessPayload: business, BusinessPayloadDigest: businessDigest, MeteringVersion: GpuMeteringVersion, QuotaItems: []GpuQuotaItem{{QuotaCode: GpuSharedQuotaCode, Units: 12288}, {QuotaCode: "storage.bytes", Units: 1024}}}
	raw, err := json.Marshal(canonical)
	require.NoError(t, err)
	command := &QuotaDispatchCommand{OperationID: "20000000-0000-4000-8000-000000000001", ResourceID: "20000000-0000-4000-8000-000000000002", ResourceTenantID: "20000000-0000-4000-8000-000000000003", Actor: QuotaActor{Type: "user", ID: "7"}, Action: InferenceCreateAction, RequestHash: "hash", CanonicalRequest: raw, Charges: []QuotaChargeRef{{ChargeID: "20000000-0000-4000-8000-000000000004", QuotaCode: GpuSharedQuotaCode, ChargedUnits: 12288}, {ChargeID: "20000000-0000-4000-8000-000000000005", QuotaCode: "storage.bytes", ChargedUnits: 1024}}}
	return r, command
}

func TestInferenceAdapterPreservesOriginalBusinessPlanAndAllCharges(t *testing.T) {
	r, command := inferenceBusiness(t)
	peer := &inferenceWirePeer{ack: &attachment.DurableOwnerAck{OperationId: command.OperationID, ResourceId: command.ResourceID, Accepted: true}}
	binding := NewInferenceGpuBinding(peer, nil)
	raw, err := binding.Dispatch(context.Background(), command)
	require.NoError(t, err)
	require.NoError(t, ValidateDurableOwnerAck(command, raw))
	require.Equal(t, command.OperationID, peer.create.RequestId)
	require.Equal(t, command.ResourceID, peer.create.GpuOwnerAttachment.Ref.ResourceId)
	require.Len(t, peer.create.GpuOwnerAttachment.GpuCharges, 1)
	require.Len(t, peer.create.OriginalCharges, 2)
	require.True(t, proto.Equal(r.Engine, peer.create.Engine))
	require.Equal(t, r.Resource.Requests, peer.create.Resource.Requests)
	command.CreateOperationID = command.OperationID
	command.OperationID = "20000000-0000-4000-8000-000000000006"
	command.Action = InferenceDeleteAction
	peer.ack = &attachment.DurableOwnerAck{OperationId: command.OperationID, ResourceId: command.ResourceID, Accepted: true}
	_, err = binding.Dispatch(context.Background(), command)
	require.NoError(t, err)
	require.Equal(t, command.CreateOperationID, peer.delete.GpuOwnerAttachment.Ref.CreateOperationId)
	require.Equal(t, command.OperationID, peer.delete.GpuOwnerAttachment.DeleteOperationId)
	require.Zero(t, peer.delete.ExpectedGeneration, "owner receives deletion intent without caller guessing local generation")
	require.Len(t, peer.delete.OriginalCharges, 2)
}

func TestInferenceAdapterRejectsWrongDurableAckAndIncompleteVector(t *testing.T) {
	_, command := inferenceBusiness(t)
	for _, ack := range []*attachment.DurableOwnerAck{nil, {OperationId: command.OperationID, ResourceId: command.ResourceID, Accepted: false}, {OperationId: command.OperationID, ResourceId: "other", Accepted: true}, {OperationId: "other", ResourceId: command.ResourceID, Accepted: true}} {
		_, err := NewInferenceGpuBinding(&inferenceWirePeer{ack: ack}, nil).Dispatch(context.Background(), command)
		require.Equal(t, codes.FailedPrecondition, status.Code(err))
	}
	peer := &inferenceWirePeer{}
	command.Charges = command.Charges[:1]
	_, err := NewInferenceGpuBinding(peer, nil).Dispatch(context.Background(), command)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Nil(t, peer.create)
}

func TestInferencePublicBusinessRejectsAttachmentAndGPUBypass(t *testing.T) {
	r, _ := inferenceBusiness(t)
	binding := NewInferenceGpuBinding(nil, nil)
	_, err := binding.ValidateGpuBusiness(context.Background(), r)
	require.NoError(t, err)
	r.Resource.Limits["nvidia.com/gpu"] = "1"
	_, err = binding.ValidateGpuBusiness(context.Background(), r)
	require.Error(t, err)
	delete(r.Resource.Limits, "nvidia.com/gpu")
	r.GpuOwnerAttachment = &attachment.GpuOwnerCreateAttachment{}
	_, err = binding.CanonicalGpuBusiness(r)
	require.Error(t, err)
	require.Error(t, binding.AuthorizeGpu(context.Background(), &auth.Principal{Type: auth.SubjectUser, ID: 7, TenantID: 1}, InferenceCreateAction, ""))
}

func TestInferenceRequestHashMatchesSharedPublicContract(t *testing.T) {
	r, command := inferenceBusiness(t)
	business, err := inferencev1.CanonicalBusinessPayload(r)
	require.NoError(t, err)
	var businessObject map[string]any
	require.NoError(t, json.Unmarshal(business, &businessObject))
	gpuObject, err := gpuCanonicalMessage(inferenceGpuRequest(r.Resource.Gpu).ProtoReflect())
	require.NoError(t, err)
	govHash, err := gpuHash(map[string]any{"schema": "gov-gpu-create-v1", "tenant_id": command.ResourceTenantID, "actor_type": command.Actor.Type, "actor_id": command.Actor.ID, "owner": "ani-inference", "action": InferenceCreateAction, "gpu_request": gpuObject, "business_type": string(r.ProtoReflect().Descriptor().FullName()), "business": businessObject})
	require.NoError(t, err)
	ownerHash, err := inferencev1.ManagedCreateRequestHash(r, command.ResourceTenantID, command.Actor.Type, command.Actor.ID)
	require.NoError(t, err)
	require.Equal(t, govHash, ownerHash)
	govHash, err = gpuHash(map[string]any{"schema": "gov-gpu-delete-v1", "tenant_id": command.ResourceTenantID, "actor_type": command.Actor.Type, "actor_id": command.Actor.ID, "owner": "ani-inference", "action": InferenceDeleteAction, "resource_id": command.ResourceID, "create_operation_id": command.OperationID})
	require.NoError(t, err)
	ownerHash, err = inferencev1.ManagedDeleteRequestHash(command.ResourceTenantID, command.ResourceID, command.OperationID, command.Actor.Type, command.Actor.ID)
	require.NoError(t, err)
	require.Equal(t, govHash, ownerHash)
}

func TestInferenceMissingAuthoritativeModuleCannotEnableFromConfig(t *testing.T) {
	err := data.NewInferenceAuthorizationRepo(nil).Authorize(context.Background(), 1, 7, data.InferenceCreatePath)
	if !data.InferenceModuleRegistered() {
		require.ErrorIs(t, err, data.ErrInferenceAuthorizationDenied)
	} else {
		require.ErrorIs(t, err, data.ErrInferenceAuthorizationUnavailable)
	}
}

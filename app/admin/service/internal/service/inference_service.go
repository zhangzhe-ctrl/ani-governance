package service

import (
	"context"
	"encoding/json"
	"errors"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	acc "github.com/zhangzhe-ctrl/ani-accelerator-service/api/gen/go/accelerator/v1"
	inferencev1 "github.com/zhangzhe-ctrl/ani-inference-service/api/inference/v1"
	admin "go-wind-admin/api/gen/go/admin/service/v1"
	view "go-wind-admin/api/gen/go/inference/service/v1"
	quotapb "go-wind-admin/api/gen/go/quota/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/pkg/middleware/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Command actions are the owner's authoritative RPC methods; public callers
// cannot select an action or destination.
const InferenceCreateAction = inferencev1.InferenceServiceManager_CreateInferenceService_FullMethodName
const InferenceDeleteAction = inferencev1.InferenceServiceManager_DeleteInferenceService_FullMethodName

type InferenceOwnerClient interface {
	Create(context.Context, *inferencev1.CreateInferenceServiceRequest) (*inferencev1.OperationResponse, error)
	Delete(context.Context, *inferencev1.DeleteInferenceServiceRequest) (*inferencev1.OperationResponse, error)
}

type InferenceActionAuthorizer interface {
	Authorize(context.Context, uint32, uint32, string) error
}

type InferenceGpuBinding struct {
	client        InferenceOwnerClient
	authorization InferenceActionAuthorizer
}

func NewInferenceGpuBinding(client InferenceOwnerClient, authorization InferenceActionAuthorizer) *InferenceGpuBinding {
	return &InferenceGpuBinding{client: client, authorization: authorization}
}
func (*InferenceGpuBinding) OwnerService() string { return "ani-inference" }
func (*InferenceGpuBinding) CreateAction() string { return InferenceCreateAction }
func (*InferenceGpuBinding) DeleteAction() string { return InferenceDeleteAction }
func (*InferenceGpuBinding) Actions() []string {
	return []string{InferenceCreateAction, InferenceDeleteAction}
}
func (*InferenceGpuBinding) GpuQuotaCodes() []string {
	return []string{GpuPhysicalQuotaCode, GpuSharedQuotaCode}
}

func (b *InferenceGpuBinding) AuthorizeGpu(ctx context.Context, p *auth.Principal, action, resource string) error {
	if p == nil || p.Type != auth.SubjectUser || p.TenantID == 0 || p.ID == 0 || b == nil || b.authorization == nil {
		return kratoserrors.Forbidden("FORBIDDEN", "inference action forbidden")
	}
	path := data.InferenceCreatePath
	if action == InferenceDeleteAction {
		path = data.InferenceDeletePath
	} else if action != InferenceCreateAction {
		return kratoserrors.Forbidden("FORBIDDEN", "inference action forbidden")
	}
	err := b.authorization.Authorize(ctx, p.TenantID, p.ID, path)
	if errors.Is(err, data.ErrInferenceAuthorizationDenied) {
		return kratoserrors.Forbidden("FORBIDDEN", "inference action forbidden")
	}
	if errors.Is(err, data.ErrInferenceAuthorizationUnavailable) {
		return kratoserrors.ServiceUnavailable("INFERENCE_AUTHORIZATION_UNAVAILABLE", "inference authorization unavailable")
	}
	return err
}

// CanonicalGpuBusiness selects the shared public API canonicalization. Trusted
// attachments and immediate command IDs never enter the original business hash.
func (*InferenceGpuBinding) CanonicalGpuBusiness(message proto.Message) ([]byte, error) {
	request, ok := message.(*inferencev1.CreateInferenceServiceRequest)
	if !ok || request == nil || request.GetGpuOwnerAttachment() != nil || len(request.GetOriginalCharges()) != 0 || request.GetRequestId() != "" {
		return nil, quotapb.ErrorInvalidQuotaRequest("%s", "invalid inference user business")
	}
	return inferencev1.CanonicalBusinessPayload(request)
}

func (*InferenceGpuBinding) ValidateGpuBusiness(_ context.Context, message proto.Message) ([]data.QuotaOccupyItem, error) {
	r, ok := message.(*inferencev1.CreateInferenceServiceRequest)
	if !ok || r == nil || r.GetResource().GetGpu() == nil || r.GetGpuOwnerAttachment() != nil || len(r.GetOriginalCharges()) != 0 || r.GetRequestId() != "" {
		return nil, quotapb.ErrorInvalidQuotaRequest("%s", "invalid inference user business")
	}
	if err := inferencev1.ValidateBusinessPayload(r); err != nil {
		return nil, quotapb.ErrorInvalidQuotaRequest("%s", "invalid inference business or GPU topology")
	}
	// This slice adds the established GPU meter. Dispatch below preserves all
	// original mixed charges already present in a frozen business command.
	return nil, nil
}

func (b *InferenceGpuBinding) Dispatch(ctx context.Context, command *QuotaDispatchCommand) ([]byte, error) {
	if b == nil || b.client == nil || command == nil {
		return nil, status.Error(codes.Unavailable, "INFERENCE_ADAPTER_UNAVAILABLE")
	}
	createAttachment, deleteAttachment, err := BuildGpuOwnerAttachments("ani-inference", command)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, "INVALID_ORIGINAL_GPU_COMMAND")
	}
	frozen, err := DecodeGpuCanonical(command.CanonicalRequest)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, "INVALID_ORIGINAL_GPU_COMMAND")
	}
	charges := make([]*inferencev1.OriginalQuotaCharge, 0, len(command.Charges))
	for _, charge := range command.Charges {
		charges = append(charges, &inferencev1.OriginalQuotaCharge{ChargeId: charge.ChargeID, QuotaCode: charge.QuotaCode, OriginalUnits: charge.ChargedUnits})
	}
	var response *inferencev1.OperationResponse
	switch command.Action {
	case InferenceCreateAction:
		if createAttachment == nil || deleteAttachment != nil || command.CreateOperationID != "" {
			return nil, status.Error(codes.FailedPrecondition, "INVALID_INFERENCE_CREATE_COMMAND")
		}
		request, decodeErr := inferencev1.DecodeBusinessPayload(frozen.BusinessPayload)
		if decodeErr != nil {
			return nil, status.Error(codes.FailedPrecondition, "INVALID_INFERENCE_BUSINESS")
		}
		digest, digestErr := inferencev1.BusinessPayloadDigest(request)
		if digestErr != nil || digest != frozen.BusinessPayloadDigest {
			return nil, status.Error(codes.FailedPrecondition, "INVALID_INFERENCE_BUSINESS_DIGEST")
		}
		if _, validationErr := b.ValidateGpuBusiness(ctx, request); validationErr != nil {
			return nil, status.Error(codes.FailedPrecondition, "INVALID_INFERENCE_BUSINESS")
		}
		if !proto.Equal(inferenceGpuRequest(request.Resource.Gpu), frozen.GpuRequest) {
			return nil, status.Error(codes.FailedPrecondition, "INVALID_INFERENCE_GPU_REQUEST")
		}
		request.RequestId = command.OperationID
		request.GpuOwnerAttachment = createAttachment
		request.OriginalCharges = charges
		response, err = b.client.Create(ctx, request)
	case InferenceDeleteAction:
		if deleteAttachment == nil || createAttachment != nil || command.CreateOperationID == "" {
			return nil, status.Error(codes.FailedPrecondition, "INVALID_INFERENCE_DELETE_COMMAND")
		}
		response, err = b.client.Delete(ctx, &inferencev1.DeleteInferenceServiceRequest{RequestId: command.OperationID, ResourceId: command.ResourceID, GpuOwnerAttachment: deleteAttachment, OriginalCharges: charges})
	default:
		return nil, status.Error(codes.FailedPrecondition, "INVALID_INFERENCE_ACTION")
	}
	if err != nil {
		return nil, err
	}
	if response == nil || response.DurableOwnerAck == nil || len(response.DurableOwnerAck.ProtoReflect().GetUnknown()) != 0 || !response.DurableOwnerAck.Accepted || response.DurableOwnerAck.OperationId != command.OperationID || response.DurableOwnerAck.ResourceId != command.ResourceID {
		return nil, status.Error(codes.FailedPrecondition, "INVALID_DURABLE_OWNER_ACK")
	}
	ack := struct {
		OperationID string `json:"operation_id"`
		ResourceID  string `json:"resource_id"`
		Accepted    bool   `json:"accepted"`
	}{command.OperationID, command.ResourceID, true}
	raw, err := json.Marshal(ack)
	if err != nil {
		return nil, err
	}
	if err = ValidateDurableOwnerAck(command, raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func inferenceGpuRequest(g *inferencev1.GpuRequest) *acc.GpuRequest {
	if g == nil {
		return nil
	}
	return &acc.GpuRequest{ClusterId: g.ClusterId, PoolId: g.PoolId, ProfileId: g.ProfileId, ProfileVersion: g.ProfileVersion, Replicas: g.Replicas, DevicesPerReplica: g.DevicesPerReplica, ContainerName: g.ContainerName}
}

type InferenceService struct {
	admin.UnimplementedInferenceServiceServer
	acceptance *GpuAcceptance
}

func NewInferenceService(a *GpuAcceptance) *InferenceService { return &InferenceService{acceptance: a} }

func (s *InferenceService) CreateInference(ctx context.Context, in *view.CreateInferenceRequest) (*view.InferenceAcceptance, error) {
	if in == nil || in.Data == nil {
		return nil, quotapb.ErrorInvalidQuotaRequest("%s", "inference data is required")
	}
	if s == nil || s.acceptance == nil {
		return nil, quotapb.ErrorQuotaAdapterUnavailable("%s", "inference unavailable")
	}
	d := in.Data
	request := &inferencev1.CreateInferenceServiceRequest{Name: d.Name, ModelVersionId: d.ModelVersionId, Resource: d.Resource, Replicas: d.Replicas, Runtime: d.Runtime, ModelArtifact: d.ModelArtifact, Engine: d.Engine, ServedModelName: d.ServedModelName}
	result, err := s.acceptance.AcceptGpuCreate(ctx, d.IdempotencyKey, inferenceGpuRequest(d.GetResource().GetGpu()), request)
	if err != nil {
		return nil, err
	}
	return &view.InferenceAcceptance{OperationId: result.OperationID, ResourceId: result.ResourceID, Replayed: result.Replayed, Result: "ACCEPTED", DispatchOperationId: result.OperationID}, nil
}

func (s *InferenceService) DeleteInference(ctx context.Context, in *view.DeleteInferenceRequest) (*view.InferenceAcceptance, error) {
	if in == nil || in.Data == nil {
		return nil, quotapb.ErrorInvalidQuotaRequest("%s", "inference data is required")
	}
	if s == nil || s.acceptance == nil {
		return nil, quotapb.ErrorQuotaAdapterUnavailable("%s", "inference unavailable")
	}
	result, err := s.acceptance.AcceptGpuDelete(ctx, in.Data.IdempotencyKey, in.Data.ResourceId)
	if err != nil {
		return nil, err
	}
	return &view.InferenceAcceptance{OperationId: result.DeleteOperationId, ResourceId: result.ResourceId, Result: result.Result, DispatchOperationId: result.DispatchOperationId}, nil
}

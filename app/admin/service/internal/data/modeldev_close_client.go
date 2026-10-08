package data

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	modeldevv1 "github.com/zhangzhe-ctrl/ani-modeldev-service/api/ani/modeldev/v1"
	trainingv1 "github.com/zhangzhe-ctrl/ani-modeldev-service/api/ani/training/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ModelDevCloseReceipt records the owner's durable creation fence, which is
// independent of Governance's source intent generation.
type ModelDevCloseReceipt struct {
	OperationID       string `json:"operation_id"`
	ExecutionID       string `json:"execution_id"`
	ExecutionSpecHash string `json:"execution_spec_hash"`
	CloseGeneration   uint64 `json:"close_generation"`
	CloseState        string `json:"close_state"`
	Replayed          bool   `json:"replayed"`
}

func (c *ModelDevClient) ApplyCloseIntent(ctx context.Context, intent ModelDevStopIntent) (ModelDevCloseReceipt, error) {
	if err := ctx.Err(); err != nil {
		return ModelDevCloseReceipt{}, err
	}
	request, err := modelDevCloseRequest(intent)
	if contextErr := ctx.Err(); contextErr != nil {
		return ModelDevCloseReceipt{}, contextErr
	}
	if err != nil {
		return ModelDevCloseReceipt{}, err
	}
	if c == nil || c.connection == nil || c.timeout <= 0 {
		return ModelDevCloseReceipt{}, &ModelDevDeliveryFailure{Code: "OWNER_UNAVAILABLE"}
	}
	callContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	requestID := uuid.NewString()
	callContext = metadata.NewOutgoingContext(callContext, metadata.Pairs(
		"x-ani-tenant-id", intent.ResourceTenantID, "x-ani-actor", intent.RequestedActor, "x-ani-request-id", requestID,
	))
	response, err := modeldevv1.NewModelDevCommandServiceClient(c.connection).ApplyCloseIntent(callContext, request, grpc.WaitForReady(true))
	if contextErr := ctx.Err(); contextErr != nil {
		return ModelDevCloseReceipt{}, contextErr
	}
	if callContext.Err() != nil {
		return ModelDevCloseReceipt{}, &ModelDevDeliveryFailure{Code: "OWNER_UNAVAILABLE"}
	}
	if err != nil {
		return ModelDevCloseReceipt{}, modelDevDispatchError(err, requestID)
	}
	if response == nil || response.Identity == nil || !response.DurablyRecorded ||
		len(response.ProtoReflect().GetUnknown()) != 0 || len(response.Identity.ProtoReflect().GetUnknown()) != 0 {
		return ModelDevCloseReceipt{}, &ModelDevDeliveryFailure{Code: "INVALID_ACK"}
	}
	receipt := ModelDevCloseReceipt{
		OperationID: response.Identity.OperationId, ExecutionID: response.Identity.ExecutionId, ExecutionSpecHash: response.Identity.ExecutionSpecHash,
		CloseGeneration: response.CloseGeneration, CloseState: strings.TrimPrefix(response.CloseState.String(), "CLOSE_STATE_"), Replayed: response.Replayed,
	}
	validationErr := validateModelDevCloseReceipt(receipt, intent)
	if contextErr := ctx.Err(); contextErr != nil {
		return ModelDevCloseReceipt{}, contextErr
	}
	if validationErr != nil {
		return ModelDevCloseReceipt{}, &ModelDevDeliveryFailure{Code: "INVALID_ACK"}
	}
	return receipt, nil
}

func modelDevCloseRequest(intent ModelDevStopIntent) (*modeldevv1.ApplyCloseIntentRequest, error) {
	requestedAt := timestamppb.New(intent.RequestedAt)
	if intent.TenantID == 0 || !modelDevCanonicalUUID(intent.ResourceTenantID) ||
		!modelDevCanonicalUUID(intent.OperationID) || !modelDevCanonicalUUID(intent.ExecutionID) || !modelDevDigest(intent.ExecutionSpecHash) ||
		intent.Generation == 0 || !validModelDevClientActor(intent.RequestedActor) || !modelDevValidTimestamp(requestedAt) || intent.RequestedAt.Nanosecond()%1000 != 0 {
		return nil, &ModelDevDeliveryFailure{Code: "INVALID_COMMAND", Permanent: true}
	}
	return &modeldevv1.ApplyCloseIntentRequest{
		Identity:         &trainingv1.ExecutionIdentity{OperationId: intent.OperationID, ExecutionId: intent.ExecutionID, ExecutionSpecHash: intent.ExecutionSpecHash},
		ResourceTenantId: intent.ResourceTenantID, IntentGeneration: intent.Generation, Reason: modeldevv1.CloseReason_CLOSE_REASON_USER_STOP,
		RequestedAt: requestedAt, RequestedActorId: intent.RequestedActor,
	}, nil
}

func validateModelDevCloseReceipt(receipt ModelDevCloseReceipt, intent ModelDevStopIntent) error {
	if _, err := modelDevCloseRequest(intent); err != nil ||
		receipt.OperationID != intent.OperationID || receipt.ExecutionID != intent.ExecutionID || receipt.ExecutionSpecHash != intent.ExecutionSpecHash || receipt.CloseGeneration == 0 {
		return ErrModelDevInvalidReceipt
	}
	switch receipt.CloseState {
	case "CLOSING", "CLOSED", "NEEDS_REVIEW":
	default:
		return ErrModelDevInvalidReceipt
	}
	encoded, err := json.Marshal(receipt)
	if err != nil || len(encoded) > 4096 {
		return ErrModelDevInvalidReceipt
	}
	return nil
}

package data

import (
	"context"
	"errors"
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
	return ModelDevCloseReceipt{}, errors.New("modeldev close client not implemented")
}

func validateModelDevCloseReceipt(receipt ModelDevCloseReceipt, intent ModelDevStopIntent) error {
	return errors.New("modeldev close receipt validation not implemented")
}

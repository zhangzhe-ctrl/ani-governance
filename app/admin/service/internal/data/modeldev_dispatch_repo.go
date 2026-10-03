package data

import (
	"context"
	"time"
)

type ModelDevDeliveryClaim struct {
	Acceptance *ModelDevAcceptance
	LeaseOwner string
	LeaseGeneration int64
	AttemptCount int64
}

func (r *ModelDevAcceptanceRepo) ClaimDelivery(context.Context, string, time.Duration) (*ModelDevDeliveryClaim, error) {
	return nil, errModelDevDeliveryNotImplemented
}

func (r *ModelDevAcceptanceRepo) AckDelivery(context.Context, *ModelDevDeliveryClaim, ModelDevOwnerReceipt) (bool, error) {
	return false, errModelDevDeliveryNotImplemented
}

func (r *ModelDevAcceptanceRepo) DeferDelivery(context.Context, *ModelDevDeliveryClaim, ModelDevDeliveryFailure) (bool, error) {
	return false, errModelDevDeliveryNotImplemented
}

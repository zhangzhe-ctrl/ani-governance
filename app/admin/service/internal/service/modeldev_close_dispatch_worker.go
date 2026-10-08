package service

import (
	"context"
	"errors"
	"time"

	"go-wind-admin/app/admin/service/internal/data"
)

// Reuses the existing worker lifecycle; persisted close commands are delivered
// before new creates. Failure to read the close queue also postpones creation.
func (w *ModelDevDispatchWorker) deliverNextClose(ctx context.Context) (worked, queueAvailable bool) {
	call, cancel := context.WithTimeout(ctx, 3*time.Second)
	claim, err := w.repository.ClaimCloseDelivery(call, w.workerID, 15*time.Second)
	cancel()
	if ctx.Err() != nil {
		return false, false
	}
	if err != nil {
		w.warn("modeldev close claim unavailable")
		return false, false
	}
	if claim == nil {
		return false, true
	}
	if claim.Intent == nil {
		w.warn("modeldev close original unavailable")
		return true, false
	}
	call, cancel = context.WithTimeout(ctx, 3*time.Second)
	receipt, deliveryErr := w.client.ApplyCloseIntent(call, *claim.Intent)
	cancel()
	if ctx.Err() != nil {
		return true, false
	}
	write, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var written bool
	if deliveryErr == nil {
		written, err = w.repository.AckCloseDelivery(write, claim, receipt)
	} else {
		failure := data.ModelDevDeliveryFailure{Code: "OWNER_UNAVAILABLE"}
		var classified *data.ModelDevDeliveryFailure
		if errors.As(deliveryErr, &classified) && classified != nil {
			failure = *classified
		}
		written, err = w.repository.DeferCloseDelivery(write, claim, failure)
	}
	if ctx.Err() == nil {
		if err != nil {
			w.warn("modeldev close delivery writeback unavailable")
		} else if !written {
			w.warn("modeldev close delivery writeback ignored after lease change")
		}
	}
	return true, true
}

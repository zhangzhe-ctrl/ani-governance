package data

import (
	"errors"

	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01"
)

var ErrModelDevInvalidReceipt = errors.New("invalid modeldev owner receipt")

var errModelDevDeliveryNotImplemented = errors.New("modeldev delivery storage not implemented")

// ModelDevOwnerReceipt is one validated, committed owner observation. Revision
// and all four states belong to the same response; they are never fetched apart.
type ModelDevOwnerReceipt struct {
	OperationID string
	ExecutionID string
	ExecutionSpecHash string
	ComputeState string
	DeliveryState string
	ResourceState string
	CloseState string
	Revision uint64
	Replayed bool
}

// ModelDevDeliveryFailure keeps only a finite safe reason. Remote messages and
// credentials never become a persisted diagnostic or worker log message.
type ModelDevDeliveryFailure struct {
	Code string
	Permanent bool
}

func (f *ModelDevDeliveryFailure) Error() string {
	if f != nil {
		switch f.Code {
		case "INVALID_COMMAND":
			return "modeldev delivery command invalid"
		case "COMMAND_CONFLICT":
			return "modeldev delivery command conflict"
		case "OWNER_UNAVAILABLE":
			return "modeldev delivery owner unavailable"
		case "INVALID_ACK":
			return "modeldev delivery receipt invalid"
		}
	}
	return "modeldev delivery unavailable"
}

func validateModelDevOwnerReceipt(ModelDevOwnerReceipt, cpup01.AdmissionEnvelope) error {
	return errModelDevDeliveryNotImplemented
}

// Envelope preserves the accepted command, including its original time and
// deadline. Delivery does not resolve current defaults or renew an old command.
func (a *ModelDevAcceptance) Envelope() cpup01.AdmissionEnvelope {
	return cpup01.AdmissionEnvelope{
		TenantID: a.Scope.ResourceTenantID, Actor: a.Scope.Actor,
		OperationID: a.OperationID, ExecutionID: a.ExecutionID,
		Intent: a.Intent, IntentHash: a.IntentHash,
		Snapshot: a.Snapshot, SpecHash: a.ExecutionSpecHash, AcceptedAt: a.AcceptedAt,
	}
}

package data

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"

	modeldevv1 "github.com/zhangzhe-ctrl/ani-modeldev-service/api/ani/modeldev/v1"
	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01"
)

var ErrModelDevInvalidReceipt = errors.New("invalid modeldev owner receipt")

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

func (r ModelDevOwnerReceipt) Validate(envelope cpup01.AdmissionEnvelope) error {
	return validateModelDevOwnerReceipt(r, envelope)
}

func validateModelDevOwnerReceipt(r ModelDevOwnerReceipt, envelope cpup01.AdmissionEnvelope) error {
	if _, _, err := envelope.CanonicalPayloads(); err != nil ||
		r.OperationID != envelope.OperationID || r.ExecutionID != envelope.ExecutionID || r.ExecutionSpecHash != envelope.SpecHash ||
		r.Revision == 0 || r.ResourceState != "NOT_APPLICABLE" ||
		modeldevv1.ComputeState_value["COMPUTE_STATE_"+r.ComputeState] == 0 ||
		modeldevv1.DeliveryState_value["DELIVERY_STATE_"+r.DeliveryState] == 0 ||
		modeldevv1.CloseState_value["CLOSE_STATE_"+r.CloseState] == 0 {
		return ErrModelDevInvalidReceipt
	}
	return nil
}

const modelDevOwnerReceiptSchema = "ani.governance.modeldev-owner-receipt.v1"

// The private storage envelope keeps the complete owner observation together.
// A decimal string retains the full uint64 revision across JSON/SQL consumers.
type modelDevReceiptDocument struct {
	Schema string `json:"schema"`
	Identity struct {
		OperationID string `json:"operation_id"`
		ExecutionID string `json:"execution_id"`
		ExecutionSpecHash string `json:"execution_spec_hash"`
	} `json:"identity"`
	States struct {
		ComputeState string `json:"compute_state"`
		DeliveryState string `json:"delivery_state"`
		ResourceState string `json:"resource_state"`
		CloseState string `json:"close_state"`
	} `json:"states"`
	Revision string `json:"revision"`
	Replayed bool `json:"replayed"`
}

func encodeModelDevOwnerReceipt(r ModelDevOwnerReceipt, envelope cpup01.AdmissionEnvelope) ([]byte, error) {
	if err := r.Validate(envelope); err != nil {
		return nil, err
	}
	var doc modelDevReceiptDocument
	doc.Schema = modelDevOwnerReceiptSchema
	doc.Identity.OperationID, doc.Identity.ExecutionID, doc.Identity.ExecutionSpecHash = r.OperationID, r.ExecutionID, r.ExecutionSpecHash
	doc.States.ComputeState, doc.States.DeliveryState = r.ComputeState, r.DeliveryState
	doc.States.ResourceState, doc.States.CloseState = r.ResourceState, r.CloseState
	doc.Revision, doc.Replayed = strconv.FormatUint(r.Revision, 10), r.Replayed
	return json.Marshal(doc)
}

func decodeModelDevOwnerReceipt(raw []byte, envelope cpup01.AdmissionEnvelope) (*ModelDevOwnerReceipt, error) {
	if len(raw) == 0 || len(raw) > 4096 {
		return nil, ErrModelDevInvalidReceipt
	}
	var doc modelDevReceiptDocument
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&doc) != nil || decoder.Decode(new(any)) != io.EOF || doc.Schema != modelDevOwnerReceiptSchema {
		return nil, ErrModelDevInvalidReceipt
	}
	revision, err := strconv.ParseUint(doc.Revision, 10, 64)
	if err != nil {
		return nil, ErrModelDevInvalidReceipt
	}
	r := ModelDevOwnerReceipt{
		OperationID: doc.Identity.OperationID, ExecutionID: doc.Identity.ExecutionID, ExecutionSpecHash: doc.Identity.ExecutionSpecHash,
		ComputeState: doc.States.ComputeState, DeliveryState: doc.States.DeliveryState,
		ResourceState: doc.States.ResourceState, CloseState: doc.States.CloseState,
		Revision: revision, Replayed: doc.Replayed,
	}
	canonical, err := encodeModelDevOwnerReceipt(r, envelope)
	// Byte equality also rejects duplicate/omitted members, null and alternate
	// decimal spellings; no corrupted observation is projected to a caller.
	if err != nil || !bytes.Equal(canonical, raw) {
		return nil, ErrModelDevInvalidReceipt
	}
	return &r, nil
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

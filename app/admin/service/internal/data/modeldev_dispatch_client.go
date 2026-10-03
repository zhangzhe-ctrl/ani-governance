package data

import (
	"context"
	"errors"

	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01"
)

// AcceptExecution delivers the persisted Governance original to its owner.
// RED stub: transport delivery and strict receipt validation are not implemented.
func (c *ModelDevClient) AcceptExecution(ctx context.Context, envelope cpup01.AdmissionEnvelope) (ModelDevOwnerReceipt, error) {
	return ModelDevOwnerReceipt{}, errors.New("modeldev delivery client not implemented")
}

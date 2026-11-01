package data

// This finite maintenance port does not participate in the publisher or HTTP
// composition roots. It can only deliver the four originals rejected by c99.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/modeldevacceptance"
	"go-wind-admin/app/admin/service/internal/data/ent/tenant"
	appViewer "go-wind-admin/pkg/entgo/viewer"
)

const OriginalCommandMaintenanceSource = "6c17f0103994620f49c8f6f10c09f414169c6b7d"

type originalCommandIdentity struct{ Operation, Execution, IntentSHA, SpecSHA string }

var originalCommands = map[uint32]originalCommandIdentity{
	55: {"13bd316b-0b85-4c8e-a005-698eb9ffdd5e", "27cf73a3-4107-4c07-86f7-862ce9fb413d", "451f319fadcf72f4be5641971f8c930d7fedab27c4af70f529e801e8844cf0ce", "cb9831ca80e7df95cf1ca3746db4ff53b623b51671efcf34230275235e0c1690"},
	57: {"cd2ee5e3-e4fb-4515-99f8-33b3da892a8f", "c9b470e0-d9cb-4989-a7f2-d1fbfbe72ab9", "fda56fe14be954c582addb294805dc0c0afe639c085defdd45aa4428dcc621f0", "9883ac49d909b2809e78002db7d47d564caa88f84bd29ba14d6248a34e8bb096"},
	59: {"ff5fc1f4-d9a8-4c6a-8c47-5b1d4ef06f05", "91e351e7-102e-4807-b0d2-9236c1426da9", "732ea6ba966201699f3ad707f50dbb9151bcf568c23d856beeb6902f3166eb6f", "e75a332cef7521f278df8fc78df0b3f869d8499f96d188fd3b8a915fe02ec829"},
	60: {"25b88cf7-bccb-4fac-8c9a-6b3ff91477ee", "f148135c-640f-47ed-b9b1-4b039d4709d1", "d0f78b8d1ef87c029313b06b0c0d193bddb3b968198744e05d37ed79d63e47c7", "85fa8793e5ff44c8c611316129f344a36124e027f03cc71bf797d89fa1d40a45"},
}

type OriginalCommandTarget struct {
	ID                                                                   uint32
	OperationID, ExecutionID, IntentSHA256, SpecSHA256, RowSHA256, Actor string
	AcceptedAt                                                           time.Time
	DispatchState, CloseDispatchState                                    string
	RetryBlocked                                                         bool
	StopGeneration                                                       int64
	CloseReceipt                                                         *ModelDevCloseReceipt `json:",omitempty"`
	Blockers                                                             []string
}
type OriginalCommandPlan struct {
	Schema     string
	Targets    []OriginalCommandTarget
	CodecAudit *OriginalCodecAudit `json:",omitempty"`
}

// OriginalCommandProof is an explicit operator trust boundary. The CLI requires
// the exact root-approved proof hash and verifies sourceproof bytes separately.
// No boolean can substitute for the fresh original HTTP/identity evidence.
type OriginalCommandProof struct {
	Schema, PlanSHA256, SourceProofSHA256, ConfigurationSHA256, GovernanceSource string
	ObservedAt                                                                   time.Time
	Entry, Context, APIServer, ClusterID, KubeSystemUID                          string
	HTTPMethod, HTTPPath, HTTPReason, HTTPResponseSHA256                         string
	HTTPStatus                                                                   int
	StopActor                                                                    string
}

func (p OriginalCommandProof) Validate(planSHA string) error {
	if p.Schema != "ani.modeldev.original-command-retirement-proof.v1" || p.PlanSHA256 != planSHA || !modelDevDigest(planSHA) || !modelDevDigest(p.SourceProofSHA256) || !modelDevDigest(p.ConfigurationSHA256) || !modelDevDigest(p.HTTPResponseSHA256) || p.GovernanceSource != OriginalCommandMaintenanceSource || p.ObservedAt.IsZero() || time.Since(p.ObservedAt) < 0 || time.Since(p.ObservedAt) > 2*time.Minute || p.Entry != "kubercloud@100.64.128.12" || p.Context != "kubernetes-admin@ani-kcn-full" || p.APIServer != "https://lb.kubesphere.local:6443" || p.ClusterID != "f39bf6e2-f3ed-4191-ada3-caeebf86c220" || p.KubeSystemUID != "3eeb5379-8748-4bf8-8f6d-d7a48ce3bc24" || p.HTTPMethod != "POST" || p.HTTPPath != "/api/v1/modeldev/executions" || p.HTTPStatus != 503 || p.HTTPReason != "MODELDEV_CREATE_MAINTENANCE" || !validModelDevClientActor(p.StopActor) {
		return errors.New("ORIGINAL_COMMAND_FRESH_ROOT_PROOF_REQUIRED")
	}
	return nil
}
func OriginalCommandPlanSHA(p OriginalCommandPlan) string {
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func originalCommandRowSHA(row *ent.ModelDevAcceptance) string {
	b, _ := json.Marshal(row)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func originalCommandIdentityValid(row *ent.ModelDevAcceptance) bool {
	w, ok := originalCommands[row.ID]
	return ok && row.TenantID != nil && *row.TenantID == 2 && row.ResourceTenantID == "64c5c35a-f554-432c-b4bc-7b4f36f08420" && row.OperationID == w.Operation && row.ExecutionID == w.Execution && row.IntentHash == w.IntentSHA && row.ExecutionSpecHash == w.SpecSHA
}
func originalCommandTarget(row *ent.ModelDevAcceptance) OriginalCommandTarget {
	t := OriginalCommandTarget{ID: row.ID, OperationID: row.OperationID, ExecutionID: row.ExecutionID, IntentSHA256: row.IntentHash, SpecSHA256: row.ExecutionSpecHash, RowSHA256: originalCommandRowSHA(row), Actor: row.Actor, AcceptedAt: row.AcceptedAt, DispatchState: string(row.DispatchState), CloseDispatchState: string(row.CloseDispatchState), RetryBlocked: row.RetryBlocked, StopGeneration: row.StopIntentGeneration, Blockers: []string{}}
	if !originalCommandIdentityValid(row) {
		t.Blockers = append(t.Blockers, "ORIGINAL_IDENTITY_DIFFERS")
		return t
	}
	a, e := modelDevAcceptanceFromRow(row)
	if e != nil {
		t.Blockers = append(t.Blockers, "NATIVE_21BF_CODEC_REJECTED")
		return t
	}
	if a.Snapshot.ComputeOrigin.DeviceMode != "cpu" {
		t.Blockers = append(t.Blockers, "ORIGINAL_CPU_REQUIRED")
	}
	if row.CloseDispatchState == modeldevacceptance.CloseDispatchStateACKED {
		i, e := modelDevStopIntentFromRow(row)
		var receipt ModelDevCloseReceipt
		if e != nil || json.Unmarshal(row.CloseReceiptCanonical, &receipt) != nil || validateModelDevCloseReceipt(receipt, *i) != nil || (receipt.CloseState != "CLOSING" && receipt.CloseState != "CLOSED") {
			t.Blockers = append(t.Blockers, "DURABLE_ORIGINAL_STOP_ACK_INVALID")
		} else {
			b, _ := json.Marshal(receipt)
			if !bytes.Equal(b, row.CloseReceiptCanonical) {
				t.Blockers = append(t.Blockers, "DURABLE_ORIGINAL_STOP_ACK_INVALID")
			} else {
				t.CloseReceipt = &receipt
			}
		}
	}
	return t
}
func (r *ModelDevAcceptanceRepo) PlanOriginalCommandRetirement(ctx context.Context) (OriginalCommandPlan, error) {
	ctx = appViewer.NewSystemViewerContext(ctx)
	p := OriginalCommandPlan{Schema: "ani.modeldev.original-command-retirement-plan.v1", Targets: []OriginalCommandTarget{}}
	for _, id := range []uint32{55, 57, 59, 60} {
		row, e := r.entClient.Client().ModelDevAcceptance.Query().Where(modeldevacceptance.IDEQ(id)).Only(ctx)
		if e != nil {
			return p, errors.New("ORIGINAL_COMMAND_ROW_UNAVAILABLE")
		}
		t := originalCommandTarget(row)
		if e = r.originalCommandNoAccounting(ctx, row.OperationID, row.ExecutionID); e != nil {
			t.Blockers = append(t.Blockers, "ORIGINAL_ACCOUNTING_PRESENT_OR_UNREADABLE")
		}
		p.Targets = append(p.Targets, t)
	}
	return p, nil
}
func (r *ModelDevAcceptanceRepo) originalCommandNoAccounting(ctx context.Context, operation, execution string) error {
	var n int
	e := r.entClient.DB().QueryRowContext(ctx, `SELECT (SELECT count(*) FROM sys_quota_operations WHERE operation_id=$1 OR resource_id=$2 OR create_operation_id=$1)+(SELECT count(*) FROM sys_quota_charges WHERE operation_id=$1)`, operation, execution).Scan(&n)
	if e != nil || n != 0 {
		return errors.New("ORIGINAL_ACCOUNTING_PRESENT_OR_UNREADABLE")
	}
	return nil
}
func (r *ModelDevAcceptanceRepo) originalCommandNoLeases(ctx context.Context) error {
	var n int
	e := r.entClient.DB().QueryRowContext(ctx, `SELECT count(*) FROM sys_modeldev_acceptances WHERE (dispatch_state='DISPATCHING' AND lease_until>clock_timestamp()) OR (close_dispatch_state='DISPATCHING' AND close_lease_until>clock_timestamp())`).Scan(&n)
	if e != nil || n != 0 {
		return errors.New("ORIGINAL_COMMAND_LIVE_LEASES")
	}
	return nil
}
func originalPlanTarget(p OriginalCommandPlan, id uint32) (OriginalCommandTarget, error) {
	if p.Schema != "ani.modeldev.original-command-retirement-plan.v1" || len(p.Targets) != 4 {
		return OriginalCommandTarget{}, errors.New("ORIGINAL_COMMAND_PLAN_INVALID")
	}
	seen := map[uint32]bool{}
	var found OriginalCommandTarget
	for _, t := range p.Targets {
		w, ok := originalCommands[t.ID]
		if !ok || seen[t.ID] || t.OperationID != w.Operation || t.ExecutionID != w.Execution || t.SpecSHA256 != w.SpecSHA || t.IntentSHA256 != w.IntentSHA || !modelDevDigest(t.RowSHA256) || len(t.Blockers) != 0 {
			return found, errors.New("ORIGINAL_COMMAND_PLAN_BLOCKED")
		}
		seen[t.ID] = true
		if t.ID == id {
			found = t
		}
	}
	if found.ID == 0 {
		return found, errors.New("ORIGINAL_COMMAND_ID_NOT_ALLOWED")
	}
	return found, nil
}

// StopOriginalCommand uses the unchanged durable Stop queue/sender/ACK chain.
// A CLOSING tombstone is reported as CLOSING and never represented as CLOSED.
func (r *ModelDevAcceptanceRepo) StopOriginalCommand(ctx context.Context, p OriginalCommandPlan, id uint32, proof OriginalCommandProof, sender *ModelDevClient) (ModelDevCloseReceipt, error) {
	t, e := originalPlanTarget(p, id)
	if e != nil {
		return ModelDevCloseReceipt{}, e
	}
	if e = proof.Validate(OriginalCommandPlanSHA(p)); e != nil {
		return ModelDevCloseReceipt{}, e
	}
	ctx = appViewer.NewSystemViewerContext(ctx)
	row, e := r.entClient.Client().ModelDevAcceptance.Query().Where(modeldevacceptance.IDEQ(id)).Only(ctx)
	if e != nil || originalCommandRowSHA(row) != t.RowSHA256 {
		return ModelDevCloseReceipt{}, errors.New("ORIGINAL_COMMAND_ROW_DRIFT")
	}
	if row.StopIntentGeneration < 0 || row.StopIntentGeneration > 1 || (row.StopIntentGeneration == 0 && !(row.DispatchState == modeldevacceptance.DispatchStateUNKNOWN && row.RetryBlocked && row.LastErrorCode != nil && *row.LastErrorCode == "INVALID_COMMAND" && row.AttemptCount == 0 && row.LeaseGeneration == 0 && row.LeaseOwner == nil && row.LeaseUntil == nil)) {
		return ModelDevCloseReceipt{}, errors.New("ORIGINAL_COMMAND_NOT_CLAIMABLE")
	}
	if e = r.originalCommandNoLeases(ctx); e != nil {
		return ModelDevCloseReceipt{}, e
	}
	if e = r.originalCommandNoAccounting(ctx, t.OperationID, t.ExecutionID); e != nil {
		return ModelDevCloseReceipt{}, e
	}
	intent, _, e := r.AcceptStop(ctx, 2, row.ResourceTenantID, row.ExecutionID, proof.StopActor)
	if e != nil {
		return ModelDevCloseReceipt{}, e
	}
	if t.CloseReceipt != nil {
		return *t.CloseReceipt, nil
	}
	claim, e := r.ClaimCloseDeliveryFor(ctx, *intent, "original-command-retirement-stop", time.Minute)
	if e != nil || claim == nil {
		return ModelDevCloseReceipt{}, errors.New("ORIGINAL_STOP_NOT_CLAIMABLE")
	}
	receipt, e := sender.ApplyCloseIntent(ctx, *claim.Intent)
	if e != nil {
		if changed, writeErr := r.DeferCloseDelivery(ctx, claim, originalRetirementFailure(e)); writeErr != nil || !changed {
			return receipt, errors.New("ORIGINAL_STOP_FAILURE_NOT_COMMITTED")
		}
		return receipt, e
	}
	changed, e := r.AckCloseDelivery(ctx, claim, receipt)
	if e != nil || !changed {
		return receipt, errors.New("ORIGINAL_STOP_ACK_NOT_COMMITTED")
	}
	return receipt, nil
}
func originalRetirementFailure(e error) ModelDevDeliveryFailure {
	var f *ModelDevDeliveryFailure
	if errors.As(e, &f) {
		return *f
	}
	return ModelDevDeliveryFailure{Code: "OWNER_UNAVAILABLE"}
}

// ClaimOriginalCommandRetirement is the only extra queue operation. It cannot
// unpause the publisher or rewrite the saved command. Existing completion and
// lease fencing remain authoritative. Each attempt requires a newly read plan.
func (r *ModelDevAcceptanceRepo) ClaimOriginalCommandRetirement(ctx context.Context, p OriginalCommandPlan, id uint32, proof OriginalCommandProof) (*ModelDevDeliveryClaim, error) {
	t, e := originalPlanTarget(p, id)
	if e != nil {
		return nil, e
	}
	if e = proof.Validate(OriginalCommandPlanSHA(p)); e != nil {
		return nil, e
	}
	ctx = appViewer.NewSystemViewerContext(ctx)
	var claim *ModelDevDeliveryClaim
	e = r.acceptanceTransaction(ctx, func(tx *ent.Tx) error {
		if _, e := tx.Tenant.Query().Where(tenant.IDEQ(2), tenant.ResourceTenantIDEQ("64c5c35a-f554-432c-b4bc-7b4f36f08420")).ForUpdate().Only(ctx); e != nil {
			return e
		}
		row, e := tx.ModelDevAcceptance.Query().Where(modeldevacceptance.IDEQ(id), modeldevacceptance.TenantIDEQ(2)).ForUpdate().Only(ctx)
		if e != nil {
			return e
		}
		if originalCommandRowSHA(row) != t.RowSHA256 {
			return errors.New("ORIGINAL_COMMAND_ROW_DRIFT")
		}
		current := originalCommandTarget(row)
		if len(current.Blockers) != 0 || current.CloseReceipt == nil || row.StopIntentGeneration != 1 {
			return errors.New("DURABLE_ORIGINAL_STOP_ACK_REQUIRED")
		}
		if row.DispatchState == modeldevacceptance.DispatchStateACKED {
			return errors.New("ORIGINAL_COMMAND_ALREADY_ACKED")
		}
		initial := row.DispatchState == modeldevacceptance.DispatchStateUNKNOWN && row.RetryBlocked && row.LastErrorCode != nil && *row.LastErrorCode == "INVALID_COMMAND" && row.AttemptCount == 0 && row.LeaseGeneration == 0 && row.LeaseOwner == nil && row.LeaseUntil == nil
		retry := row.StopIntentGeneration > 0 && row.AttemptCount > 0 && row.LeaseGeneration == row.AttemptCount && ((row.DispatchState == modeldevacceptance.DispatchStateUNKNOWN && !row.RetryBlocked && row.LastErrorCode != nil && (*row.LastErrorCode == "OWNER_UNAVAILABLE" || *row.LastErrorCode == "INVALID_ACK")) || (row.DispatchState == modeldevacceptance.DispatchStateDISPATCHING && row.LeaseUntil != nil && row.LeaseUntil.Before(time.Now()) && row.LeaseOwner != nil && *row.LeaseOwner == "original-command-retirement-create"))
		if (!initial && !retry) || row.LeaseGeneration == math.MaxInt64 || row.AttemptCount == math.MaxInt64 {
			return errors.New("ORIGINAL_COMMAND_NOT_CLAIMABLE")
		}
		if e = r.originalCommandNoLeases(ctx); e != nil {
			return e
		}
		if e = r.originalCommandNoAccounting(ctx, row.OperationID, row.ExecutionID); e != nil {
			return e
		}
		a, e := modelDevAcceptanceFromRow(row)
		if e != nil {
			return e
		}
		n, e := tx.ModelDevAcceptance.Update().Where(modeldevacceptance.IDEQ(id), modeldevacceptance.TenantIDEQ(2), modeldevacceptance.LeaseGenerationEQ(row.LeaseGeneration), modeldevacceptance.AttemptCountEQ(row.AttemptCount)).SetDispatchState(modeldevacceptance.DispatchStateDISPATCHING).SetRetryBlocked(false).ClearLastErrorCode().ClearNextAttemptAt().SetLeaseOwner("original-command-retirement-create").SetLeaseGeneration(row.LeaseGeneration + 1).SetAttemptCount(row.AttemptCount + 1).Modify(func(u *entsql.UpdateBuilder) {
			u.Set(modeldevacceptance.FieldLeaseUntil, modelDevDeliveryAfter(time.Minute))
		}).Save(ctx)
		if e != nil {
			return e
		}
		if n != 1 {
			return fmt.Errorf("ORIGINAL_COMMAND_CAS_FAILED")
		}
		a.DispatchState = "DISPATCHING"
		claim = &ModelDevDeliveryClaim{Acceptance: a, LeaseOwner: "original-command-retirement-create", LeaseGeneration: row.LeaseGeneration + 1, AttemptCount: row.AttemptCount + 1}
		return nil
	})
	return claim, e
}
func (r *ModelDevAcceptanceRepo) DeliverOriginalCommand(ctx context.Context, p OriginalCommandPlan, id uint32, proof OriginalCommandProof, sender *ModelDevClient) (ModelDevOwnerReceipt, error) {
	c, e := r.ClaimOriginalCommandRetirement(ctx, p, id, proof)
	if e != nil {
		return ModelDevOwnerReceipt{}, e
	}
	receipt, e := sender.AcceptExecution(ctx, c.Acceptance.Envelope())
	if e == nil && receipt.CloseState != "CLOSING" && receipt.CloseState != "CLOSED" {
		e = &ModelDevDeliveryFailure{Code: "INVALID_ACK"}
	}
	if e != nil {
		if changed, writeErr := r.DeferDelivery(ctx, c, originalRetirementFailure(e)); writeErr != nil || !changed {
			return receipt, errors.New("ORIGINAL_CREATE_FAILURE_NOT_COMMITTED")
		}
		return receipt, e
	}
	changed, e := r.AckDelivery(ctx, c, receipt)
	if e != nil || !changed {
		return receipt, errors.New("ORIGINAL_CREATE_ACK_NOT_COMMITTED")
	}
	return receipt, nil
}

// OriginalCommandFailureReason excludes database/remote text from operator output.
func OriginalCommandFailureReason(err error) string {
	var failure *ModelDevDeliveryFailure
	if errors.As(err, &failure) {
		return failure.Code
	}
	for _, code := range []string{"ORIGINAL_COMMAND_FRESH_ROOT_PROOF_REQUIRED", "ORIGINAL_COMMAND_PLAN_INVALID", "ORIGINAL_COMMAND_PLAN_BLOCKED", "ORIGINAL_COMMAND_ID_NOT_ALLOWED", "ORIGINAL_COMMAND_ROW_DRIFT", "ORIGINAL_COMMAND_LIVE_LEASES", "ORIGINAL_ACCOUNTING_PRESENT_OR_UNREADABLE", "ORIGINAL_STOP_NOT_CLAIMABLE", "ORIGINAL_STOP_ACK_NOT_COMMITTED", "ORIGINAL_STOP_FAILURE_NOT_COMMITTED", "DURABLE_ORIGINAL_STOP_ACK_REQUIRED", "ORIGINAL_COMMAND_ALREADY_ACKED", "ORIGINAL_COMMAND_NOT_CLAIMABLE", "ORIGINAL_COMMAND_CAS_FAILED", "ORIGINAL_CREATE_ACK_NOT_COMMITTED", "ORIGINAL_CREATE_FAILURE_NOT_COMMITTED"} {
		if err != nil && err.Error() == code {
			return code
		}
	}
	return "ORIGINAL_PROTOCOL_STATE_UNAVAILABLE"
}

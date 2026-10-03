package data

import (
	"context"
	"fmt"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/modeldevacceptance"
	"go-wind-admin/app/admin/service/internal/data/ent/tenant"
)

// Generation belongs to Governance's durable source command, not ModelDev's
// independently allocated creation fence. A user Stop never resets this intent.
type ModelDevStopIntent struct {
	TenantID                                                      uint32
	ResourceTenantID, OperationID, ExecutionID, ExecutionSpecHash string
	Generation                                                    uint64
	RequestedActor                                                string
	RequestedAt                                                   time.Time
}

func (r *ModelDevAcceptanceRepo) AcceptStop(ctx context.Context, tenantID uint32, resourceTenantID, executionID, actor string) (*ModelDevStopIntent, bool, error) {
	if tenantID == 0 || !modelDevCanonicalUUID(resourceTenantID) || !modelDevCanonicalUUID(executionID) || !validModelDevClientActor(actor) {
		return nil, false, fmt.Errorf("%w: modeldev stop scope", cpup01.ErrInvalidArgument)
	}
	var intent *ModelDevStopIntent
	var replayed bool
	err := r.acceptanceTransaction(ctx, func(tx *ent.Tx) error {
		if _, err := tx.Tenant.Query().Where(tenant.IDEQ(tenantID), tenant.ResourceTenantIDEQ(resourceTenantID)).ForUpdate().Only(ctx); err != nil {
			return err
		}
		row, err := tx.ModelDevAcceptance.Query().Where(modeldevacceptance.TenantIDEQ(tenantID), modeldevacceptance.ResourceTenantIDEQ(resourceTenantID), modeldevacceptance.ExecutionIDEQ(executionID)).ForUpdate().Only(ctx)
		if err != nil {
			return err
		}
		if _, err := modelDevAcceptanceFromRow(row); err != nil {
			return err
		}
		if row.StopIntentGeneration == 0 {
			_, err = tx.ModelDevAcceptance.Update().Where(modeldevacceptance.IDEQ(row.ID), modeldevacceptance.TenantIDEQ(tenantID), modeldevacceptance.StopIntentGenerationEQ(0)).
				SetStopIntentGeneration(1).SetStopRequestedActor(actor).SetCloseDispatchState(modeldevacceptance.CloseDispatchStateQUEUED).
				Modify(func(u *entsql.UpdateBuilder) {
					u.Set(modeldevacceptance.FieldStopRequestedAt, entsql.Expr("statement_timestamp()"))
				}).Save(ctx)
			if err != nil {
				return err
			}
			row, err = tx.ModelDevAcceptance.Query().Where(modeldevacceptance.IDEQ(row.ID), modeldevacceptance.TenantIDEQ(tenantID)).Only(ctx)
			if err != nil {
				return err
			}
		} else {
			replayed = true
		}
		intent, err = modelDevStopIntentFromRow(row)
		return err
	})
	if ent.IsNotFound(err) {
		return nil, false, ErrModelDevAcceptanceNotFound
	}
	if err != nil {
		return nil, false, err
	}
	return intent, replayed, nil
}

func modelDevStopIntentFromRow(row *ent.ModelDevAcceptance) (*ModelDevStopIntent, error) {
	if row == nil || row.TenantID == nil || *row.TenantID == 0 || row.StopIntentGeneration <= 0 || row.StopRequestedAt == nil || row.StopRequestedActor == nil || !validModelDevClientActor(*row.StopRequestedActor) || row.StopRequestedAt.IsZero() || row.CloseDispatchState == modeldevacceptance.CloseDispatchStateIDLE {
		return nil, fmt.Errorf("invalid persisted modeldev stop intent")
	}
	return &ModelDevStopIntent{TenantID: *row.TenantID, ResourceTenantID: row.ResourceTenantID, OperationID: row.OperationID, ExecutionID: row.ExecutionID, ExecutionSpecHash: row.ExecutionSpecHash, Generation: uint64(row.StopIntentGeneration), RequestedActor: *row.StopRequestedActor, RequestedAt: *row.StopRequestedAt}, nil
}

type ModelDevCloseDeliveryClaim struct {
	Intent *ModelDevStopIntent
	LeaseOwner string
	LeaseGeneration, AttemptCount int64
}

func (r *ModelDevAcceptanceRepo) ClaimCloseDelivery(ctx context.Context, workerID string, lease time.Duration) (*ModelDevCloseDeliveryClaim, error) {
	return nil, fmt.Errorf("modeldev close delivery not implemented")
}

func (r *ModelDevAcceptanceRepo) AckCloseDelivery(ctx context.Context, claim *ModelDevCloseDeliveryClaim, receipt ModelDevCloseReceipt) (bool, error) {
	return false, fmt.Errorf("modeldev close delivery not implemented")
}

func (r *ModelDevAcceptanceRepo) DeferCloseDelivery(ctx context.Context, claim *ModelDevCloseDeliveryClaim, failure ModelDevDeliveryFailure) (bool, error) {
	return false, fmt.Errorf("modeldev close delivery not implemented")
}

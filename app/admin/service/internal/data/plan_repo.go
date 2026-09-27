package data

import (
	"context"
	"time"

	"entgo.io/ent/dialect/sql"
	"go-wind-admin/pkg/localdeps/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/pkg/localdeps/kratos-bootstrap/logger"

	paginationV1 "go-wind-admin/pkg/localdeps/go-crud/api/gen/go/pagination/v1"
	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"

	"go-wind-admin/pkg/localdeps/go-utils/copierutil"
	"go-wind-admin/pkg/localdeps/go-utils/mapper"

	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/plan"
	"go-wind-admin/app/admin/service/internal/data/ent/tenant"

	"go-wind-admin/app/admin/service/internal/data/ent/predicate"
	appViewer "go-wind-admin/pkg/entgo/viewer"

	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
)

type PlanRepo struct {
	entClient *entCrud.EntClient[*ent.Client]
	log       *bLogger.Helper

	mapper           *mapper.CopierMapper[identityV1.Plan, ent.Plan]
	versionConverter *mapper.EnumTypeConverter[identityV1.Plan_Version, plan.Version]
	expiryPolicyConv *mapper.EnumTypeConverter[identityV1.Plan_ExpiryPolicy, plan.ExpiryPolicy]

	repository *entCrud.Repository[
		ent.PlanQuery, ent.PlanSelect,
		ent.PlanCreate, ent.PlanCreateBulk,
		ent.PlanUpdate, ent.PlanUpdateOne,
		ent.PlanDelete,
		predicate.Plan,
		identityV1.Plan, ent.Plan,
	]
}

func NewPlanRepo(
	ctx *bootstrap.Context,
	entClient *entCrud.EntClient[*ent.Client],
) *PlanRepo {
	repo := &PlanRepo{
		log:       ctx.NewLoggerHelper("plan/repo/admin-service"),
		entClient: entClient,
		mapper:    mapper.NewCopierMapper[identityV1.Plan, ent.Plan](),
		versionConverter: mapper.NewEnumTypeConverter[identityV1.Plan_Version, plan.Version](
			identityV1.Plan_Version_name, identityV1.Plan_Version_value,
		),
		expiryPolicyConv: mapper.NewEnumTypeConverter[identityV1.Plan_ExpiryPolicy, plan.ExpiryPolicy](
			identityV1.Plan_ExpiryPolicy_name, identityV1.Plan_ExpiryPolicy_value,
		),
	}

	repo.init()

	return repo
}

func (r *PlanRepo) init() {
	r.repository = entCrud.NewRepository[
		ent.PlanQuery, ent.PlanSelect,
		ent.PlanCreate, ent.PlanCreateBulk,
		ent.PlanUpdate, ent.PlanUpdateOne,
		ent.PlanDelete,
		predicate.Plan,
		identityV1.Plan, ent.Plan,
	](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())

	r.mapper.AppendConverters(r.versionConverter.NewConverterPair())
	r.mapper.AppendConverters(r.expiryPolicyConv.NewConverterPair())
}

func (r *PlanRepo) Count(ctx context.Context, req *paginationV1.PagingRequest) (int, error) {
	builder := r.entClient.Client().Plan.Query()

	whereSelectors, _, err := r.repository.BuildListSelectorWithPaging(builder, req)
	if err != nil {
		r.log.Errorf(ctx, "parse count param error [%s]", err.Error())
		return 0, identityV1.ErrorInternalServerError("invalid query parameter")
	}
	if len(whereSelectors) != 0 {
		builder.Modify(whereSelectors...)
	}

	count, err := builder.Count(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query count failed: %s", err.Error())
		return 0, identityV1.ErrorInternalServerError("query count failed")
	}

	return count, nil
}

func (r *PlanRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*identityV1.ListPlanResponse, error) {
	if req == nil {
		return nil, identityV1.ErrorBadRequest("invalid parameter")
	}

	builder := r.entClient.Client().Plan.Query()

	ret, err := r.repository.ListWithPaging(ctx, builder, builder.Clone(), req)
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return &identityV1.ListPlanResponse{Total: 0, Items: nil}, nil
	}

	return &identityV1.ListPlanResponse{
		Total: ret.Total,
		Items: ret.Items,
	}, nil
}

func (r *PlanRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.entClient.Client().Plan.Query().
		Where(plan.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return false, identityV1.ErrorInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *PlanRepo) Get(ctx context.Context, req *identityV1.GetPlanRequest) (*identityV1.Plan, error) {
	if req == nil {
		return nil, identityV1.ErrorBadRequest("invalid parameter")
	}

	builder := r.entClient.Client().Plan.Query()

	var whereCond []func(s *sql.Selector)
	switch req.QueryBy.(type) {
	default:
	case *identityV1.GetPlanRequest_Id:
		whereCond = append(whereCond, plan.IDEQ(req.GetId()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	return dto, err
}

func (r *PlanRepo) Create(ctx context.Context, req *identityV1.CreatePlanRequest) (err error) {
	if req == nil || req.Data == nil {
		return identityV1.ErrorBadRequest("invalid parameter")
	}

	var tx *ent.Tx
	tx, err = r.entClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return identityV1.ErrorInternalServerError("start transaction failed")
	}
	defer func() {
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				r.log.Errorf(ctx, "transaction rollback failed: %s", rollbackErr.Error())
			}
			return
		}
		if commitErr := tx.Commit(); commitErr != nil {
			r.log.Errorf(ctx, "transaction commit failed: %s", commitErr.Error())
			err = identityV1.ErrorInternalServerError("transaction commit failed")
		}
	}()

	builder := tx.Plan.Create().
		SetNillableName(req.Data.Name).
		SetNillableVersion(r.versionConverter.ToEntity(req.Data.Version)).
		SetNillableExpiryPolicy(r.expiryPolicyConv.ToEntity(req.Data.ExpiryPolicy)).
		SetNillableDataRetentionDays(req.Data.DataRetentionDays).
		SetNillableDescription(req.Data.Description).
		SetNillableRemark(req.Data.Remark).
		SetNillableCreatedBy(req.Data.CreatedBy).
		SetCreatedAt(time.Now())

	if req.Data.Id != nil {
		builder.SetID(req.GetData().GetId())
	}

	if _, err = builder.Save(ctx); err != nil {
		// sys_plans.name 有唯一索引：重名是用户可自行纠正的输入错误，返回 400 而非 500
		if ent.IsConstraintError(err) {
			r.log.Warnf(ctx, "insert plan duplicated: %s", err.Error())
			return identityV1.ErrorBadRequest("plan name [%s] already exists", req.Data.GetName())
		}
		r.log.Errorf(ctx, "insert plan failed: %s", err.Error())
		return identityV1.ErrorInternalServerError("insert plan failed")
	}

	return nil
}

func (r *PlanRepo) Update(ctx context.Context, req *identityV1.UpdatePlanRequest) (err error) {
	if req == nil || req.Data == nil {
		return identityV1.ErrorBadRequest("invalid parameter")
	}
	if req.GetId() == 0 {
		return identityV1.ErrorBadRequest("id is required")
	}

	// 如果不存在则创建
	if req.GetAllowMissing() {
		var exist bool
		exist, err = r.IsExist(ctx, req.GetId())
		if err != nil {
			return err
		}
		if !exist {
			createReq := &identityV1.CreatePlanRequest{Data: req.Data}
			createReq.Data.CreatedBy = createReq.Data.UpdatedBy
			createReq.Data.UpdatedBy = nil
			return r.Create(ctx, createReq)
		}
	}

	var tx *ent.Tx
	tx, err = r.entClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return identityV1.ErrorInternalServerError("start transaction failed")
	}
	defer func() {
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				r.log.Errorf(ctx, "transaction rollback failed: %s", rollbackErr.Error())
			}
			return
		}
		if commitErr := tx.Commit(); commitErr != nil {
			r.log.Errorf(ctx, "transaction commit failed: %s", commitErr.Error())
			err = identityV1.ErrorInternalServerError("transaction commit failed")
		}
	}()

	builder := tx.Plan.UpdateOneID(req.GetId())
	_, err = r.repository.UpdateOne(ctx, builder, req.Data, req.GetUpdateMask(),
		func(dto *identityV1.Plan) {
			builder.
				SetNillableName(req.Data.Name).
				SetNillableVersion(r.versionConverter.ToEntity(req.Data.Version)).
				SetNillableExpiryPolicy(r.expiryPolicyConv.ToEntity(req.Data.ExpiryPolicy)).
				SetNillableDataRetentionDays(req.Data.DataRetentionDays).
				SetNillableDescription(req.Data.Description).
				SetNillableRemark(req.Data.Remark).
				SetNillableUpdatedBy(req.Data.UpdatedBy).
				SetUpdatedAt(time.Now())
		},
		func(s *sql.Selector) {
			s.Where(sql.EQ(plan.FieldID, req.GetId()))
		},
	)
	if err != nil {
		// 名称改重时命中 uix_sys_plans_name 唯一索引，返回 400 而非 500
		if ent.IsConstraintError(err) {
			r.log.Warnf(ctx, "update plan duplicated: %s", err.Error())
			return identityV1.ErrorBadRequest("plan name [%s] already exists", req.Data.GetName())
		}
		r.log.Errorf(ctx, "update plan failed: %s", err.Error())
		return identityV1.ErrorInternalServerError("update plan failed")
	}

	return err
}

func (r *PlanRepo) Delete(ctx context.Context, id uint32) error {
	if id == 0 {
		return identityV1.ErrorBadRequest("invalid parameter")
	}

	// 计划 §6.13：删除套餐必须保护绑定租户，不得级联删除租户账本。
	// 先锁 plan 再检查绑定（锁顺序：plan 行 FOR UPDATE）。
	sysCtx := appViewer.NewSystemViewerContext(ctx)
	tx, err := r.entClient.Client().Tx(sysCtx)
	if err != nil {
		r.log.Errorf(ctx, "delete plan %d: start tx failed: %s", id, err.Error())
		return identityV1.ErrorInternalServerError("start transaction failed")
	}
	defer func() { _ = tx.Rollback() }()
	plq := tx.Plan.Query().Where(plan.IDEQ(id))
	if supportsRowLock(r.entClient) {
		plq = plq.ForUpdate()
	}
	if _, err = plq.Only(sysCtx); ent.IsNotFound(err) {
		return identityV1.ErrorNotFound("plan not found")
	} else if err != nil {
		r.log.Errorf(ctx, "delete plan %d: lock plan failed: %s", id, err.Error())
		return identityV1.ErrorInternalServerError("lock plan failed")
	}
	bound, err := tx.Tenant.Query().Where(tenant.PlanIDEQ(id)).Exist(sysCtx)
	if err != nil {
		r.log.Errorf(ctx, "delete plan %d: check bound tenants failed: %s", id, err.Error())
		return identityV1.ErrorInternalServerError("check plan tenants failed")
	}
	if bound {
		return identityV1.ErrorConflict("plan is bound to tenants")
	}
	_ = tx.Rollback() // 只读检查，删除仍走原路径

	if err := r.entClient.Client().Plan.DeleteOneID(id).Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return identityV1.ErrorNotFound("plan not found")
		}

		r.log.Errorf(ctx, "delete one data failed: %s", err.Error())

		return identityV1.ErrorInternalServerError("delete failed")
	}

	return nil
}

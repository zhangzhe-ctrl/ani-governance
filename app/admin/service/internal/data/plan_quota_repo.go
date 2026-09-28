package data

import (
	"context"
	quotapb "go-wind-admin/api/gen/go/quota/service/v1"
	"math"

	paginationV1 "go-wind-admin/pkg/localdeps/go-crud/api/gen/go/pagination/v1"
	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"
	"go-wind-admin/pkg/localdeps/go-utils/copierutil"
	"go-wind-admin/pkg/localdeps/go-utils/fieldmaskutil"
	"go-wind-admin/pkg/localdeps/go-utils/mapper"
	"go-wind-admin/pkg/localdeps/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/pkg/localdeps/kratos-bootstrap/logger"

	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/plan"
	"go-wind-admin/app/admin/service/internal/data/ent/planquota"
	appViewer "go-wind-admin/pkg/entgo/viewer"
)

type PlanQuotaRepo struct {
	entClient     *entCrud.EntClient[*ent.Client]
	log           *bLogger.Helper
	mapper        *mapper.CopierMapper[identityV1.PlanQuota, ent.PlanQuota]
	quotaTypeConv *mapper.EnumTypeConverter[identityV1.PlanQuota_QuotaType, planquota.QuotaType]
}

func NewPlanQuotaRepo(ctx *bootstrap.Context, c *entCrud.EntClient[*ent.Client]) *PlanQuotaRepo {
	r := &PlanQuotaRepo{entClient: c, log: ctx.NewLoggerHelper("plan-quota/repo/admin-service")}
	r.init()
	return r
}
func (r *PlanQuotaRepo) init() {
	if r.mapper == nil {
		r.mapper = mapper.NewCopierMapper[identityV1.PlanQuota, ent.PlanQuota]()
	}
	if r.quotaTypeConv == nil {
		r.quotaTypeConv = mapper.NewEnumTypeConverter[identityV1.PlanQuota_QuotaType, planquota.QuotaType](identityV1.PlanQuota_QuotaType_name, identityV1.PlanQuota_QuotaType_value)
	}
	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())
	r.mapper.AppendConverters(r.quotaTypeConv.NewConverterPair())
}
func (r *PlanQuotaRepo) transaction(ctx context.Context, fn func(*ent.Tx) error) error {
	err := quotaTransaction(ctx, r.entClient.Client(), fn)
	if ent.IsConstraintError(err) {
		return quotapb.ErrorIdempotencyConflict("%s", "plan already has this quota code")
	}
	if ent.IsNotFound(err) {
		return identityV1.ErrorNotFound("plan quota not found")
	}
	return err
}
func (r *PlanQuotaRepo) toDTO(v *ent.PlanQuota) *identityV1.PlanQuota {
	out := r.mapper.ToDTO(v)
	// The plan foreign key is an Ent edge, not a scalar PlanQuota field.
	out.PlanId = ptr(v.Edges.Plan.ID)
	projectQuotaCompatFields(out, v)
	return out
}
func projectQuotaCompatFields(dto *identityV1.PlanQuota, entity *ent.PlanQuota) {
	dto.QuotaCode = ptr(entity.QuotaCode)
	dto.QuotaType = ProjectLegacyTypeForRead(entity.QuotaCode)
}
func planQuotaReadMask(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	selected := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "*" {
			return nil, nil
		}
		field, ok := planQuotaField(p)
		if !ok {
			return nil, quotapb.ErrorInvalidQuotaRequest("%s", "invalid plan quota read mask")
		}
		selected = append(selected, field)
	}
	return selected, nil
}
func applyPlanQuotaReadMask(dto *identityV1.PlanQuota, mask []string) {
	fieldmaskutil.Filter(dto, mask)
}
func (r *PlanQuotaRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (out *identityV1.ListPlanQuotaResponse, err error) {
	if req == nil {
		return nil, quotapb.ErrorInvalidQuotaRequest("%s", "paging request required")
	}
	params, e := planQuotaListParams(req)
	if e != nil {
		return nil, e
	}
	mask, e := planQuotaReadMask(req.GetFieldMask().GetPaths())
	if e != nil {
		return nil, e
	}
	err = r.transaction(ctx, func(tx *ent.Tx) error {
		ctx := appViewer.NewSystemViewerContext(ctx)
		builder := tx.PlanQuota.Query().Where(params.filter)
		total, e := builder.Clone().Count(ctx)
		if e != nil {
			return e
		}
		if params.afterID != nil {
			builder.Where(planquota.IDGT(*params.afterID))
		}
		builder.Order(params.order...).WithPlan().Offset(params.offset)
		if params.limit != nil {
			builder.Limit(*params.limit)
		}
		rows, e := builder.All(ctx)
		if e != nil {
			return e
		}
		out = &identityV1.ListPlanQuotaResponse{Total: uint64(total), Items: make([]*identityV1.PlanQuota, 0, len(rows))}
		for _, v := range rows {
			dto := r.toDTO(v)
			applyPlanQuotaReadMask(dto, mask)
			out.Items = append(out.Items, dto)
		}
		return nil
	})
	return
}
func (r *PlanQuotaRepo) Get(ctx context.Context, req *identityV1.GetPlanQuotaRequest) (out *identityV1.PlanQuota, err error) {
	if req == nil || req.GetId() == 0 {
		return nil, quotapb.ErrorInvalidQuotaRequest("%s", "id required")
	}
	mask, e := planQuotaReadMask(req.GetViewMask().GetPaths())
	if e != nil {
		return nil, e
	}
	err = r.transaction(ctx, func(tx *ent.Tx) error {
		ctx := appViewer.NewSystemViewerContext(ctx)
		v, e := tx.PlanQuota.Query().Where(planquota.IDEQ(req.GetId())).WithPlan().Only(ctx)
		if e == nil {
			out = r.toDTO(v)
			applyPlanQuotaReadMask(out, mask)
		}
		return e
	})
	return
}
func (r *PlanQuotaRepo) IsExist(ctx context.Context, id uint32) (ok bool, err error) {
	err = r.transaction(ctx, func(tx *ent.Tx) error {
		ctx := appViewer.NewSystemViewerContext(ctx)
		_, e := tx.PlanQuota.Query().Where(planquota.IDEQ(id)).WithPlan().Only(ctx)
		if ent.IsNotFound(e) {
			return nil
		}
		ok = e == nil
		return e
	})
	return
}
func legacyQuotaType(code string) *planquota.QuotaType {
	if v, ok := CodeToLegacyEntType(code); ok {
		return &v
	}
	return nil
}
func lockQuotaPlan(ctx context.Context, tx *ent.Tx, id uint32, lock bool) (*ent.Plan, error) {
	query := tx.Plan.Query().Where(plan.IDEQ(id))
	if lock {
		query.ForUpdate()
	}
	return query.Only(ctx)
}
func (r *PlanQuotaRepo) Create(ctx context.Context, req *identityV1.CreatePlanQuotaRequest) error {
	if req == nil || req.Data == nil {
		return quotapb.ErrorInvalidQuotaRequest("%s", "invalid parameter")
	}
	d := req.Data
	code, ok := ResolveQuotaCodeForWrite(d.QuotaCode, d.QuotaType)
	if !ok || d.GetPlanId() == 0 || d.QuotaValue == nil || d.GetQuotaValue() > math.MaxInt64 {
		return quotapb.ErrorInvalidQuotaRequest("%s", "valid plan/code/value required")
	}
	return r.transaction(ctx, func(tx *ent.Tx) error {
		databaseNow, clockErr := quotaDatabaseNow(ctx, tx)
		if clockErr != nil {
			return clockErr
		}
		ctx := appViewer.NewSystemViewerContext(ctx)
		if _, e := lockQuotaPlan(ctx, tx, d.GetPlanId(), supportsRowLock(r.entClient)); e != nil {
			return e
		}
		return tx.PlanQuota.Create().SetCreatedAt(databaseNow).SetUpdatedAt(databaseNow).SetPlanID(d.GetPlanId()).SetQuotaCode(code).SetNillableQuotaType(legacyQuotaType(code)).SetQuotaValue(d.GetQuotaValue()).SetNillableCreatedBy(d.CreatedBy).Exec(ctx)
	})
}
func (r *PlanQuotaRepo) Update(ctx context.Context, req *identityV1.UpdatePlanQuotaRequest) error {
	if req == nil || req.Data == nil || req.GetId() == 0 || req.GetAllowMissing() || len(req.GetUpdateMask().GetPaths()) == 0 {
		return quotapb.ErrorInvalidQuotaRequest("%s", "id and nonempty update mask required; allow_missing forbidden")
	}
	setCode, setValue := false, false
	for _, field := range req.GetUpdateMask().GetPaths() {
		canonical, ok := planQuotaField(field)
		if !ok {
			return quotapb.ErrorInvalidQuotaRequest("%s", "immutable or unsupported plan quota field")
		}
		switch canonical {
		case "quota_code", "quota_type":
			setCode = true
		case "quota_value":
			setValue = true
		case "updated_by":
		default:
			return quotapb.ErrorInvalidQuotaRequest("%s", "immutable or unsupported plan quota field")
		}
	}
	code := ""
	if setCode {
		var ok bool
		code, ok = ResolveQuotaCodeForWrite(req.Data.QuotaCode, req.Data.QuotaType)
		if !ok {
			return quotapb.ErrorInvalidQuotaRequest("%s", "invalid quota code/type")
		}
	}
	if setValue && (req.Data.QuotaValue == nil || req.Data.GetQuotaValue() > math.MaxInt64) {
		return quotapb.ErrorInvalidQuotaRequest("%s", "valid quota value required")
	}
	return r.transaction(ctx, func(tx *ent.Tx) error {
		databaseNow, clockErr := quotaDatabaseNow(ctx, tx)
		if clockErr != nil {
			return clockErr
		}
		ctx := appViewer.NewSystemViewerContext(ctx)
		row, e := tx.PlanQuota.Query().Where(planquota.IDEQ(req.GetId())).WithPlan().Only(ctx)
		if e != nil {
			return e
		}
		if _, e = lockQuotaPlan(ctx, tx, row.Edges.Plan.ID, supportsRowLock(r.entClient)); e != nil {
			return e
		}
		row, e = tx.PlanQuota.Query().Where(planquota.IDEQ(row.ID)).WithPlan().Only(ctx)
		if e != nil {
			return e
		}
		if setCode {
			row.QuotaCode = code
			row.QuotaType = legacyQuotaType(code)
		}
		if setValue {
			row.QuotaValue = req.Data.QuotaValue
		}
		builder := tx.PlanQuota.Update().SetUpdatedAt(databaseNow).Where(planquota.IDEQ(row.ID), planquota.HasPlanWith(plan.IDEQ(row.Edges.Plan.ID))).SetQuotaCode(row.QuotaCode).SetNillableQuotaValue(row.QuotaValue).SetNillableUpdatedBy(req.Data.UpdatedBy)
		if row.QuotaType == nil {
			builder.ClearQuotaType()
		} else {
			builder.SetQuotaType(*row.QuotaType)
		}
		n, e := builder.Save(ctx)
		if e != nil {
			return e
		}
		if n != 1 {
			return &ent.NotFoundError{}
		}
		return nil
	})
}
func (r *PlanQuotaRepo) Delete(ctx context.Context, id uint32) error {
	if id == 0 {
		return quotapb.ErrorInvalidQuotaRequest("%s", "id required")
	}
	return r.transaction(ctx, func(tx *ent.Tx) error {
		ctx := appViewer.NewSystemViewerContext(ctx)
		row, e := tx.PlanQuota.Query().Where(planquota.IDEQ(id)).WithPlan().Only(ctx)
		if e != nil {
			return e
		}
		if _, e = lockQuotaPlan(ctx, tx, row.Edges.Plan.ID, supportsRowLock(r.entClient)); e != nil {
			return e
		}
		n, e := tx.PlanQuota.Delete().Where(planquota.IDEQ(row.ID), planquota.HasPlanWith(plan.IDEQ(row.Edges.Plan.ID))).Exec(ctx)
		if e != nil {
			return e
		}
		if n != 1 {
			return &ent.NotFoundError{}
		}
		return nil
	})
}

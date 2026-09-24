package data

import (
	"context"
	"math"
	"strings"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"
	"go-wind-admin/pkg/localdeps/go-utils/copierutil"
	"go-wind-admin/pkg/localdeps/go-utils/mapper"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"

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
		return QuotaErrIdempotencyConflict("plan already has this quota code")
	}
	if ent.IsNotFound(err) {
		return identityV1.ErrorNotFound("plan quota not found")
	}
	return err
}
func planQuotaDTO(v *ent.PlanQuota) *identityV1.PlanQuota {
	out := &identityV1.PlanQuota{Id: ptr(uint32(v.ID)), PlanId: ptr(v.Edges.Plan.ID), QuotaCode: ptr(v.QuotaCode), QuotaType: ProjectLegacyTypeForRead(v.QuotaCode), QuotaValue: v.QuotaValue}
	if v.CreatedAt != nil {
		out.CreatedAt = timestamppb.New(*v.CreatedAt)
	}
	if v.UpdatedAt != nil {
		out.UpdatedAt = timestamppb.New(*v.UpdatedAt)
	}
	if v.DeletedAt != nil {
		out.DeletedAt = timestamppb.New(*v.DeletedAt)
	}
	if v.CreatedBy != nil {
		out.CreatedBy = ptr(uint32(*v.CreatedBy))
	}
	if v.UpdatedBy != nil {
		out.UpdatedBy = ptr(uint32(*v.UpdatedBy))
	}
	if v.DeletedBy != nil {
		out.DeletedBy = ptr(uint32(*v.DeletedBy))
	}
	return out
}
func projectQuotaCompatFields(dto *identityV1.PlanQuota, entity *ent.PlanQuota) {
	dto.QuotaCode = ptr(entity.QuotaCode)
	dto.QuotaType = ProjectLegacyTypeForRead(entity.QuotaCode)
}
func planQuotaReadMask(paths []string) (map[string]bool, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	selected := map[string]bool{}
	for _, p := range paths {
		if p == "*" {
			return nil, nil
		}
		field, ok := planQuotaField(p)
		if !ok {
			return nil, QuotaErrInvalid("invalid plan quota read mask")
		}
		selected[field] = true
	}
	return selected, nil
}
func applyPlanQuotaReadMask(dto *identityV1.PlanQuota, mask map[string]bool) {
	if mask == nil {
		return
	}
	m := dto.ProtoReflect()
	m.Range(func(f protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		if !mask[string(f.Name())] {
			m.Clear(f)
		}
		return true
	})
}
func (r *PlanQuotaRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (out *identityV1.ListPlanQuotaResponse, err error) {
	if req == nil {
		return nil, QuotaErrInvalid("paging request required")
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
			dto := planQuotaDTO(v)
			applyPlanQuotaReadMask(dto, mask)
			out.Items = append(out.Items, dto)
		}
		return nil
	})
	return
}
func (r *PlanQuotaRepo) Get(ctx context.Context, req *identityV1.GetPlanQuotaRequest) (out *identityV1.PlanQuota, err error) {
	if req == nil || req.GetId() == 0 {
		return nil, QuotaErrInvalid("id required")
	}
	mask, e := planQuotaReadMask(req.GetViewMask().GetPaths())
	if e != nil {
		return nil, e
	}
	err = r.transaction(ctx, func(tx *ent.Tx) error {
		ctx := appViewer.NewSystemViewerContext(ctx)
		v, e := tx.PlanQuota.Query().Where(planquota.IDEQ(req.GetId())).WithPlan().Only(ctx)
		if e == nil {
			out = planQuotaDTO(v)
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
		return QuotaErrInvalid("invalid parameter")
	}
	d := req.Data
	code, ok := ResolveQuotaCodeForWrite(d.QuotaCode, d.QuotaType)
	if !ok || d.GetPlanId() == 0 || d.QuotaValue == nil || d.GetQuotaValue() > math.MaxInt64 {
		return QuotaErrInvalid("valid plan/code/value required")
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
		return QuotaErrInvalid("id and nonempty update mask required; allow_missing forbidden")
	}
	setCode, setValue := false, false
	for _, field := range req.GetUpdateMask().GetPaths() {
		switch strings.ReplaceAll(strings.ToLower(field), "_", "") {
		case "quotacode", "quotatype":
			setCode = true
		case "quotavalue":
			setValue = true
		case "updatedby":
		default:
			return QuotaErrInvalid("immutable or unsupported plan quota field")
		}
	}
	code := ""
	if setCode {
		var ok bool
		code, ok = ResolveQuotaCodeForWrite(req.Data.QuotaCode, req.Data.QuotaType)
		if !ok {
			return QuotaErrInvalid("invalid quota code/type")
		}
	}
	if setValue && (req.Data.QuotaValue == nil || req.Data.GetQuotaValue() > math.MaxInt64) {
		return QuotaErrInvalid("valid quota value required")
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
		return QuotaErrInvalid("id required")
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

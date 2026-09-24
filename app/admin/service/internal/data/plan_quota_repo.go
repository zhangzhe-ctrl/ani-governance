package data

import (
	"context"
	"errors"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	entCrud "github.com/tx7do/go-crud/entgo"
	"github.com/tx7do/go-utils/mapper"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/planquota"
	q "go-wind-admin/app/admin/service/internal/data/quotasql"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type PlanQuotaRepo struct {
	entClient     *entCrud.EntClient[*ent.Client]
	log           *bLogger.Helper
	mapper        *mapper.CopierMapper[identityV1.PlanQuota, ent.PlanQuota]
	quotaTypeConv *mapper.EnumTypeConverter[identityV1.PlanQuota_QuotaType, planquota.QuotaType]
}

func NewPlanQuotaRepo(ctx *bootstrap.Context, c *entCrud.EntClient[*ent.Client]) *PlanQuotaRepo {
	return &PlanQuotaRepo{entClient: c, log: ctx.NewLoggerHelper("plan-quota/repo/admin-service")}
}
func (r *PlanQuotaRepo) init() {}
func (r *PlanQuotaRepo) transaction(ctx context.Context, fn func(*q.Queries) error) error {
	err := quotaTransaction(ctx, r.entClient.DB(), fn)
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && pgerr.Code == "23505" {
		return QuotaErrIdempotencyConflict("plan already has this quota code")
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return identityV1.ErrorNotFound("plan quota not found")
	}
	return err
}
func planQuotaDTO(v q.SysPlanQuota) *identityV1.PlanQuota {
	out := &identityV1.PlanQuota{Id: ptr(uint32(v.ID)), PlanId: ptr(uint32(v.PlanID)), QuotaCode: ptr(v.QuotaCode), QuotaType: ProjectLegacyTypeForRead(v.QuotaCode), QuotaValue: ptr(uint64(v.QuotaValue))}
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
	err = r.transaction(ctx, func(tx *q.Queries) error {
		rows, e := tx.ListPlanQuotas(ctx, params.ListPlanQuotasParams)
		if e != nil {
			return e
		}
		total, e := tx.CountPlanQuotas(ctx, q.CountPlanQuotasParams{Predicate: params.countPredicate, SearchTerms: params.SearchTerms})
		if e != nil {
			return e
		}
		out = &identityV1.ListPlanQuotaResponse{Total: uint64(total)}
		for _, v := range rows {
			dto := planQuotaDTO(q.SysPlanQuota(v))
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
	err = r.transaction(ctx, func(tx *q.Queries) error {
		v, e := tx.GetPlanQuota(ctx, int64(req.GetId()))
		if e == nil {
			out = planQuotaDTO(v)
			applyPlanQuotaReadMask(out, mask)
		}
		return e
	})
	return
}
func (r *PlanQuotaRepo) IsExist(ctx context.Context, id uint32) (ok bool, err error) {
	err = r.transaction(ctx, func(tx *q.Queries) error {
		_, e := tx.GetPlanQuota(ctx, int64(id))
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		ok = e == nil
		return e
	})
	return
}
func legacyTypeString(code string) *string {
	if v, ok := CodeToLegacyEntType(code); ok {
		return ptr(string(v))
	}
	return nil
}
func int64Pointer(v *uint32) *int64 {
	if v == nil {
		return nil
	}
	return ptr(int64(*v))
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
	return r.transaction(ctx, func(tx *q.Queries) error {
		if _, e := tx.LockPlanExclusive(ctx, int64(d.GetPlanId())); e != nil {
			return e
		}
		return tx.InsertPlanQuota(ctx, q.InsertPlanQuotaParams{PlanID: int64(d.GetPlanId()), QuotaCode: code, QuotaType: legacyTypeString(code), QuotaValue: int64(d.GetQuotaValue()), CreatedBy: int64Pointer(d.CreatedBy)})
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
	return r.transaction(ctx, func(tx *q.Queries) error {
		row, e := tx.GetPlanQuota(ctx, int64(req.GetId()))
		if e != nil {
			return e
		}
		if _, e = tx.LockPlanExclusive(ctx, row.PlanID); e != nil {
			return e
		}
		row, e = tx.GetPlanQuota(ctx, row.ID)
		if e != nil {
			return e
		}
		if setCode {
			row.QuotaCode = code
			row.QuotaType = legacyTypeString(code)
		}
		if setValue {
			row.QuotaValue = int64(req.Data.GetQuotaValue())
		}
		n, e := tx.UpdatePlanQuota(ctx, q.UpdatePlanQuotaParams{ID: row.ID, PlanID: row.PlanID, QuotaCode: row.QuotaCode, QuotaType: row.QuotaType, QuotaValue: row.QuotaValue, UpdatedBy: int64Pointer(req.Data.UpdatedBy)})
		if e != nil {
			return e
		}
		if n != 1 {
			return pgx.ErrNoRows
		}
		return nil
	})
}
func (r *PlanQuotaRepo) Delete(ctx context.Context, id uint32) error {
	if id == 0 {
		return QuotaErrInvalid("id required")
	}
	return r.transaction(ctx, func(tx *q.Queries) error {
		row, e := tx.GetPlanQuota(ctx, int64(id))
		if e != nil {
			return e
		}
		if _, e = tx.LockPlanExclusive(ctx, row.PlanID); e != nil {
			return e
		}
		n, e := tx.DeletePlanQuota(ctx, q.DeletePlanQuotaParams{ID: row.ID, PlanID: row.PlanID})
		if e != nil {
			return e
		}
		if n != 1 {
			return pgx.ErrNoRows
		}
		return nil
	})
}

package data

import (
	"context"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	entCrud "github.com/tx7do/go-crud/entgo"

	"github.com/tx7do/go-utils/copierutil"
	"github.com/tx7do/go-utils/mapper"
	"github.com/tx7do/go-utils/trans"

	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/plan"
	"go-wind-admin/app/admin/service/internal/data/ent/planquota"

	appViewer "go-wind-admin/pkg/entgo/viewer"
	"go-wind-admin/app/admin/service/internal/data/ent/predicate"

	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
)

type PlanQuotaRepo struct {
	entClient *entCrud.EntClient[*ent.Client]
	log       *bLogger.Helper

	mapper           *mapper.CopierMapper[identityV1.PlanQuota, ent.PlanQuota]
	quotaTypeConv    *mapper.EnumTypeConverter[identityV1.PlanQuota_QuotaType, planquota.QuotaType]

	repository *entCrud.Repository[
		ent.PlanQuotaQuery, ent.PlanQuotaSelect,
		ent.PlanQuotaCreate, ent.PlanQuotaCreateBulk,
		ent.PlanQuotaUpdate, ent.PlanQuotaUpdateOne,
		ent.PlanQuotaDelete,
		predicate.PlanQuota,
		identityV1.PlanQuota, ent.PlanQuota,
	]
}

func NewPlanQuotaRepo(
	ctx *bootstrap.Context,
	entClient *entCrud.EntClient[*ent.Client],
) *PlanQuotaRepo {
	repo := &PlanQuotaRepo{
		log:       ctx.NewLoggerHelper("plan-quota/repo/admin-service"),
		entClient: entClient,
		mapper:    mapper.NewCopierMapper[identityV1.PlanQuota, ent.PlanQuota](),
		quotaTypeConv: mapper.NewEnumTypeConverter[identityV1.PlanQuota_QuotaType, planquota.QuotaType](
			identityV1.PlanQuota_QuotaType_name, identityV1.PlanQuota_QuotaType_value,
		),
	}

	repo.init()

	return repo
}

func (r *PlanQuotaRepo) init() {
	r.repository = entCrud.NewRepository[
		ent.PlanQuotaQuery, ent.PlanQuotaSelect,
		ent.PlanQuotaCreate, ent.PlanQuotaCreateBulk,
		ent.PlanQuotaUpdate, ent.PlanQuotaUpdateOne,
		ent.PlanQuotaDelete,
		predicate.PlanQuota,
		identityV1.PlanQuota, ent.PlanQuota,
	](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())

	r.mapper.AppendConverters(r.quotaTypeConv.NewConverterPair())
}

func (r *PlanQuotaRepo) Count(ctx context.Context, whereCond []func(s *sql.Selector)) (int, error) {
	builder := r.entClient.Client().PlanQuota.Query()
	if len(whereCond) != 0 {
		builder.Modify(whereCond...)
	}

	count, err := builder.Count(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query count failed: %s", err.Error())
		return 0, identityV1.ErrorInternalServerError("query count failed")
	}

	return count, nil
}

func (r *PlanQuotaRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*identityV1.ListPlanQuotaResponse, error) {
	if req == nil {
		return nil, identityV1.ErrorBadRequest("invalid parameter")
	}

	builder := r.entClient.Client().PlanQuota.Query().WithPlan()

	whereSelectors, _, err := r.repository.BuildListSelectorWithPaging(builder, req)
	if err != nil {
		r.log.Errorf(ctx, "parse list param error [%s]", err.Error())
		return nil, identityV1.ErrorBadRequest("invalid query parameter")
	}

	entities, err := builder.All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query plan quota list failed: %s", err.Error())
		return nil, identityV1.ErrorInternalServerError("query plan quota list failed")
	}

	dtos := make([]*identityV1.PlanQuota, 0, len(entities))
	for _, entity := range entities {
		dto := r.mapper.ToDTO(entity)
		if entity.Edges.Plan != nil {
			dto.PlanId = &entity.Edges.Plan.ID
		}
		projectQuotaCompatFields(dto, entity)
		dtos = append(dtos, dto)
	}

	count, err := r.Count(ctx, whereSelectors)
	if err != nil {
		return nil, err
	}

	return &identityV1.ListPlanQuotaResponse{
		Total: uint64(count),
		Items: dtos,
	}, nil
}

func (r *PlanQuotaRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.entClient.Client().PlanQuota.Query().
		Where(planquota.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return false, identityV1.ErrorInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *PlanQuotaRepo) Get(ctx context.Context, req *identityV1.GetPlanQuotaRequest) (*identityV1.PlanQuota, error) {
	if req == nil {
		return nil, identityV1.ErrorBadRequest("invalid parameter")
	}

	builder := r.entClient.Client().PlanQuota.Query()

	var whereCond []func(s *sql.Selector)
	switch req.QueryBy.(type) {
	default:
	case *identityV1.GetPlanQuotaRequest_Id:
		whereCond = append(whereCond, planquota.IDEQ(req.GetId()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	return dto, err
}

// projectQuotaCompatFields 统一读取投影：quota_code 权威；quota_type 仅旧三项
// 保持一致旧枚举，gpu.count 等新项不输出旧枚举（nil→JSON 省略/UNSPECIFIED）。
func projectQuotaCompatFields(dto *identityV1.PlanQuota, entity *ent.PlanQuota) {
	dto.QuotaCode = trans.Ptr(entity.QuotaCode)
	dto.QuotaType = ProjectLegacyTypeForRead(entity.QuotaCode)
}

func (r *PlanQuotaRepo) Create(ctx context.Context, req *identityV1.CreatePlanQuotaRequest) (err error) {
	if req == nil || req.Data == nil {
		return identityV1.ErrorBadRequest("invalid parameter")
	}

	// 计划 §5.2/§5.3：plan_id、quota_code、quota_value 必填；
	// code/type 兼容解析集中在本映射文件，冲突/缺失回 400。
	quotaCode, ok := ResolveQuotaCodeForWrite(req.Data.QuotaCode, req.Data.QuotaType)
	if !ok {
		return QuotaErrInvalid("quota_code is required and must be consistent with legacy quota_type")
	}
	if req.Data.PlanId == nil || *req.Data.PlanId == 0 {
		return QuotaErrInvalid("plan_id is required")
	}
	if req.Data.QuotaValue == nil {
		return QuotaErrInvalid("quota_value is required")
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

	// §7.1：套餐配额变更先 plan 行 FOR UPDATE，再写该 plan 的 quota 行；
	// 不得反向再锁 tenant/account。占额路径对 plan 为 FOR SHARE，与此串行化。
	// SQLite（单写者测试库）不支持行锁，按方言跳过。
	sysCtx := appViewer.NewSystemViewerContext(ctx)
	pq := tx.Plan.Query().Where(plan.IDEQ(*req.Data.PlanId))
	if supportsRowLock(r.entClient) {
		pq = pq.ForUpdate()
	}
	if _, err = pq.Only(sysCtx); ent.IsNotFound(err) {
		return QuotaErrInvalid("plan not found")
	} else if err != nil {
		r.log.Errorf(ctx, "lock plan failed: %s", err.Error())
		return identityV1.ErrorInternalServerError("lock plan failed")
	}

	builder := tx.PlanQuota.Create().
		SetQuotaCode(quotaCode).
		SetNillableQuotaValue(req.Data.QuotaValue).
		SetNillableCreatedBy(req.Data.CreatedBy).
		SetCreatedAt(time.Now())

	// 旧三项保持旧枚举一致投影；gpu.count 等新项不写旧枚举（NULL 合法）。
	if legacyType, ok := CodeToLegacyEntType(quotaCode); ok && req.Data.QuotaType != nil {
		builder.SetQuotaType(legacyType)
	}

	if req.Data.PlanId != nil {
		builder.SetPlanID(*req.Data.PlanId)
	}

	if req.Data.Id != nil {
		builder.SetID(req.GetData().GetId())
	}

	if _, err = builder.Save(ctx); err != nil {
		r.log.Errorf(ctx, "insert plan quota failed: %s", err.Error())
		// 同套餐同 code 唯一冲突（含并发提交时数据库约束兜底）。
		if ent.IsConstraintError(err) {
			return QuotaErrIdempotencyConflict("plan already has a quota item with this quota_code")
		}
		return identityV1.ErrorInternalServerError("insert plan quota failed")
	}

	return nil
}

func (r *PlanQuotaRepo) Update(ctx context.Context, req *identityV1.UpdatePlanQuotaRequest) (err error) {
	if req == nil || req.Data == nil {
		return identityV1.ErrorBadRequest("invalid parameter")
	}
	if req.GetId() == 0 {
		return identityV1.ErrorBadRequest("id is required")
	}
	// 计划 §5.3：Update 必须非空 updateMask；不允许 allowMissing 隐式创建
	// 或改 planId。allowMissing 在 Service 层拒绝，这里再兜底。
	if req.GetAllowMissing() {
		return QuotaErrInvalid("allow_missing is not allowed for plan quotas")
	}

	// 变更 code/type 的 mask 兼容处理集中实现：两者同时出现仍须映射一致。
	// mask 路径兼容 snake_case 与 lowerCamel（FieldMask JSON 合同为 lowerCamel）。
	mask := req.GetUpdateMask().GetPaths()
	has := func(name string) bool {
		norm := strings.ReplaceAll(strings.ToLower(name), "_", "")
		for _, p := range mask {
			if strings.ReplaceAll(strings.ToLower(p), "_", "") == norm {
				return true
			}
		}
		return false
	}
	hasCode := has("quotaCode") || has("quotaType")
	hasValue := has("quotaValue")
	if has("planId") {
		return QuotaErrInvalid("plan_id cannot be changed")
	}
	var resolvedCode string
	var codeChanged bool
	if hasCode {
		resolvedCode, codeChanged = ResolveQuotaCodeForWrite(req.Data.QuotaCode, req.Data.QuotaType)
		if !codeChanged {
			return QuotaErrInvalid("quota_code is required and must be consistent with legacy quota_type")
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

	builder := tx.PlanQuota.UpdateOneID(req.GetId())
	_, err = r.repository.UpdateOne(ctx, builder, req.Data, req.GetUpdateMask(),
		func(dto *identityV1.PlanQuota) {
			if codeChanged {
				builder.SetQuotaCode(resolvedCode)
				// 旧三项保持旧枚举一致投影；改为新项目时清掉旧枚举（NULL 合法）。
				if legacyType, ok := CodeToLegacyEntType(resolvedCode); ok {
					builder.SetQuotaType(legacyType)
				} else {
					builder.ClearQuotaType()
				}
			}
			if hasValue {
				builder.SetNillableQuotaValue(req.Data.QuotaValue)
			}
			builder.
				SetNillableUpdatedBy(req.Data.UpdatedBy).
				SetUpdatedAt(time.Now())
		},
		func(s *sql.Selector) {
			s.Where(sql.EQ(planquota.FieldID, req.GetId()))
		},
	)
	if err != nil {
		r.log.Errorf(ctx, "update plan quota failed: %s", err.Error())
		if ent.IsConstraintError(err) {
			return QuotaErrIdempotencyConflict("plan already has a quota item with this quota_code")
		}
		return identityV1.ErrorInternalServerError("update plan quota failed")
	}

	return err
}

func (r *PlanQuotaRepo) Delete(ctx context.Context, id uint32) error {
	if id == 0 {
		return identityV1.ErrorBadRequest("invalid parameter")
	}

	if err := r.entClient.Client().PlanQuota.DeleteOneID(id).Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return identityV1.ErrorNotFound("plan quota not found")
		}

		r.log.Errorf(ctx, "delete one data failed: %s", err.Error())

		return identityV1.ErrorInternalServerError("delete failed")
	}

	return nil
}

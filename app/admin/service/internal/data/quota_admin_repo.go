package data

import (
	"context"
	"strconv"

	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	entCrud "github.com/tx7do/go-crud/entgo"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	quotapb "go-wind-admin/api/gen/go/quota/service/v1"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/plan"
	"go-wind-admin/app/admin/service/internal/data/ent/planquota"
	"go-wind-admin/app/admin/service/internal/data/ent/quotaaccount"
	"go-wind-admin/app/admin/service/internal/data/ent/quotadefinition"
	"go-wind-admin/app/admin/service/internal/data/ent/tenant"

	appViewer "go-wind-admin/pkg/entgo/viewer"
)

// QuotaAdminRepo 提供配额目录与租户账户的读取（QUOTA-01/02）。
// 目录是平台配置元数据；账户按租户读取，占额不等于真实运行资源数量。
type QuotaAdminRepo struct {
	entClient *entCrud.EntClient[*ent.Client]
	log       *bLogger.Helper
}

func NewQuotaAdminRepo(
	ctx *bootstrap.Context,
	entClient *entCrud.EntClient[*ent.Client],
) *QuotaAdminRepo {
	return &QuotaAdminRepo{
		entClient: entClient,
		log:       ctx.NewLoggerHelper("quota-admin/repo/admin-service"),
	}
}

// ListDefinitions 返回配额目录（只读；目录新增走显式版本数据脚本）。
func (r *QuotaAdminRepo) ListDefinitions(ctx context.Context, req *paginationV1.PagingRequest) (*adminV1.ListQuotaDefinitionsResponse, error) {
	sysCtx := appViewer.NewSystemViewerContext(ctx)

	query := r.entClient.Client().QuotaDefinition.Query().
		Order(ent.Asc(quotadefinition.FieldCode))

	// 目录规模固定且极小（本批 4 项），分页沿用现有合同。
	if req != nil && req.GetPageSize() > 0 {
		offset := int(req.GetPage() - 1)
		if offset < 0 {
			offset = 0
		}
		query.Offset(offset).Limit(int(req.GetPageSize()))
	}

	defs, err := query.All(sysCtx)
	if err != nil {
		r.log.Errorf(ctx, "list quota definitions failed: %s", err.Error())
		return nil, QuotaErrStorageUnavailable("query quota definitions failed")
	}
	total, err := r.entClient.Client().QuotaDefinition.Query().Count(sysCtx)
	if err != nil {
		r.log.Errorf(ctx, "count quota definitions failed: %s", err.Error())
		return nil, QuotaErrStorageUnavailable("count quota definitions failed")
	}

	items := make([]*quotapb.QuotaDefinition, 0, len(defs))
	for _, d := range defs {
		items = append(items, &quotapb.QuotaDefinition{
			Code:           d.Code,
			DisplayName:    d.DisplayName,
			Unit:           d.Unit,
			AccountingKind: mapAccountingKind(d.AccountingKind),
			Enforcement:    DeriveEnforcement(d.Code),
		})
	}
	return &adminV1.ListQuotaDefinitionsResponse{Items: items, Total: uint64(total)}, nil
}

// ListTenantAccounts 汇总指定租户的配额账户视图：
// limit 来自当前套餐政策（缺项=0，表示不允许申请，不是无限制），
// occupied 来自账本，available=max(limit-occupied,0)。
// 展示的是"已占额度"，不是真实运行 GPU 数。
func (r *QuotaAdminRepo) ListTenantAccounts(ctx context.Context, tenantId uint32) (*adminV1.ListTenantQuotaAccountsResponse, error) {
	sysCtx := appViewer.NewSystemViewerContext(ctx)

	t, err := r.entClient.Client().Tenant.Query().
		Where(tenant.IDEQ(tenantId)).
		WithPlan(func(q *ent.PlanQuery) { q.WithQuotas() }).
		Only(sysCtx)
	if ent.IsNotFound(err) {
		return nil, QuotaErrNotFound("tenant not found")
	} else if err != nil {
		r.log.Errorf(ctx, "list tenant accounts: tenant %d query failed: %s", tenantId, err.Error())
		return nil, QuotaErrStorageUnavailable("query tenant failed")
	}

	// 当前套餐政策：quota_code -> value
	planLimits := map[string]uint64{}
	if t.Edges.Plan != nil {
		for _, pq := range t.Edges.Plan.Edges.Quotas {
			if pq.QuotaValue != nil {
				planLimits[pq.QuotaCode] = *pq.QuotaValue
			}
		}
	}

	accounts, err := r.entClient.Client().QuotaAccount.Query().
		Where(quotaaccount.TenantIDEQ(tenantId)).
		All(sysCtx)
	if err != nil {
		r.log.Errorf(ctx, "list tenant accounts: accounts query failed: %s", err.Error())
		return nil, QuotaErrStorageUnavailable("query quota accounts failed")
	}
	occupiedByCode := map[string]int64{}
	for _, a := range accounts {
		occupiedByCode[a.QuotaCode] = a.OccupiedUnits
	}

	defs, err := r.entClient.Client().QuotaDefinition.Query().
		Order(ent.Asc(quotadefinition.FieldCode)).
		All(sysCtx)
	if err != nil {
		r.log.Errorf(ctx, "list tenant accounts: definitions query failed: %s", err.Error())
		return nil, QuotaErrStorageUnavailable("query quota definitions failed")
	}

	items := make([]*quotapb.QuotaAccountView, 0, len(defs))
	for _, d := range defs {
		limit := planLimits[d.Code] // 政策缺项=0
		occupied := occupiedByCode[d.Code]
		available := uint64(0)
		if occupied >= 0 && uint64(occupied) < limit {
			available = limit - uint64(occupied)
		}
		items = append(items, &quotapb.QuotaAccountView{
			QuotaCode:   d.Code,
			Unit:        d.Unit,
			Limit:       strconv.FormatUint(limit, 10),
			Occupied:    strconv.FormatInt(occupied, 10),
			Available:   strconv.FormatUint(available, 10),
			OverLimit:   occupied > int64(limit),
			Enforcement: DeriveEnforcement(d.Code),
		})
	}
	return &adminV1.ListTenantQuotaAccountsResponse{TenantId: tenantId, Items: items}, nil
}

// HasPlanQuotaPolicy 判断套餐是否配置了指定配额编码（占额事务与读取共用）。
func (r *QuotaAdminRepo) HasPlanQuotaPolicy(ctx context.Context, planId uint32, code string) (bool, error) {
	sysCtx := appViewer.NewSystemViewerContext(ctx)
	exist, err := r.entClient.Client().PlanQuota.Query().
		Where(planquota.HasPlanWith(plan.IDEQ(planId)), planquota.QuotaCodeEQ(code)).
		Exist(sysCtx)
	if err != nil {
		return false, err
	}
	return exist, nil
}

// mapAccountingKind 将 ent 计数模型映射到 proto。
func mapAccountingKind(kind quotadefinition.AccountingKind) quotapb.AccountingKind {
	switch kind {
	case quotadefinition.AccountingKindConcurrent:
		return quotapb.AccountingKind_CONCURRENT
	case quotadefinition.AccountingKindCounter:
		return quotapb.AccountingKind_COUNTER
	default:
		return quotapb.AccountingKind_ACCOUNTING_KIND_UNSPECIFIED
	}
}

// DeriveEnforcement 从编码推导执行能力（§5.1：执行能力由已注册适配器决定，
// 不落库、不能由前端提交获得）。旧三项=LEGACY_CONFIG_ONLY；其余（gpu.count 等）
// 在正式构建没有生产执行器，固定 LAB_ONLY。
func DeriveEnforcement(code string) quotapb.Enforcement {
	if IsLegacyQuotaCode(code) {
		return quotapb.Enforcement_LEGACY_CONFIG_ONLY
	}
	return quotapb.Enforcement_LAB_ONLY
}

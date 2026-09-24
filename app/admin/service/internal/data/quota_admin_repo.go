package data

import (
	"context"
	"strconv"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	entCrud "github.com/tx7do/go-crud/entgo"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

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

type QuotaAdminRepo struct {
	entClient    *entCrud.EntClient[*ent.Client]
	log          *bLogger.Helper
	capabilities QuotaExecutionCapabilities
}

// QuotaExecutionCapabilities is supplied only by controlled owner/action
// composition, before serving requests. Database policies cannot implement it.
type QuotaExecutionCapabilities interface{ EnforcesQuota(code string) bool }

func (r *QuotaAdminRepo) SetExecutionCapabilities(c QuotaExecutionCapabilities) { r.capabilities = c }
func (r *QuotaAdminRepo) enforcement(code string) quotapb.Enforcement {
	if isGPUCode(code) && r.capabilities != nil && r.capabilities.EnforcesQuota(code) {
		return quotapb.Enforcement_ENFORCED
	}
	return DeriveEnforcement(code)
}

func NewQuotaAdminRepo(ctx *bootstrap.Context, c *entCrud.EntClient[*ent.Client]) *QuotaAdminRepo {
	return &QuotaAdminRepo{entClient: c, log: ctx.NewLoggerHelper("quota-admin/repo/admin-service")}
}
func (r *QuotaAdminRepo) ListDefinitions(ctx context.Context, req *paginationV1.PagingRequest) (out *adminV1.ListQuotaDefinitionsResponse, err error) {
	err = quotaTransaction(ctx, r.entClient.Client(), func(tx *ent.Tx) error {
		ctx := appViewer.NewSystemViewerContext(ctx)
		defs, e := tx.QuotaDefinition.Query().Order(ent.Asc(quotadefinition.FieldCode)).All(ctx)
		if e != nil {
			return e
		}
		out = &adminV1.ListQuotaDefinitionsResponse{Total: uint64(len(defs))}
		start, end := 0, len(defs)
		if req != nil && !req.GetNoPaging() && req.GetPageSize() > 0 {
			page := uint64(req.GetPage())
			if page < 1 {
				page = 1
			}
			start = int(min((page-1)*uint64(req.GetPageSize()), uint64(len(defs))))
			end = int(min(uint64(start)+uint64(req.GetPageSize()), uint64(len(defs))))
		} else if req != nil && !req.GetNoPaging() && req.Offset != nil && req.Limit != nil {
			start = int(min(req.GetOffset(), uint64(len(defs))))
			end = int(min(uint64(start)+uint64(req.GetLimit()), uint64(len(defs))))
		}
		for _, d := range defs[start:end] {
			out.Items = append(out.Items, &quotapb.QuotaDefinition{Code: d.Code, DisplayName: d.DisplayName, Unit: d.Unit, AccountingKind: mapAccountingKind(string(d.AccountingKind)), Enforcement: r.enforcement(d.Code)})
		}
		return nil
	})
	return
}
func (r *QuotaAdminRepo) ListTenantAccounts(ctx context.Context, tid uint32) (out *adminV1.ListTenantQuotaAccountsResponse, err error) {
	if tid == 0 {
		return nil, QuotaErrInvalid("tenant required")
	}
	err = quotaTransaction(ctx, r.entClient.Client(), func(tx *ent.Tx) error {
		ctx := appViewer.NewSystemViewerContext(ctx)
		t, e := tx.Tenant.Query().Where(tenant.IDEQ(tid), tenant.IDGT(0)).Only(ctx)
		if e != nil {
			return e
		}
		limits := map[string]int64{}
		if t.PlanID != nil {
			policies, e := tx.PlanQuota.Query().Where(planquota.HasPlanWith(plan.IDEQ(*t.PlanID))).Order(ent.Asc(planquota.FieldQuotaCode)).All(ctx)
			if e != nil {
				return e
			}
			for _, p := range policies {
				limits[p.QuotaCode] = int64(*p.QuotaValue)
			}
		}
		accs, e := tx.QuotaAccount.Query().Where(quotaaccount.TenantIDEQ(tid)).Order(ent.Asc(quotaaccount.FieldQuotaCode)).All(ctx)
		if e != nil {
			return e
		}
		occupied := map[string]int64{}
		for _, a := range accs {
			occupied[a.QuotaCode] = a.OccupiedUnits
		}
		defs, e := tx.QuotaDefinition.Query().Order(ent.Asc(quotadefinition.FieldCode)).All(ctx)
		if e != nil {
			return e
		}
		out = &adminV1.ListTenantQuotaAccountsResponse{TenantId: tid}
		for _, d := range defs {
			limit, occ := limits[d.Code], occupied[d.Code]
			available := limit - occ
			if available < 0 {
				available = 0
			}
			out.Items = append(out.Items, &quotapb.QuotaAccountView{QuotaCode: d.Code, Unit: d.Unit, Limit: strconv.FormatInt(limit, 10), Occupied: strconv.FormatInt(occ, 10), Available: strconv.FormatInt(available, 10), OverLimit: occ > limit, Enforcement: r.enforcement(d.Code)})
		}
		return nil
	})
	return
}
func (r *QuotaAdminRepo) HasPlanQuotaPolicy(ctx context.Context, id uint32, code string) (ok bool, err error) {
	err = quotaTransaction(ctx, r.entClient.Client(), func(tx *ent.Tx) error {
		ctx := appViewer.NewSystemViewerContext(ctx)
		rows, e := tx.PlanQuota.Query().Where(planquota.HasPlanWith(plan.IDEQ(id))).Order(ent.Asc(planquota.FieldQuotaCode)).All(ctx)
		if e != nil {
			return e
		}
		for _, p := range rows {
			if p.QuotaCode == code {
				ok = true
			}
		}
		return nil
	})
	return
}
func mapAccountingKind(kind string) quotapb.AccountingKind {
	switch kind {
	case "CONCURRENT":
		return quotapb.AccountingKind_CONCURRENT
	case "COUNTER":
		return quotapb.AccountingKind_COUNTER
	default:
		return quotapb.AccountingKind_ACCOUNTING_KIND_UNSPECIFIED
	}
}

// Execution capability is supplied by controlled owner/action assembly; a
// directory row, plan policy, or client request can never enable enforcement.
func DeriveEnforcement(code string) quotapb.Enforcement {
	if IsLegacyQuotaCode(code) {
		return quotapb.Enforcement_LEGACY_CONFIG_ONLY
	}
	if code == "gpu.count" {
		return quotapb.Enforcement_LAB_ONLY
	}
	return quotapb.Enforcement_NOT_ENABLED
}

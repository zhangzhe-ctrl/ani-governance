package data

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"github.com/google/uuid"
	entCrud "github.com/tx7do/go-crud/entgo"
	"github.com/tx7do/go-utils/trans"

	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/plan"
	"go-wind-admin/app/admin/service/internal/data/ent/planquota"
	"go-wind-admin/app/admin/service/internal/data/ent/quotaaccount"
	"go-wind-admin/app/admin/service/internal/data/ent/quotacharge"
	"go-wind-admin/app/admin/service/internal/data/ent/quotaoperation"
	"go-wind-admin/app/admin/service/internal/data/ent/quotareleasereceipt"
	"go-wind-admin/app/admin/service/internal/data/ent/tenant"

	appViewer "go-wind-admin/pkg/entgo/viewer"
)

// QuotaLedgerRepo 实现单次占额、累计释放与未发撤销的纯数据库事务（计划 §7/§9/§8.3）。
// 不使用 Redis/内存计数；锁顺序按 §7.1 固定；网络 RPC 不出现在任何事务内。
type QuotaLedgerRepo struct {
	entClient *entCrud.EntClient[*ent.Client]
	log       *bLogger.Helper
}

func NewQuotaLedgerRepo(
	ctx *bootstrap.Context,
	entClient *entCrud.EntClient[*ent.Client],
) *QuotaLedgerRepo {
	return &QuotaLedgerRepo{
		entClient: entClient,
		log:       ctx.NewLoggerHelper("quota-ledger/repo/admin-service"),
	}
}

// ── 输入/输出合同 ─────────────────────────────────────────────

// QuotaOccupyItem 一次占额中的一项配额。
type QuotaOccupyItem struct {
	QuotaCode string
	Units     int64
}

// QuotaOccupyInput 占额输入；字段在提交后不可改写（§6.3）。
type QuotaOccupyInput struct {
	TenantID         uint32
	ResourceTenantID string // 持久 resource_tenant_id（可信 Principal 解析）
	ResourceID       string // 创建操作随原操作持久化的稳定资源 ID
	ActorType        string
	ActorID          string
	OwnerService     string
	Action           string
	IdempotencyKey   string // 必须为 UUID
	RequestHash      string // 规范请求哈希（不含 request-id/时间戳/新 UUID）
	CanonicalRequest string // 经校验业务参数（schema_version=1）
	Items            []QuotaOccupyItem
}

// QuotaOccupyResult 占额结果。
type QuotaOccupyResult struct {
	OperationID string
	ResourceID  string
	ChargeIDs   []string // 与输入 Items 顺序一致
	Replayed    bool     // 命中同内容幂等记录
}

// QuotaReleaseInput 累计释放输入（协议形态已由 server 层校验）。
type QuotaReleaseInput struct {
	OwnerService   string // 来自已验证证书精确 SAN
	ReleaseEventID string
	OperationID    string
	Reason         string
	PayloadHash    string
	PayloadJSON    string
	Items          []QuotaReleaseItemInput
}

// QuotaReleaseItemInput 单笔累计释放。
type QuotaReleaseItemInput struct {
	ChargeID      string
	QuotaCode     string
	ReleasedTotal int64
}

// QuotaReleaseResult 单笔回执结果。
type QuotaReleaseResult struct {
	ChargeID      string
	AppliedDelta  int64
	ReleasedTotal int64
}

func generateUUID() string { return uuid.NewString() }

// supportsRowLock 仅 PostgreSQL 支持 FOR UPDATE/FOR SHARE；
// SQLite（测试内存库）单写者无并发，跳过行锁方言分支。
func supportsRowLock(c *entCrud.EntClient[*ent.Client]) bool {
	return c.Driver() != nil && c.Driver().Dialect() == "postgres"
}

// ── 一次占额事务（§7.2） ──────────────────────────────────────

// Occupy 在一个 PostgreSQL 事务内完成：锁 tenant → FOR SHARE plan →
// 幂等检查 → 目录/政策校验 → account 检查并增量 → 保存 operation(QUEUED)+charges。
// 提交成功前不产生任何下游调用；提交失败禁止转发（由调用方保证）。
func (r *QuotaLedgerRepo) Occupy(ctx context.Context, in *QuotaOccupyInput) (*QuotaOccupyResult, error) {
	sysCtx := appViewer.NewSystemViewerContext(ctx)

	if in == nil || len(in.Items) == 0 {
		return nil, QuotaErrInvalid("empty quota items")
	}
	if in.IdempotencyKey == "" || in.RequestHash == "" || in.CanonicalRequest == "" {
		return nil, QuotaErrInvalid("idempotency key, request hash and canonical request are required")
	}
	if in.ResourceTenantID == "" || in.ResourceID == "" || in.OwnerService == "" || in.Action == "" {
		return nil, QuotaErrInvalid("resource tenant, resource id, owner service and action are required")
	}
	for _, it := range in.Items {
		if it.Units <= 0 {
			return nil, QuotaErrInvalid("quota units must be positive")
		}
		if it.QuotaCode == "" {
			return nil, QuotaErrInvalid("quota_code is required")
		}
	}

	tx, err := r.entClient.Client().Tx(sysCtx)
	if err != nil {
		r.log.Errorf(ctx, "occupy: start tx failed: %s", err.Error())
		return nil, QuotaErrStorageUnavailable("start transaction failed")
	}
	rollback := func() { _ = tx.Rollback() }
	rowLock := supportsRowLock(r.entClient)

	// 1. tenant 行 FOR UPDATE：准入与政策读串行化。
	tq := tx.Tenant.Query().Where(tenant.IDEQ(in.TenantID))
	if supportsRowLock(r.entClient) {
		tq = tq.ForUpdate()
	}
	t, err := tq.Only(sysCtx)
	if ent.IsNotFound(err) {
		rollback()
		return nil, QuotaErrAdmissionDenied("tenant not found")
	} else if err != nil {
		rollback()
		r.log.Errorf(ctx, "occupy: lock tenant failed: %s", err.Error())
		return nil, QuotaErrStorageUnavailable("lock tenant failed")
	}
	if t.Status == nil || *t.Status != tenant.StatusOn {
		rollback()
		return nil, QuotaErrAdmissionDenied("tenant is not active")
	}
	// 到期：expired_at 为 nil 或数据库当前时间严格小于到期时间；相等视为到期。
	// 无论 READONLY/BLOCK_LOGIN/FREEZE，新占额一律拒绝，不等定时任务。
	if t.ExpiredAt != nil && !t.ExpiredAt.IsZero() && !t.ExpiredAt.After(time.Now()) {
		rollback()
		return nil, QuotaErrAdmissionDenied("tenant subscription expired")
	}
	planId := uint32(0)
	if t.PlanID != nil {
		planId = *t.PlanID
	}
	if planId == 0 {
		rollback()
		return nil, QuotaErrNotConfigured("tenant has no subscription plan")
	}

	// 2. 当前 plan 行 FOR SHARE：政策写入（FOR UPDATE）与占额读相互串行化。
	plq := tx.Plan.Query().Where(plan.IDEQ(planId))
	if supportsRowLock(r.entClient) {
		plq = plq.ForShare()
	}
	if _, err = plq.Only(sysCtx); ent.IsNotFound(err) {
		rollback()
		return nil, QuotaErrNotConfigured("plan not found")
	} else if err != nil {
		rollback()
		r.log.Errorf(ctx, "occupy: lock plan failed: %s", err.Error())
		return nil, QuotaErrStorageUnavailable("lock plan failed")
	}

	// 3. 幂等：同 (tenant,actor_type,actor_id,action,idempotency_key) 记录。
	existing, err := tx.QuotaOperation.Query().
		Where(
			quotaoperation.TenantIDEQ(in.TenantID),
			quotaoperation.ActorTypeEQ(in.ActorType),
			quotaoperation.ActorIDEQ(in.ActorID),
			quotaoperation.ActionEQ(in.Action),
			quotaoperation.IdempotencyKeyEQ(in.IdempotencyKey),
		).
		Only(sysCtx)
	if err == nil {
		// 命中：内容一致返回既有 operation；不同则冲突。不得再次扣额。
		if existing.RequestHash != in.RequestHash {
			rollback()
			return nil, QuotaErrIdempotencyConflict("idempotency key reused with different payload")
		}
		existingCharges, cerr := tx.QuotaCharge.Query().
			Where(quotacharge.TenantIDEQ(in.TenantID), quotacharge.OperationIDEQ(existing.OperationID)).
			All(sysCtx)
		if cerr != nil {
			rollback()
			r.log.Errorf(ctx, "occupy: query existing charges failed: %s", cerr.Error())
			return nil, QuotaErrStorageUnavailable("query charges failed")
		}
		chargeIDs := make([]string, 0, len(existingCharges))
		for _, c := range existingCharges {
			chargeIDs = append(chargeIDs, c.ChargeID)
		}
		rollback()
		return &QuotaOccupyResult{
			OperationID: existing.OperationID,
			ResourceID:  existing.ResourceID,
			ChargeIDs:   chargeIDs,
			Replayed:    true,
		}, nil
	} else if !ent.IsNotFound(err) {
		rollback()
		r.log.Errorf(ctx, "occupy: idempotency lookup failed: %s", err.Error())
		return nil, QuotaErrStorageUnavailable("idempotency lookup failed")
	}

	// 4. 对每个配额项（按 quota_code 排序）检查政策并锁定账户。
	items := make([]QuotaOccupyItem, len(in.Items))
	copy(items, in.Items)
	sort.Slice(items, func(i, j int) bool { return items[i].QuotaCode < items[j].QuotaCode })

	policies, perr := tx.PlanQuota.Query().
		Where(planquota.HasPlanWith(plan.IDEQ(planId))).
		All(sysCtx)
	if perr != nil {
		rollback()
		r.log.Errorf(ctx, "occupy: query plan policies failed: %s", perr.Error())
		return nil, QuotaErrStorageUnavailable("query plan policies failed")
	}
	policyByCode := make(map[string]int64, len(policies))
	for _, pq := range policies {
		if pq.QuotaValue != nil {
			policyByCode[pq.QuotaCode] = int64(*pq.QuotaValue)
		}
	}
	limits := make(map[string]int64, len(items))
	for _, it := range items {
		limit, ok := policyByCode[it.QuotaCode]
		if !ok {
			// 缺少配额项表示不允许申请，不能解释为无限制。
			rollback()
			return nil, QuotaErrNotConfigured("quota not configured in plan: " + it.QuotaCode)
		}
		limits[it.QuotaCode] = limit
	}

	accounts := make(map[string]*ent.QuotaAccount, len(items))
	for _, it := range items {
		acc, aerr := lockOrCreateAccount(sysCtx, tx, in.TenantID, it.QuotaCode, rowLock)
		if aerr != nil {
			rollback()
			r.log.Errorf(ctx, "occupy: lock account failed: %s", aerr.Error())
			return nil, QuotaErrStorageUnavailable("lock quota account failed")
		}
		accounts[it.QuotaCode] = acc
		// 安全算术：occupied+units<=limit（数量均为非负 int64，减法无溢出）。
		if acc.OccupiedUnits > limits[it.QuotaCode]-it.Units {
			rollback()
			return nil, QuotaErrExceeded("quota exceeded for " + it.QuotaCode)
		}
	}

	// 5. 保存 operation(QUEUED)、charges 与 occupied 增量。
	op, err := tx.QuotaOperation.Create().
		SetOperationID(generateUUID()).
		SetTenantID(in.TenantID).
		SetResourceTenantID(in.ResourceTenantID).
		SetResourceID(in.ResourceID).
		SetActorType(in.ActorType).
		SetActorID(in.ActorID).
		SetOwnerService(in.OwnerService).
		SetAction(in.Action).
		SetIdempotencyKey(in.IdempotencyKey).
		SetRequestHash(in.RequestHash).
		SetCanonicalRequest(in.CanonicalRequest).
		SetDispatchState(quotaoperation.DispatchStateQueued).
		Save(sysCtx)
	if err != nil {
		rollback()
		if ent.IsConstraintError(err) {
			// 幂等唯一键并发竞争：唯一约束兜底。
			return nil, QuotaErrIdempotencyConflict("concurrent idempotency conflict")
		}
		r.log.Errorf(ctx, "occupy: save operation failed: %s", err.Error())
		return nil, QuotaErrStorageUnavailable("save operation failed")
	}

	chargeIDs := make([]string, 0, len(items))
	for _, it := range items {
		charge, cerr := tx.QuotaCharge.Create().
			SetChargeID(generateUUID()).
			SetTenantID(in.TenantID).
			SetOperationID(op.OperationID).
			SetQuotaCode(it.QuotaCode).
			SetOriginalUnits(it.Units).
			SetReleasedUnits(0).
			Save(sysCtx)
		if cerr != nil {
			rollback()
			r.log.Errorf(ctx, "occupy: save charge failed: %s", cerr.Error())
			return nil, QuotaErrStorageUnavailable("save charge failed")
		}
		chargeIDs = append(chargeIDs, charge.ChargeID)

		acc := accounts[it.QuotaCode]
		if _, uerr := tx.QuotaAccount.UpdateOneID(acc.ID).
			SetOccupiedUnits(acc.OccupiedUnits + it.Units).
			AddVersion(1).
			Save(sysCtx); uerr != nil {
			rollback()
			r.log.Errorf(ctx, "occupy: update account failed: %s", uerr.Error())
			return nil, QuotaErrStorageUnavailable("update quota account failed")
		}
	}

	if err = tx.Commit(); err != nil {
		r.log.Errorf(ctx, "occupy: commit failed: %s", err.Error())
		return nil, QuotaErrStorageUnavailable("commit failed")
	}
	return &QuotaOccupyResult{OperationID: op.OperationID, ResourceID: in.ResourceID, ChargeIDs: chargeIDs}, nil
}

// lockOrCreateAccount 读取并锁定账户；不存在时插入（唯一冲突后重读锁定）。
func lockOrCreateAccount(ctx context.Context, tx *ent.Tx, tenantId uint32, code string, rowLock bool) (*ent.QuotaAccount, error) {
	aq := tx.QuotaAccount.Query().
		Where(quotaaccount.TenantIDEQ(tenantId), quotaaccount.QuotaCodeEQ(code))
	if rowLock {
		aq = aq.ForUpdate()
	}
	acc, err := aq.Only(ctx)
	if err == nil {
		return acc, nil
	}
	if !ent.IsNotFound(err) {
		return nil, err
	}
	if err = tx.QuotaAccount.Create().
		SetTenantID(tenantId).
		SetQuotaCode(code).
		SetOccupiedUnits(0).
		SetVersion(0).
		OnConflict().
		DoNothing().
		Exec(ctx); err != nil {
		return nil, err
	}
	rq := tx.QuotaAccount.Query().
		Where(quotaaccount.TenantIDEQ(tenantId), quotaaccount.QuotaCodeEQ(code))
	if rowLock {
		rq = rq.ForUpdate()
	}
	return rq.Only(ctx)
}

// ── 累计释放（§9.2） ─────────────────────────────────────────

// Release 处理内部 mTLS 退额 RPC 的账本事务。
// incoming_total 是自创建以来累计可退还数量：new_total=max(stored,incoming)；
// delta=new_total-stored；occupied-=delta。任一非法整笔回滚。
// 不检查套餐到期、租户状态、余额是否超限或原用户 token；仍严格验证 owner 与账本归属。
func (r *QuotaLedgerRepo) Release(ctx context.Context, in *QuotaReleaseInput) ([]QuotaReleaseResult, error) {
	sysCtx := appViewer.NewSystemViewerContext(ctx)

	if in == nil || len(in.Items) == 0 {
		return nil, releaseErrInvalid("items must not be empty")
	}

	// 0. 先按不可变 charge 定位 tenant（§7.1 退额锁顺序）。
	charges0, err := r.entClient.Client().QuotaCharge.Query().
		Where(quotacharge.ChargeIDIn(chargeIDsOf(in.Items)...)).
		All(sysCtx)
	if err != nil {
		r.log.Errorf(ctx, "release: locate charges failed: %s", err.Error())
		return nil, releaseErrUnavailable("locate charges failed")
	}
	if len(charges0) != len(in.Items) {
		return nil, releaseErrNotFound("unknown charge")
	}
	tenantId := derefUint32(charges0[0].TenantID)

	tx, err := r.entClient.Client().Tx(sysCtx)
	if err != nil {
		r.log.Errorf(ctx, "release: start tx failed: %s", err.Error())
		return nil, releaseErrUnavailable("start transaction failed")
	}
	rollback := func() { _ = tx.Rollback() }
	rowLock := supportsRowLock(r.entClient)

	// 1. tenant 行 FOR UPDATE。
	relq := tx.Tenant.Query().Where(tenant.IDEQ(tenantId))
	if rowLock {
		relq = relq.ForUpdate()
	}
	if _, err = relq.Only(sysCtx); err != nil {
		rollback()
		r.log.Errorf(ctx, "release: lock tenant failed: %s", err.Error())
		return nil, releaseErrUnavailable("lock tenant failed")
	}

	// 2. 原创建 operation 必须存在且属于同租户；owner 从证书取出。
	op, err := tx.QuotaOperation.Query().
		Where(
			quotaoperation.OperationIDEQ(in.OperationID),
			quotaoperation.TenantIDEQ(tenantId),
		).
		Only(sysCtx)
	if ent.IsNotFound(err) {
		rollback()
		return nil, releaseErrNotFound("unknown operation")
	} else if err != nil {
		rollback()
		r.log.Errorf(ctx, "release: query operation failed: %s", err.Error())
		return nil, releaseErrUnavailable("query operation failed")
	}
	// 同 CA 的另一服务不能退他人额度。
	if op.OwnerService != in.OwnerService {
		rollback()
		return nil, releaseErrPermissionDenied("release not allowed for this owner")
	}

	// 3. 回执幂等：同 event 同内容返回当前权威累计；同 ID 不同内容冲突。
	existingReceipt, err := tx.QuotaReleaseReceipt.Query().
		Where(
			quotareleasereceipt.OwnerServiceEQ(in.OwnerService),
			quotareleasereceipt.ReleaseEventIDEQ(in.ReleaseEventID),
		).
		Only(sysCtx)
	if err == nil {
		if existingReceipt.PayloadHash != in.PayloadHash {
			rollback()
			return nil, releaseErrConflict("release_event_id reused with different payload")
		}
		results, gerr := authoritativeTotals(sysCtx, tx, tenantId, in.Items)
		if gerr != nil {
			rollback()
			return nil, gerr
		}
		rollback()
		return results, nil
	} else if !ent.IsNotFound(err) {
		rollback()
		r.log.Errorf(ctx, "release: receipt lookup failed: %s", err.Error())
		return nil, releaseErrUnavailable("receipt lookup failed")
	}

	// 4. 校验并处理各 item（原子：任一非法整笔回滚）。
	sort.Slice(in.Items, func(i, j int) bool { return in.Items[i].ChargeID < in.Items[j].ChargeID })
	results := make([]QuotaReleaseResult, 0, len(in.Items))
	occupiedDeltaByAccount := make(map[uint32]int64, len(in.Items))
	for _, item := range in.Items {
		cq := tx.QuotaCharge.Query().
			Where(
				quotacharge.ChargeIDEQ(item.ChargeID),
				quotacharge.TenantIDEQ(tenantId),
			)
		if rowLock {
			cq = cq.ForUpdate()
		}
		charge, cerr := cq.Only(sysCtx)
		if ent.IsNotFound(cerr) {
			rollback()
			return nil, releaseErrNotFound("unknown charge")
		} else if cerr != nil {
			rollback()
			r.log.Errorf(ctx, "release: lock charge failed: %s", cerr.Error())
			return nil, releaseErrUnavailable("lock charge failed")
		}
		// 计量 code 不匹配 / 跨 operation 的 charge 一律拒绝。
		if charge.QuotaCode != item.QuotaCode {
			rollback()
			return nil, releaseErrConflict("quota_code mismatch for charge " + item.ChargeID)
		}
		if charge.OperationID != in.OperationID {
			rollback()
			return nil, releaseErrConflict("charge does not belong to operation " + in.OperationID)
		}
		// 0<=incoming<=original。
		if item.ReleasedTotal < 0 || item.ReleasedTotal > charge.OriginalUnits {
			rollback()
			return nil, releaseErrConflict("released_total out of range for charge " + item.ChargeID)
		}
		newTotal := charge.ReleasedUnits
		if item.ReleasedTotal > newTotal {
			newTotal = item.ReleasedTotal
		}
		delta := newTotal - charge.ReleasedUnits
		if delta > 0 {
			if _, uerr := tx.QuotaCharge.UpdateOneID(charge.ID).
				SetReleasedUnits(newTotal).
				Save(sysCtx); uerr != nil {
				rollback()
				r.log.Errorf(ctx, "release: update charge failed: %s", uerr.Error())
				return nil, releaseErrUnavailable("update charge failed")
			}
			auq := tx.QuotaAccount.Query().
				Where(quotaaccount.TenantIDEQ(tenantId), quotaaccount.QuotaCodeEQ(charge.QuotaCode))
			if rowLock {
				auq = auq.ForUpdate()
			}
			acc, aerr := auq.Only(sysCtx)
			if aerr != nil {
				rollback()
				r.log.Errorf(ctx, "release: lock account failed: %s", aerr.Error())
				return nil, releaseErrUnavailable("lock quota account failed")
			}
			if acc.OccupiedUnits-delta < 0 {
				rollback()
				return nil, releaseErrConflict("release would make occupied negative")
			}
			occupiedDeltaByAccount[acc.ID] += delta
		}
		results = append(results, QuotaReleaseResult{
			ChargeID:      charge.ChargeID,
			AppliedDelta:  delta,
			ReleasedTotal: newTotal,
		})
	}

	for accID, delta := range occupiedDeltaByAccount {
		if delta > 0 {
			if _, uerr := tx.QuotaAccount.UpdateOneID(accID).
				AddOccupiedUnits(-delta).
				AddVersion(1).
				Save(sysCtx); uerr != nil {
				rollback()
				r.log.Errorf(ctx, "release: update account failed: %s", uerr.Error())
				return nil, releaseErrUnavailable("update quota account failed")
			}
		}
	}

	// 5. 回执与账本修改同事务提交（数据库错误返回 Unavailable，owner 保留重试）。
	if _, err = tx.QuotaReleaseReceipt.Create().
		SetReceiptID(generateUUID()).
		SetTenantID(tenantId).
		SetOwnerService(in.OwnerService).
		SetReleaseEventID(in.ReleaseEventID).
		SetPayloadHash(in.PayloadHash).
		SetPayloadJSON(in.PayloadJSON).
		Save(sysCtx); err != nil {
		rollback()
		r.log.Errorf(ctx, "release: save receipt failed: %s", err.Error())
		return nil, releaseErrUnavailable("save receipt failed")
	}

	if err = tx.Commit(); err != nil {
		r.log.Errorf(ctx, "release: commit failed: %s", err.Error())
		return nil, releaseErrUnavailable("commit failed")
	}
	return results, nil
}

// authoritativeTotals 幂等重放路径：返回各 charge 当前权威累计（不同 event_id
// 携带相同累计值不重复退额；旧累计值晚到是合法 no-op）。
func authoritativeTotals(ctx context.Context, tx *ent.Tx, tenantId uint32, items []QuotaReleaseItemInput) ([]QuotaReleaseResult, error) {
	results := make([]QuotaReleaseResult, 0, len(items))
	for _, item := range items {
		charge, err := tx.QuotaCharge.Query().
			Where(quotacharge.ChargeIDEQ(item.ChargeID), quotacharge.TenantIDEQ(tenantId)).
			Only(ctx)
		if ent.IsNotFound(err) {
			return nil, releaseErrNotFound("unknown charge")
		} else if err != nil {
			return nil, releaseErrUnavailable("query charge failed")
		}
		results = append(results, QuotaReleaseResult{
			ChargeID:      charge.ChargeID,
			AppliedDelta:  0,
			ReleasedTotal: charge.ReleasedUnits,
		})
	}
	return results, nil
}

func chargeIDsOf(items []QuotaReleaseItemInput) []string {
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ChargeID)
	}
	return ids
}

// ── 未发送本地撤销（§8.3） ───────────────────────────────────

// CancelUnsent 撤销从未尝试发送的操作：operation 必须 QUEUED 且 attempt_count=0。
// 同一事务内：锁 tenant → 锁 operation 标记 CANCELED_UNSENT（封闭 worker 领取）→
// 全额退还 charge 并写本地原因记录。
func (r *QuotaLedgerRepo) CancelUnsent(ctx context.Context, operationID string) error {
	sysCtx := appViewer.NewSystemViewerContext(ctx)

	// 先用不可变归属定位 tenant。
	op0, err := r.entClient.Client().QuotaOperation.Query().
		Where(quotaoperation.OperationIDEQ(operationID)).
		Only(sysCtx)
	if ent.IsNotFound(err) {
		return QuotaErrNotFound("operation not found")
	} else if err != nil {
		r.log.Errorf(ctx, "cancel: locate operation failed: %s", err.Error())
		return QuotaErrStorageUnavailable("locate operation failed")
	}
	tenantId := derefUint32(op0.TenantID)

	tx, err := r.entClient.Client().Tx(sysCtx)
	if err != nil {
		return QuotaErrStorageUnavailable("start transaction failed")
	}
	rollback := func() { _ = tx.Rollback() }

	cql := supportsRowLock(r.entClient)
	caq := tx.Tenant.Query().Where(tenant.IDEQ(tenantId))
	if cql {
		caq = caq.ForUpdate()
	}
	if _, err = caq.Only(sysCtx); err != nil {
		rollback()
		return QuotaErrStorageUnavailable("lock tenant failed")
	}
	oq := tx.QuotaOperation.Query().
		Where(quotaoperation.OperationIDEQ(operationID), quotaoperation.TenantIDEQ(tenantId))
	if cql {
		oq = oq.ForUpdate()
	}
	op, err := oq.Only(sysCtx)
	if ent.IsNotFound(err) {
		rollback()
		return QuotaErrNotFound("operation not found")
	} else if err != nil {
		rollback()
		return QuotaErrStorageUnavailable("lock operation failed")
	}
	// 只允许 QUEUED 且 attempt_count=0；任何发送尝试发生后必须走 owner 协议。
	if op.DispatchState != quotaoperation.DispatchStateQueued || op.AttemptCount != 0 {
		rollback()
		return QuotaErrInvalid("operation is no longer cancelable without owner protocol")
	}
	if _, err = tx.QuotaOperation.UpdateOneID(op.ID).
		SetDispatchState(quotaoperation.DispatchStateCanceledUnsent).
		SetNillableLastErrorCode(trans.Ptr("LOCAL_CANCEL")).
		Save(sysCtx); err != nil {
		rollback()
		return QuotaErrStorageUnavailable("mark canceled failed")
	}

	// 全额退还对应 charge。
	chq := tx.QuotaCharge.Query().
		Where(quotacharge.TenantIDEQ(tenantId), quotacharge.OperationIDEQ(operationID))
	if cql {
		chq = chq.ForUpdate()
	}
	charges, err := chq.All(sysCtx)
	if err != nil {
		rollback()
		return QuotaErrStorageUnavailable("lock charges failed")
	}
	occupiedDeltaByAccount := make(map[uint32]int64, len(charges))
	for _, charge := range charges {
		delta := charge.OriginalUnits - charge.ReleasedUnits
		if delta <= 0 {
			continue
		}
		if _, uerr := tx.QuotaCharge.UpdateOneID(charge.ID).
			SetReleasedUnits(charge.OriginalUnits).
			Save(sysCtx); uerr != nil {
			rollback()
			return QuotaErrStorageUnavailable("refund charge failed")
		}
		acq := tx.QuotaAccount.Query().
			Where(quotaaccount.TenantIDEQ(tenantId), quotaaccount.QuotaCodeEQ(charge.QuotaCode))
		if cql {
			acq = acq.ForUpdate()
		}
		acc, aerr := acq.Only(sysCtx)
		if aerr != nil {
			rollback()
			return QuotaErrStorageUnavailable("lock account failed")
		}
		occupiedDeltaByAccount[acc.ID] += delta
	}
	for accID, delta := range occupiedDeltaByAccount {
		if delta > 0 {
			if _, uerr := tx.QuotaAccount.UpdateOneID(accID).
				AddOccupiedUnits(-delta).
				AddVersion(1).
				Save(sysCtx); uerr != nil {
				rollback()
				return QuotaErrStorageUnavailable("update account failed")
			}
		}
	}

	if err = tx.Commit(); err != nil {
		r.log.Errorf(ctx, "cancel: commit failed: %s", err.Error())
		return QuotaErrStorageUnavailable("commit failed")
	}
	return nil
}

// ── 不变量重算（INV-01） ─────────────────────────────────────

// InvariantRow 单租户单编码的恒等式重算结果。
type InvariantRow struct {
	TenantID        uint32
	QuotaCode       string
	OccupiedUnits   int64
	ChargeRemainder int64
	Balanced        bool
}

// RecomputeInvariants 用 SQL 重算 account.occupied_units 与
// SUM(charge.original-released) 的恒等式（tenantId=0 表示全部租户）。
func (r *QuotaLedgerRepo) RecomputeInvariants(ctx context.Context, tenantId uint32) ([]InvariantRow, error) {
	rows, err := r.entClient.DB().QueryContext(ctx, `
SELECT a.tenant_id, a.quota_code, a.occupied_units,
       COALESCE(SUM(c.original_units - c.released_units), 0) AS charge_remainder
FROM sys_quota_accounts a
LEFT JOIN sys_quota_charges c
       ON c.tenant_id = a.tenant_id AND c.quota_code = a.quota_code
WHERE ($1 = 0 OR a.tenant_id = $1)
GROUP BY a.tenant_id, a.quota_code, a.occupied_units
ORDER BY a.tenant_id, a.quota_code`, tenantId)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]InvariantRow, 0, 8)
	for rows.Next() {
		var row InvariantRow
		if err = rows.Scan(&row.TenantID, &row.QuotaCode, &row.OccupiedUnits, &row.ChargeRemainder); err != nil {
			return nil, err
		}
		row.Balanced = row.OccupiedUnits == row.ChargeRemainder
		out = append(out, row)
	}
	return out, rows.Err()
}

var _ = sql.ErrNoRows
var _ = fmt.Stringer(nil)

// ── 投递 worker 支持方法（§8.2） ─────────────────────────────

// ClaimedOperation 一次成功领取的操作。
type ClaimedOperation struct {
	ID                uint32
	TenantID          uint32
	OperationID       string
	ResourceTenantID  string
	ResourceID        string
	CreateOperationID string
	ActorType         string
	ActorID           string
	OwnerService      string
	Action            string
	RequestHash       string
	CanonicalRequest  string
	LeaseGeneration   int64
	AttemptCount      int
	Charges           []QuotaChargeRef
}

// claimCandidate 候选条件：QUEUED/UNKNOWN/DISPATCHING(租约过期)，
// 未被 retry_blocked，next_attempt_at 到期或为空。
const claimCandidateSQL = `
SELECT id FROM sys_quota_operations
WHERE retry_blocked = false
  AND (next_attempt_at IS NULL OR next_attempt_at <= now())
  AND dispatch_state IN ('QUEUED', 'UNKNOWN')
   OR (dispatch_state = 'DISPATCHING' AND (lease_until IS NULL OR lease_until <= now()))
ORDER BY created_at
LIMIT $1`

// ClaimDispatchable 领取待投递操作：每条在独立事务内锁定并提交 DISPATCHING；
// 领取事务先提交，网络调用由调用方随后进行（§8.2）。
func (r *QuotaLedgerRepo) ClaimDispatchable(ctx context.Context, workerOwner string, lease time.Duration, limit int) ([]ClaimedOperation, error) {
	sysCtx := appViewer.NewSystemViewerContext(ctx)
	rowLock := supportsRowLock(r.entClient)

	rows, err := r.entClient.DB().QueryContext(sysCtx, claimCandidateSQL, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]uint32, 0, limit)
	for rows.Next() {
		var id uint32
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()

	claimed := make([]ClaimedOperation, 0, len(ids))
	for _, id := range ids {
		tx, terr := r.entClient.Client().Tx(sysCtx)
		if terr != nil {
			return claimed, terr
		}
		oq := tx.QuotaOperation.Query().Where(quotaoperation.IDEQ(id))
		if rowLock {
			oq = oq.ForUpdate()
		}
		op, oerr := oq.Only(sysCtx)
		if oerr != nil {
			_ = tx.Rollback()
			continue // 已被其他 worker 领取/删除
		}
		claimable := op.DispatchState == quotaoperation.DispatchStateQueued ||
			op.DispatchState == quotaoperation.DispatchStateUnknown ||
			(op.DispatchState == quotaoperation.DispatchStateDispatching &&
				op.LeaseUntil != nil && !op.LeaseUntil.After(time.Now()))
		if !claimable || op.RetryBlocked {
			_ = tx.Rollback()
			continue
		}
		gen := op.LeaseGeneration + 1
		charges, cerr := tx.QuotaCharge.Query().
			Where(quotacharge.TenantIDEQ(derefUint32(op.TenantID)), quotacharge.OperationIDEQ(op.OperationID)).
			All(sysCtx)
		if cerr != nil {
			_ = tx.Rollback()
			continue
		}
		refs := make([]QuotaChargeRef, 0, len(charges))
		for _, c := range charges {
			refs = append(refs, QuotaChargeRef{ChargeID: c.ChargeID, QuotaCode: c.QuotaCode, ChargedUnits: c.OriginalUnits})
		}
		now := time.Now()
		upq := tx.QuotaOperation.UpdateOneID(op.ID).
			SetDispatchState(quotaoperation.DispatchStateDispatching).
			AddAttemptCount(1).
			SetLeaseGeneration(gen).
			SetLeaseOwner(workerOwner).
			SetLeaseUntil(now.Add(lease))
		if _, uerr := upq.Save(sysCtx); uerr != nil {
			_ = tx.Rollback()
			continue
		}
		if cerr = tx.Commit(); cerr != nil {
			continue
		}
		createOpID := ""
		if op.CreateOperationID != nil {
			createOpID = *op.CreateOperationID
		}
		claimed = append(claimed, ClaimedOperation{
			ID:                op.ID,
			TenantID:          derefUint32(op.TenantID),
			OperationID:       op.OperationID,
			ResourceTenantID:  op.ResourceTenantID,
			ResourceID:        op.ResourceID,
			CreateOperationID: createOpID,
			ActorType:         op.ActorType,
			ActorID:           op.ActorID,
			OwnerService:      op.OwnerService,
			Action:            op.Action,
			RequestHash:       op.RequestHash,
			CanonicalRequest:  op.CanonicalRequest,
			LeaseGeneration:   gen,
			AttemptCount:      op.AttemptCount + 1,
			Charges:           refs,
		})
	}
	return claimed, nil
}

// AckDispatched 成功回写 ACKED：必须匹配领取时的 lease_generation 和
// DISPATCHING 前态（旧 worker 迟到回写被拒绝，FAIL-18）。
func (r *QuotaLedgerRepo) AckDispatched(ctx context.Context, operationID string, generation int64, ackJSON string) (bool, error) {
	sysCtx := appViewer.NewSystemViewerContext(ctx)
	cnt, err := r.entClient.Client().QuotaOperation.Update().
		Where(
			quotaoperation.OperationIDEQ(operationID),
			quotaoperation.LeaseGenerationEQ(generation),
			quotaoperation.DispatchStateEQ(quotaoperation.DispatchStateDispatching),
		).
		SetDispatchState(quotaoperation.DispatchStateAcked).
		SetNillableAckJSON(trans.Ptr(ackJSON)).
		Save(sysCtx)
	return cnt == 1, err
}

// MarkUnknown 发送结果不确定/永久合同错误回写：保持占额；
// retryBlocked=true 时暂停自动重试（FAIL-17）。
func (r *QuotaLedgerRepo) MarkUnknown(ctx context.Context, operationID string, generation int64, nextAttempt time.Time, lastErrCode string, retryBlocked bool) (bool, error) {
	sysCtx := appViewer.NewSystemViewerContext(ctx)
	upd := r.entClient.Client().QuotaOperation.Update().
		Where(
			quotaoperation.OperationIDEQ(operationID),
			quotaoperation.LeaseGenerationEQ(generation),
			quotaoperation.DispatchStateEQ(quotaoperation.DispatchStateDispatching),
		).
		SetDispatchState(quotaoperation.DispatchStateUnknown).
		SetNextAttemptAt(nextAttempt).
		SetRetryBlocked(retryBlocked).
		SetNillableLastErrorCode(trans.Ptr(lastErrCode))
	cnt, err := upd.Save(sysCtx)
	return cnt == 1, err
}

// ResumeDispatch 解除 retry_blocked 暂停：保留原 operation/charge/request_hash，
// 恢复后由正常 worker 重试原命令（FAIL-17）。仅允许 lab 控制钩子调用并记录审计。
func (r *QuotaLedgerRepo) ResumeDispatch(ctx context.Context, operationID string) error {
	sysCtx := appViewer.NewSystemViewerContext(ctx)
	cnt, err := r.entClient.Client().QuotaOperation.Update().
		Where(quotaoperation.OperationIDEQ(operationID), quotaoperation.RetryBlockedEQ(true)).
		SetRetryBlocked(false).
		SetNextAttemptAt(time.Now()).
		Save(sysCtx)
	if err != nil {
		return err
	}
	if cnt == 0 {
		return QuotaErrNotFound("no blocked operation with this id")
	}
	return nil
}

// GetOperationForUser 按 operation_id + tenant 读取投递状态（QUOTA-LAB-04）；
// 禁止无租户 GetByID 后原样返回（§6.8）。
func (r *QuotaLedgerRepo) GetOperationForUser(ctx context.Context, tenantId uint32, operationID string) (*ent.QuotaOperation, error) {
	sysCtx := appViewer.NewSystemViewerContext(ctx)
	op, err := r.entClient.Client().QuotaOperation.Query().
		Where(
			quotaoperation.OperationIDEQ(operationID),
			quotaoperation.TenantIDEQ(tenantId),
		).
		Only(sysCtx)
	return op, err
}

// QuotaChargeRef 命令携带的占额明细（service 层投递命令复用）。
type QuotaChargeRef struct {
	ChargeID     string
	QuotaCode    string
	ChargedUnits int64
}

// ── 删除操作登记（§6.2：DELETE 不新建 charge、不新增占额） ────

// QuotaDeleteInput 删除操作输入。
type QuotaDeleteInput struct {
	TenantID        uint32
	ActorType       string
	ActorID         string
	OwnerService    string
	Action          string
	IdempotencyKey  string
	RequestHash     string
	CanonicalRequest string
	ResourceID      string
}

// QuotaDeleteResult 删除操作登记结果。
type QuotaDeleteResult struct {
	OperationID       string
	CreateOperationID string
	ChargeID          string // 原创建 charge（供转发命令与归属校验）
	ResourceID        string
	Replayed          bool
}

// CreateDeleteOperation 登记删除操作：按 (tenant,owner,resource_id,create IS NULL)
// 定位原创建操作并持久保存 create_operation_id（重启后不依赖内存关联）。
func (r *QuotaLedgerRepo) CreateDeleteOperation(ctx context.Context, in *QuotaDeleteInput) (*QuotaDeleteResult, error) {
	sysCtx := appViewer.NewSystemViewerContext(ctx)
	if in == nil || in.IdempotencyKey == "" || in.RequestHash == "" || in.CanonicalRequest == "" || in.ResourceID == "" {
		return nil, QuotaErrInvalid("delete operation input is incomplete")
	}

	tx, err := r.entClient.Client().Tx(sysCtx)
	if err != nil {
		return nil, QuotaErrStorageUnavailable("start transaction failed")
	}
	rollback := func() { _ = tx.Rollback() }

	// 幂等：同删除 key 同内容返回既有操作；不同内容冲突。
	existing, err := tx.QuotaOperation.Query().
		Where(
			quotaoperation.TenantIDEQ(in.TenantID),
			quotaoperation.ActorTypeEQ(in.ActorType),
			quotaoperation.ActorIDEQ(in.ActorID),
			quotaoperation.ActionEQ(in.Action),
			quotaoperation.IdempotencyKeyEQ(in.IdempotencyKey),
		).
		Only(sysCtx)
	if err == nil {
		if existing.RequestHash != in.RequestHash {
			rollback()
			return nil, QuotaErrIdempotencyConflict("idempotency key reused with different payload")
		}
		origOpID := ""
		if existing.CreateOperationID != nil {
			origOpID = *existing.CreateOperationID
		}
		chargeID := ""
		if origOpID != "" {
			if ch, cerr := tx.QuotaCharge.Query().
				Where(quotacharge.TenantIDEQ(in.TenantID), quotacharge.OperationIDEQ(origOpID)).
				First(sysCtx); cerr == nil {
				chargeID = ch.ChargeID
			}
		}
		rollback()
		return &QuotaDeleteResult{
			OperationID:       existing.OperationID,
			CreateOperationID: origOpID,
			ChargeID:          chargeID,
			ResourceID:        existing.ResourceID,
			Replayed:          true,
		}, nil
	} else if !ent.IsNotFound(err) {
		rollback()
		r.log.Errorf(ctx, "delete op: idempotency lookup failed: %s", err.Error())
		return nil, QuotaErrStorageUnavailable("idempotency lookup failed")
	}

	// 原创建操作必须存在且属于本租户（跨租户统一 404，不泄露存在性）。
	createOp, err := tx.QuotaOperation.Query().
		Where(
			quotaoperation.TenantIDEQ(in.TenantID),
			quotaoperation.OwnerServiceEQ(in.OwnerService),
			quotaoperation.ResourceIDEQ(in.ResourceID),
			quotaoperation.CreateOperationIDIsNil(),
		).
		Only(sysCtx)
	if ent.IsNotFound(err) {
		rollback()
		return nil, QuotaErrNotFound("resource not found")
	} else if err != nil {
		rollback()
		r.log.Errorf(ctx, "delete op: locate create operation failed: %s", err.Error())
		return nil, QuotaErrStorageUnavailable("locate create operation failed")
	}

	origCharge, err := tx.QuotaCharge.Query().
		Where(quotacharge.TenantIDEQ(in.TenantID), quotacharge.OperationIDEQ(createOp.OperationID)).
		First(sysCtx)
	if ent.IsNotFound(err) {
		rollback()
		return nil, QuotaErrNotFound("original charge not found")
	} else if err != nil {
		rollback()
		return nil, QuotaErrStorageUnavailable("query original charge failed")
	}

	op, err := tx.QuotaOperation.Create().
		SetOperationID(generateUUID()).
		SetTenantID(in.TenantID).
		SetResourceTenantID(createOp.ResourceTenantID).
		SetResourceID(createOp.ResourceID).
		SetNillableCreateOperationID(trans.Ptr(createOp.OperationID)).
		SetActorType(in.ActorType).
		SetActorID(in.ActorID).
		SetOwnerService(in.OwnerService).
		SetAction(in.Action).
		SetIdempotencyKey(in.IdempotencyKey).
		SetRequestHash(in.RequestHash).
		SetCanonicalRequest(in.CanonicalRequest).
		SetDispatchState(quotaoperation.DispatchStateQueued).
		Save(sysCtx)
	if err != nil {
		rollback()
		if ent.IsConstraintError(err) {
			return nil, QuotaErrIdempotencyConflict("concurrent idempotency conflict")
		}
		r.log.Errorf(ctx, "delete op: save failed: %s", err.Error())
		return nil, QuotaErrStorageUnavailable("save delete operation failed")
	}

	if err = tx.Commit(); err != nil {
		r.log.Errorf(ctx, "delete op: commit failed: %s", err.Error())
		return nil, QuotaErrStorageUnavailable("commit failed")
	}
	return &QuotaDeleteResult{
		OperationID:       op.OperationID,
		CreateOperationID: createOp.OperationID,
		ChargeID:          origCharge.ChargeID,
		ResourceID:        createOp.ResourceID,
	}, nil
}

// FindCreateOperationByResource 按 (tenant,owner,resource_id) 定位创建操作
// 及其 charge（GET 转发与归属校验共用）。
func (r *QuotaLedgerRepo) FindCreateOperationByResource(ctx context.Context, tenantId uint32, owner, resourceID string) (*ent.QuotaOperation, *ent.QuotaCharge, error) {
	sysCtx := appViewer.NewSystemViewerContext(ctx)
	op, err := r.entClient.Client().QuotaOperation.Query().
		Where(
			quotaoperation.TenantIDEQ(tenantId),
			quotaoperation.OwnerServiceEQ(owner),
			quotaoperation.ResourceIDEQ(resourceID),
			quotaoperation.CreateOperationIDIsNil(),
		).
		Only(sysCtx)
	if err != nil {
		return nil, nil, err
	}
	charge, err := r.entClient.Client().QuotaCharge.Query().
		Where(quotacharge.TenantIDEQ(tenantId), quotacharge.OperationIDEQ(op.OperationID)).
		First(sysCtx)
	if err != nil {
		return nil, nil, err
	}
	return op, charge, nil
}

// GetChargeForOperation 返回操作的第一笔 charge（删除命令与查询响应使用）。
func (r *QuotaLedgerRepo) GetChargeForOperation(ctx context.Context, tenantId uint32, operationID string) (*ent.QuotaCharge, error) {
	sysCtx := appViewer.NewSystemViewerContext(ctx)
	return r.entClient.Client().QuotaCharge.Query().
		Where(quotacharge.TenantIDEQ(tenantId), quotacharge.OperationIDEQ(operationID)).
		First(sysCtx)
}

// NewQuotaLedgerRepoForTest 供集成测试白盒构造（不连库、不播种）。
func NewQuotaLedgerRepoForTest(c *entCrud.EntClient[*ent.Client], log *bLogger.Helper) *QuotaLedgerRepo {
	return &QuotaLedgerRepo{entClient: c, log: log}
}

// DB 暴露底层 sql.DB（集成测试与运维只读检查使用）。
func (r *QuotaLedgerRepo) DB() *sql.DB { return r.entClient.DB() }

// EntClientForTest 暴露 ent client（测试 fixture 构造使用）。
func (r *QuotaLedgerRepo) EntClientForTest() *ent.Client { return r.entClient.Client() }

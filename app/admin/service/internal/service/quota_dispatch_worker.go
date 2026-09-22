package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"github.com/google/uuid"

	"go-wind-admin/app/admin/service/internal/data"
)

// QuotaDispatchWorker 实现持久化转发（计划 §8）。
// 状态机固定为 QUEUED → DISPATCHING → {ACKED | UNKNOWN}；CANCELED_UNSENT 仅由
// §8.3 本地撤销写入。worker 使用 PostgreSQL 领取；同进程和多进程通过行锁/租约协调。
// 构造函数不查询数据库、不启动 goroutine；Start 后运行，Stop 停止领取并等待退出。
type QuotaDispatchWorker struct {
	log      *bLogger.Helper
	ledger   *data.QuotaLedgerRepo
	registry *QuotaAdapterRegistry

	workerID     string
	lease        time.Duration // 固定 15 秒
	pollInterval time.Duration
	batchLimit   int

	notify chan struct{}
	stopCh chan struct{}
	done   chan struct{}
	stopMu sync.Mutex

	// paused 仅 lab 控制钩子使用：占额提交后暂停投递（FAIL-02/15 屏障）。
	paused atomic.Bool
}

// SetPaused 设置投递暂停屏障；仅 quota_lab 控制监听调用，正式构建不暴露。
func (w *QuotaDispatchWorker) SetPaused(p bool) { w.paused.Store(p) }

// Paused 返回当前暂停状态。
func (w *QuotaDispatchWorker) Paused() bool { return w.paused.Load() }

// 固定退避 1/2/4/8/16/30 秒（§8.2）。
func backoffForAttempt(attempt int) time.Duration {
	shift := attempt
	if shift > 5 {
		shift = 5
	}
	if shift < 1 {
		shift = 1
	}
	d := time.Duration(1<<uint(shift-1)) * time.Second
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
}

func NewQuotaDispatchWorker(
	ctx *bootstrap.Context,
	ledger *data.QuotaLedgerRepo,
	registry *QuotaAdapterRegistry,
) *QuotaDispatchWorker {
	return &QuotaDispatchWorker{
		log:          ctx.NewLoggerHelper("quota-dispatch/worker/admin-service"),
		ledger:       ledger,
		registry:     registry,
		workerID:     "worker-" + uuid.NewString(),
		lease:        15 * time.Second,
		pollInterval: 300 * time.Millisecond,
		batchLimit:   8,
		notify:       make(chan struct{}, 1),
		stopCh:       make(chan struct{}),
		done:         make(chan struct{}),
	}
}

// Notify 在占额事务提交成功后唤醒 worker（提交前不得调用）。
func (w *QuotaDispatchWorker) Notify() {
	select {
	case w.notify <- struct{}{}:
	default:
	}
}

// Start 实现 transport.Server 生命周期；不阻塞调用方。
func (w *QuotaDispatchWorker) Start(_ context.Context) error {
	go w.loop()
	return nil
}

// Stop 停止领取并等待有限时间退出。
func (w *QuotaDispatchWorker) Stop(_ context.Context) error {
	w.stopMu.Lock()
	select {
	case <-w.stopCh:
	default:
		close(w.stopCh)
	}
	w.stopMu.Unlock()
	select {
	case <-w.done:
	case <-time.After(5 * time.Second):
		w.log.Warn(context.Background(), "quota dispatch worker stop timeout")
	}
	return nil
}

func (w *QuotaDispatchWorker) loop() {
	defer close(w.done)
	for {
		select {
		case <-w.stopCh:
			return
		case <-w.notify:
		case <-time.After(w.pollInterval):
		}
		w.drain()
	}
}

// drain 扫描并投递一批到期操作；每次网络调用不在任何数据库事务内（§7.1）。
// 暂停屏障生效时立即返回（已提交操作保持 QUEUED 等待恢复）。
func (w *QuotaDispatchWorker) drain() {
	if w.paused.Load() {
		return
	}
	for {
		select {
		case <-w.stopCh:
			return
		default:
		}
		ops, err := w.ledger.ClaimDispatchable(context.Background(), w.workerID, w.lease, w.batchLimit)
		if err != nil {
			w.log.Errorf(context.Background(), "claim dispatchable failed: %s", err.Error())
			return
		}
		if len(ops) == 0 {
			return
		}
		for i := range ops {
			select {
			case <-w.stopCh:
				return
			default:
			}
			w.dispatchOne(&ops[i])
		}
	}
}

// dispatchOne 投递单个已领取操作：先提交领取（ClaimDispatchable 已提交），再发 RPC。
func (w *QuotaDispatchWorker) dispatchOne(op *data.ClaimedOperation) {
	adapter := w.registry.Lookup(op.OwnerService, op.Action)
	if adapter == nil {
		// 恢复遇到缺失 adapter：保留账本并报错，不退额、不改余额（§8.2）。
		w.log.Errorf(context.Background(), "missing quota adapter for owner=%s action=%s operation=%s: ledger kept, dispatch deferred",
			op.OwnerService, op.Action, op.OperationID)
		next := time.Now().Add(60 * time.Second)
		if _, err := w.ledger.MarkUnknown(context.Background(), op.OperationID, op.LeaseGeneration, next, "ADAPTER_MISSING", false); err != nil {
			w.log.Errorf(context.Background(), "mark unknown failed: %s", err.Error())
		}
		return
	}

	// 删除操作不新建 charge（§6.2）：命令携带原创建操作的 charge。
	charges := op.Charges
	if len(charges) == 0 && op.CreateOperationID != "" {
		if orig, err := w.ledger.GetChargeForOperation(context.Background(), op.TenantID, op.CreateOperationID); err == nil {
			charges = []data.QuotaChargeRef{{ChargeID: orig.ChargeID, QuotaCode: orig.QuotaCode, ChargedUnits: orig.OriginalUnits}}
		} else {
			w.log.Errorf(context.Background(), "resolve original charge for delete op=%s failed: %s", op.OperationID, err.Error())
		}
	}

	cmd := &QuotaDispatchCommand{
		OperationID:       op.OperationID,
		ResourceID:        op.ResourceID,
		ResourceTenantID:  op.ResourceTenantID,
		Actor:             op.ActorType + ":" + op.ActorID,
		Action:            op.Action,
		RequestHash:       op.RequestHash,
		CanonicalRequest:  []byte(op.CanonicalRequest),
		CreateOperationID: op.CreateOperationID,
		Charges:           charges,
	}

	dispatchCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	ack, derr := adapter.Dispatch(dispatchCtx, cmd)
	cancel()

	if derr == nil {
		ok, aerr := w.ledger.AckDispatched(context.Background(), op.OperationID, op.LeaseGeneration, string(ack))
		if aerr != nil {
			w.log.Errorf(context.Background(), "ack writeback failed: %s", aerr.Error())
			return
		}
		if !ok {
			// 迟到回写：租约已被接管，放弃本次结果（FAIL-18）。
			w.log.Warnf(context.Background(), "ack writeback skipped (stale generation) operation=%s generation=%d", op.OperationID, op.LeaseGeneration)
		}
		return
	}

	// 分类：永久合同错误暂停重试；传输不确定保持占额并退避重试（同 ID 同报文）。
	perm := isPermanentContractError(derr)
	next := time.Now().Add(backoffForAttempt(op.AttemptCount))
	ok, aerr := w.ledger.MarkUnknown(context.Background(), op.OperationID, op.LeaseGeneration, next, errorReason(derr), perm)
	if aerr != nil {
		w.log.Errorf(context.Background(), "unknown writeback failed: %s", aerr.Error())
		return
	}
	if !ok {
		w.log.Warnf(context.Background(), "unknown writeback skipped (stale generation) operation=%s generation=%d", op.OperationID, op.LeaseGeneration)
		return
	}
	if perm {
		w.log.Errorf(context.Background(), "permanent contract error for operation=%s: %s; retry paused (retry_blocked), ledger kept", op.OperationID, derr.Error())
	} else {
		w.log.Warnf(context.Background(), "dispatch unknown for operation=%s: %s; will retry with same id/payload", op.OperationID, derr.Error())
	}
}

// isPermanentContractError 判定下游明确的永久合同错误（gRPC InvalidArgument/
// FailedPrecondition/NotFound/PermissionDenied/Unauthenticated）。
// 下游明确拒绝业务执行也不能由本侧凭错误字符串退额；仅暂停重试。
func isPermanentContractError(err error) bool {
	return grpcCodeIn(err,
		codesInvalidArgument,
		codesFailedPrecondition,
		codesNotFound,
		codesPermissionDenied,
		codesUnauthenticated,
	)
}

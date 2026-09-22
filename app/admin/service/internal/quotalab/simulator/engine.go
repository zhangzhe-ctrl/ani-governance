//go:build quota_lab

package simulator

// 模拟 owner 的核心业务逻辑（§11.2/§11.4/§11.5）。
// 关键性质：
//   - 创建先持久化 owner 命令，再推进 provider 分配，最后记录 owner 完成；
//     两者之间允许故障，重启后按 operation/resource ID 对账恢复。
//   - provider Allocate 与 Fence/Close 在同一 operation 的 provider 行锁上串行化，
//     防止迟到创建（旧代次在 provider 提交点被拒绝）。
//   - 释放事实以 (charge_id, unit_ordinal) 唯一；累计值从唯一事实求和。

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

// ErrPermanentConflict 同 operation_id 同规范请求冲突：不能覆盖原操作。
var ErrPermanentConflict = errors.New("SIMULATOR_CONFLICT: operation id reused with different payload")

// ErrClosed 操作已被封闭：拒绝旧代次继续分配。
var ErrClosed = errors.New("SIMULATOR_CLOSED: operation is fenced")

// ErrCapacity 模拟 provider 容量不足。
var ErrCapacity = errors.New("SIMULATOR_CAPACITY_EXCEEDED")

// CommandStatus owner 命令状态。
const (
	StatusAccepted  = "accepted"  // 已持久接受（可能尚未完成）
	StatusCompleted = "completed" // 创建完成
	StatusAborted   = "aborted"   // 已封闭并进入清理流程
)

// FailInjector 由控制面注入的确定性故障点。
type FailInjector struct {
	// FailCreateOrdinal 令创建第 N 个单元失败（1-based）。
	FailCreateOrdinal int
	// FailReleaseOrdinal 令释放第 N 个单元失败。
	FailReleaseOrdinal int
	// BlockReleaseNotify 阻断退额通知发送。
	BlockReleaseNotify bool
	// PauseAfterOwnerAccept owner 接受后挂起（丢 ACK 场景由调用方 kill 实现）。
	PauseAfterOwnerAccept bool
}

// Simulator 聚合 owner/provider 两库与故障注入。
type Simulator struct {
	owner    *OwnerStore
	provider *ProviderStore
	Fails    *FailInjector
}

func NewSimulator(owner *OwnerStore, provider *ProviderStore) *Simulator {
	return &Simulator{owner: owner, provider: provider, Fails: &FailInjector{}}
}

// ── AcceptCreate（§11.3） ────────────────────────────────────

// AcceptCreateInput 创建命令输入（字段已在 server 层校验 quota_code/charged_units）。
type AcceptCreateInput struct {
	OperationID string
	ResourceID  string
	TenantID    string
	Actor       string
	RequestHash string
	Name        string
	GpuCount    int32
	ChargeID    string
}

// AcceptCreate 持久化接受后返回 operation_id/resource_id/accepted=true。
// 同 operation_id 同规范请求只返回原接受结果；合法接受后的容量/创建失败
// 必须持久化处理并发可退额通知。
func (s *Simulator) AcceptCreate(ctx context.Context, in *AcceptCreateInput) (bool, error) {
	// 1. 幂等检查：同 ID 同 hash 幂等；同 ID 异 hash 永久合同冲突。
	//    已 completed 的命令直接返回原结果；accepted/aborted 的命令续跑
	//    （恢复不重新分配已存在单元，§11.2.6）。
	var storedHash, storedStatus string
	err := s.owner.db.QueryRowContext(ctx,
		`SELECT request_hash, status FROM sim_commands WHERE operation_id = $1`, in.OperationID).Scan(&storedHash, &storedStatus)
	switch {
	case err == nil:
		if storedHash != in.RequestHash {
			return false, ErrPermanentConflict
		}
		if storedStatus == StatusCompleted {
			return true, nil // 幂等重放：返回原接受结果
		}
		return s.resumeCreate(ctx, in)
	case errors.Is(err, sql.ErrNoRows):
	default:
		return false, err
	}

	// 2. 先持久化 owner 命令（接受）。
	if err = s.owner.withOwnerTx(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx,
			`INSERT INTO sim_commands
			   (operation_id, tenant_id, resource_id, kind, status, actor, request_hash, name, gpu_count, charge_id)
			 VALUES ($1,$2,$3,'create',$4,$5,$6,$7,$8,$9)
			 ON CONFLICT (operation_id) DO NOTHING`,
			in.OperationID, in.TenantID, in.ResourceID, StatusAccepted, in.Actor, in.RequestHash, in.Name, in.GpuCount, in.ChargeID)
		return e
	}); err != nil {
		return false, err
	}

	// 3. 推进 provider 分配（owner 接受与 provider 提交之间允许故障）。
	if err = s.provisionUnits(ctx, in); err != nil {
		// 合法接受后的失败：封闭创建、清理已分配单元、登记可退额事实。
		if cerr := s.abortCreate(ctx, in, err); cerr != nil {
			return false, fmt.Errorf("provision failed (%v) and cleanup failed: %w", err, cerr)
		}
		// 命令转为 aborted，本次向 Governance 返回永久合同错误：
		// Governance 保持占额并暂停重试，等待 owner 的退额回执。
		return false, fmt.Errorf("%w: provision failed", ErrPermanentConflict)
	}

	// 4. 记录 owner 完成。
	if err = s.owner.withOwnerTx(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx,
			`UPDATE sim_commands SET status=$1, updated_at=now() WHERE operation_id=$2`,
			StatusCompleted, in.OperationID)
		return e
	}); err != nil {
		return false, err
	}
	return true, nil
}

// resumeCreate 续跑未完成的创建命令（重启恢复/重放）：provider 已封闭走
// 清理收敛；未封闭则继续补齐缺失单元（ON CONFLICT DO NOTHING 保证不重复分配）。
func (s *Simulator) resumeCreate(ctx context.Context, in *AcceptCreateInput) (bool, error) {
	var closed bool
	err := s.provider.db.QueryRowContext(ctx,
		`SELECT closed FROM sim_provider_ops WHERE operation_id=$1`, in.OperationID).Scan(&closed)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if closed {
		if cerr := s.abortCreate(ctx, in, ErrClosed); cerr != nil {
			return false, cerr
		}
		// 收敛完成（可能仍有未证实释放的单元保留占额，见 abortCreate）。
		return true, nil
	}
	if err = s.provisionUnits(ctx, in); err != nil {
		if cerr := s.abortCreate(ctx, in, err); cerr != nil {
			return false, fmt.Errorf("provision failed (%v) and cleanup failed: %w", err, cerr)
		}
		return false, fmt.Errorf("%w: provision failed", ErrPermanentConflict)
	}
	if err = s.owner.withOwnerTx(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx,
			`UPDATE sim_commands SET status=$1, updated_at=now() WHERE operation_id=$2`,
			StatusCompleted, in.OperationID)
		return e
	}); err != nil {
		return false, err
	}
	return true, nil
}

// provisionUnits 在 provider 侧分配 gpu_count 个单元：逐单元独立提交，
// 部分故障时已提交单元保留（§11.5：先封闭创建，再清理已分配单元）。
// Allocate 提交前检查未 closed（§11.4，provider 行锁与 Fence 串行化）。
func (s *Simulator) provisionUnits(ctx context.Context, in *AcceptCreateInput) error {
	for i := int32(1); i <= in.GpuCount; i++ {
		if err := s.allocateOneUnit(ctx, in, i); err != nil {
			return err
		}
	}
	return nil
}

func (s *Simulator) allocateOneUnit(ctx context.Context, in *AcceptCreateInput, ordinal int32) error {
	tx, err := s.provider.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// 行锁 provider op（Fence/Close 与 Allocate 在此串行化）。
	var closed bool
	err = tx.QueryRowContext(ctx,
		`SELECT closed FROM sim_provider_ops WHERE operation_id=$1 FOR UPDATE`,
		in.OperationID).Scan(&closed)
	if errors.Is(err, sql.ErrNoRows) {
		if _, e := tx.ExecContext(ctx,
			`INSERT INTO sim_provider_ops (operation_id, tenant_id, resource_id) VALUES ($1,$2,$3)`,
			in.OperationID, in.TenantID, in.ResourceID); e != nil {
			return e
		}
		closed = false
	} else if err != nil {
		return err
	}
	if closed {
		return ErrClosed
	}

	// 已存在（前次提交）即跳过：重启恢复不重复分配。
	var state string
	err = tx.QueryRowContext(ctx,
		`SELECT state FROM sim_allocations WHERE resource_id=$1 AND ordinal=$2`,
		in.ResourceID, ordinal).Scan(&state)
	if err == nil {
		return tx.Commit()
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// 容量检查：全局已分配名额 < 64。
	var used int
	if err = tx.QueryRowContext(ctx,
		`SELECT count(*) FROM sim_allocations WHERE state='allocated'`).Scan(&used); err != nil {
		return err
	}
	if used+1 > 64 {
		return ErrCapacity
	}

	if s.Fails.FailCreateOrdinal == int(ordinal) {
		return fmt.Errorf("SIMULATOR_UNIT_FAIL: create unit %d injected failure", ordinal)
	}
	if _, e := tx.ExecContext(ctx,
		`INSERT INTO sim_allocations (resource_id, ordinal, tenant_id, operation_id, state)
		 VALUES ($1,$2,$3,$4,'allocated')`,
		in.ResourceID, ordinal, in.TenantID, in.OperationID); e != nil {
		return e
	}
	return tx.Commit()
}

// abortCreate 封闭创建（阻止旧代次继续分配），枚举并清理已存在单元，
// 对确认清理完成的单元登记释放事实（ABORTED_CLEANED 退额依据）。
// 清理失败的单元保留占额、不登记事实，命令保持 accepted 交由
// Recover/重放续跑；全部清理完成后才置 aborted（§11.4/§11.5）。
func (s *Simulator) abortCreate(ctx context.Context, in *AcceptCreateInput, cause error) error {
	// 1. 终止操作先提交 closed。
	if _, err := s.provider.db.ExecContext(ctx,
		`UPDATE sim_provider_ops SET closed=true, execution_generation=execution_generation+1 WHERE operation_id=$1`,
		in.OperationID); err != nil {
		return err
	}

	// 2. 枚举已存在的 provider 单元。
	rows, err := s.provider.db.QueryContext(ctx,
		`SELECT ordinal, state FROM sim_allocations WHERE resource_id=$1 AND tenant_id=$2`,
		in.ResourceID, in.TenantID)
	if err != nil {
		return err
	}
	type unit struct {
		ordinal int
		state   string
	}
	var units []unit
	for rows.Next() {
		var u unit
		if err = rows.Scan(&u.ordinal, &u.state); err != nil {
			_ = rows.Close()
			return err
		}
		units = append(units, u)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	// 3. 清理已分配单元；注入/真实失败保留该单元，不登记释放事实。
	dirty := 0
	for _, u := range units {
		if u.state != "allocated" {
			continue
		}
		if s.Fails.FailReleaseOrdinal == u.ordinal {
			dirty++
			continue
		}
		if _, err = s.provider.db.ExecContext(ctx,
			`UPDATE sim_allocations SET state='freed', updated_at=now() WHERE resource_id=$1 AND ordinal=$2`,
			in.ResourceID, u.ordinal); err != nil {
			dirty++
			continue
		}
	}

	// 4. owner 同一事务：登记已清理单元的释放事实 + 待发通知；
	//    仅当无残留（dirty==0）才置 aborted，否则保持 accepted 供续跑。
	return s.owner.withOwnerTx(ctx, func(tx *sql.Tx) error {
		for _, u := range units {
			if u.state != "allocated" {
				continue
			}
			if s.Fails.FailReleaseOrdinal == u.ordinal {
				continue
			}
			if _, e := tx.ExecContext(ctx,
				`INSERT INTO sim_units (charge_id, unit_ordinal, tenant_id, resource_id, state)
				 VALUES ($1,$2,$3,$4,'aborted') ON CONFLICT (charge_id, unit_ordinal) DO NOTHING`,
				in.ChargeID, u.ordinal, in.TenantID, in.ResourceID); e != nil {
				return e
			}
			if _, e := tx.ExecContext(ctx,
				`INSERT INTO sim_release_facts (charge_id, unit_ordinal, tenant_id)
				 VALUES ($1,$2,$3) ON CONFLICT (charge_id, unit_ordinal) DO NOTHING`,
				in.ChargeID, u.ordinal, in.TenantID); e != nil {
				return e
			}
		}
		if dirty == 0 {
			if _, e := tx.ExecContext(ctx,
				`UPDATE sim_commands SET status=$1, updated_at=now() WHERE operation_id=$2`,
				StatusAborted, in.OperationID); e != nil {
				return e
			}
		}
		// 待发通知：累计释放事实求和；event_id 固定 = "abort:"+operation_id。
		return enqueueNotifyTx(ctx, tx, notifyInput{
			EventID:     "abort:" + in.OperationID,
			OperationID: in.OperationID,
			ChargeID:    in.ChargeID,
			TenantID:    in.TenantID,
			Reason:      "ABORTED_CLEANED",
		})
	})
}

// ── AcceptDelete（§11.3） ────────────────────────────────────

// AcceptDeleteInput 删除命令输入。
type AcceptDeleteInput struct {
	OperationID      string
	CreateOperationID string
	ResourceID       string
	TenantID         string
	Actor            string
	RequestHash      string
	ChargeID         string
}

// AcceptDelete 持久化接受删除：先封闭原创建的 provider op（阻止迟到创建），
// 再逐单元释放并登记释放事实；每释放一部分即可上报累计数量。
func (s *Simulator) AcceptDelete(ctx context.Context, in *AcceptDeleteInput) (bool, error) {
	// 幂等检查。
	var storedHash string
	err := s.owner.db.QueryRowContext(ctx,
		`SELECT request_hash FROM sim_commands WHERE operation_id=$1`, in.OperationID).Scan(&storedHash)
	idempotentReplay := false
	switch {
	case err == nil:
		if storedHash != in.RequestHash {
			return false, ErrPermanentConflict
		}
		// 幂等重放：继续清理/通知路径（幂等），不重复登记命令。
		idempotentReplay = true
	case errors.Is(err, sql.ErrNoRows):
	default:
		return false, err
	}

	// 原创建记录必须属于同租户和资源。
	var origChargeID string
	err = s.owner.db.QueryRowContext(ctx,
		`SELECT charge_id FROM sim_commands WHERE operation_id=$1 AND tenant_id=$2 AND resource_id=$3 AND kind='create'`,
		in.CreateOperationID, in.TenantID, in.ResourceID).Scan(&origChargeID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("SIMULATOR_NOT_FOUND: create operation not found for this tenant/resource")
	} else if err != nil {
		return false, err
	}

	// 1. 持久化 owner 删除命令（幂等重放时跳过插入）。
	if !idempotentReplay {
	if err = s.owner.withOwnerTx(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx,
			`INSERT INTO sim_commands
			   (operation_id, tenant_id, resource_id, create_operation_id, kind, status, actor, request_hash, charge_id)
			 VALUES ($1,$2,$3,$4,'delete',$5,$6,$7,$8)
			 ON CONFLICT (operation_id) DO NOTHING`,
			in.OperationID, in.TenantID, in.ResourceID, in.CreateOperationID, StatusAccepted, in.Actor, in.RequestHash, origChargeID)
		return e
	}); err != nil {
		return false, err
	}
	}

	// 2. 封闭原创建的 provider op（Fence 先于清理，§11.4）。
	if _, err = s.provider.db.ExecContext(ctx,
		`UPDATE sim_provider_ops SET closed=true, execution_generation=execution_generation+1 WHERE operation_id=$1`,
		in.CreateOperationID); err != nil {
		return false, err
	}
	// 3. 逐单元释放（幂等：已释放单元跳过；失败的单元保留占额继续重试）。
	for {
		done, _, rerr := s.releaseOneUnit(ctx, in.TenantID, in.ResourceID, origChargeID)
		if rerr != nil {
			// 已释放的部分已入账；剩余失败单元由恢复流程继续（§11.5）。
			break
		}
		if done {
			break
		}
	}

	// 4. owner 记录删除完成并入队退额通知（累计值由唯一事实求和）。
	if err = s.owner.withOwnerTx(ctx, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx,
			`UPDATE sim_commands SET status=$1, updated_at=now() WHERE operation_id=$2`,
			StatusCompleted, in.OperationID); e != nil {
			return e
		}
		return enqueueNotifyTx(ctx, tx, notifyInput{
			EventID:     "delete:" + in.OperationID,
			OperationID: in.CreateOperationID, // 退额指向原创建操作
			ChargeID:    origChargeID,
			TenantID:    in.TenantID,
			Reason:      "RESOURCE_RELEASED",
		})
	}); err != nil {
		return false, err
	}
	return true, nil
}

// releaseOneUnit 找到下一个仍为 allocated 的单元并释放；返回 (全部完成, 本次释放数, err)。
func (s *Simulator) releaseOneUnit(ctx context.Context, tenantID, resourceID, chargeID string) (bool, int, error) {
	tx, err := s.provider.db.BeginTx(ctx, nil)
	if err != nil {
		return false, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var ordinal int
	err = tx.QueryRowContext(ctx,
		`SELECT ordinal FROM sim_allocations WHERE resource_id=$1 AND tenant_id=$2 AND state='allocated' ORDER BY ordinal LIMIT 1 FOR UPDATE`,
		resourceID, tenantID).Scan(&ordinal)
	if errors.Is(err, sql.ErrNoRows) {
		_ = tx.Commit()
		return true, 0, nil
	} else if err != nil {
		return false, 0, err
	}
	if s.Fails.FailReleaseOrdinal == ordinal {
		return false, 0, fmt.Errorf("SIMULATOR_UNIT_FAIL: release unit %d injected failure", ordinal)
	}
	if _, err = tx.ExecContext(ctx,
		`UPDATE sim_allocations SET state='freed', updated_at=now() WHERE resource_id=$1 AND ordinal=$2`,
		resourceID, ordinal); err != nil {
		return false, 0, err
	}
	if err = tx.Commit(); err != nil {
		return false, 0, err
	}

	// provider 保留可查的已释放事实（墓碑）；owner 侧以 (charge_id, ordinal) 唯一入账。
	if err = s.owner.withOwnerTx(ctx, func(oTx *sql.Tx) error {
		if _, e := oTx.ExecContext(ctx,
			`INSERT INTO sim_units (charge_id, unit_ordinal, tenant_id, resource_id, state)
			 VALUES ($1,$2,$3,$4,'released') ON CONFLICT (charge_id, unit_ordinal) DO NOTHING`,
			chargeID, ordinal, tenantID, resourceID); e != nil {
			return e
		}
		if _, e := oTx.ExecContext(ctx,
			`INSERT INTO sim_release_facts (charge_id, unit_ordinal, tenant_id)
			 VALUES ($1,$2,$3) ON CONFLICT (charge_id, unit_ordinal) DO NOTHING`,
			chargeID, ordinal, tenantID); e != nil {
			return e
		}
		return nil
	}); err != nil {
		return false, 0, err
	}
	return false, 1, nil
}

// ── GetResource（纯读取，不推进） ────────────────────────────

// ResourceView 模拟资源视图。
type ResourceView struct {
	ResourceID string
	TenantID   string
	Name       string
	Status     string
	UnitCount  int
}

// GetResource 返回资源及模拟状态/单元数量；找不到返回 ErrNotFound。
var ErrNotFound = errors.New("SIMULATOR_NOT_FOUND: resource not found")

func (s *Simulator) GetResource(ctx context.Context, tenantID, resourceID string) (*ResourceView, error) {
	var name, status string
	var gpuCount sql.NullInt64
	err := s.owner.db.QueryRowContext(ctx,
		`SELECT name, status, gpu_count FROM sim_commands
		 WHERE tenant_id=$1 AND resource_id=$2 AND kind='create'
		 ORDER BY created_at DESC LIMIT 1`,
		tenantID, resourceID).Scan(&name, &status, &gpuCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	var unitCount int
	if err = s.provider.db.QueryRowContext(ctx,
		`SELECT count(*) FROM sim_allocations WHERE tenant_id=$1 AND resource_id=$2 AND state='allocated'`,
		tenantID, resourceID).Scan(&unitCount); err != nil {
		return nil, err
	}
	return &ResourceView{
		ResourceID: resourceID,
		TenantID:   tenantID,
		Name:       name,
		Status:     status,
		UnitCount:  unitCount,
	}, nil
}

// ── 重启恢复（§11.2.6：owner 重启后按 operation/resource ID 对账） ──

// Recover 对账：owner 状态落后于 provider 时按事实补齐，不重复分配。
func (s *Simulator) Recover(ctx context.Context) error {
	rows, err := s.owner.db.QueryContext(ctx,
		`SELECT operation_id, tenant_id, resource_id, gpu_count, charge_id, status, request_hash
		 FROM sim_commands WHERE kind='create' AND status=$1`, StatusAccepted)
	if err != nil {
		return err
	}
	type pend struct {
		OperationID, TenantID, ResourceID, ChargeID string
		Status                                      string
		RequestHash                                 string
		GpuCount                                    int32
	}
	var pending []pend
	for rows.Next() {
		var p pend
		if err = rows.Scan(&p.OperationID, &p.TenantID, &p.ResourceID, &p.GpuCount, &p.ChargeID, &p.Status, &p.RequestHash); err != nil {
			_ = rows.Close()
			return err
		}
		pending = append(pending, p)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	for _, p := range pending {
		// 查询 provider：已分配数量与封闭状态。
		var allocated int
		var closed bool
		err = s.provider.db.QueryRowContext(ctx,
			`SELECT coalesce((SELECT count(*) FROM sim_allocations WHERE resource_id=$1 AND tenant_id=$2 AND state='allocated'),0),
			        coalesce((SELECT closed FROM sim_provider_ops WHERE operation_id=$3), false)`,
			p.ResourceID, p.TenantID, p.OperationID).Scan(&allocated, &closed)
		if err != nil {
			return err
		}
		in := &AcceptCreateInput{
			OperationID: p.OperationID,
			ResourceID:  p.ResourceID,
			TenantID:    p.TenantID,
			RequestHash: p.RequestHash,
			ChargeID:    p.ChargeID,
			GpuCount:    p.GpuCount,
		}
		switch {
		case closed:
			// 已封闭： aborted 流程已在别处处理；重跑清理以收敛。
			if err = s.abortCreate(ctx, in, ErrClosed); err != nil {
				return fmt.Errorf("recover abort %s: %w", p.OperationID, err)
			}
		case allocated >= int(p.GpuCount):
			// provider 已创建：补记 owner 完成，不再分配。
			if _, err = s.owner.db.ExecContext(ctx,
				`UPDATE sim_commands SET status=$1, updated_at=now() WHERE operation_id=$2`,
				StatusCompleted, p.OperationID); err != nil {
				return err
			}
		default:
			// 继续处理（可能再次触发容量/故障路径）。
			if _, err = s.AcceptCreate(ctx, in); err != nil {
				return fmt.Errorf("recover accept %s: %w", p.OperationID, err)
			}
		}
	}
	return nil
}

// ── 退额通知入队（§11.5） ────────────────────────────────────

type notifyInput struct {
	EventID     string
	OperationID string // 原创建 operation_id
	ChargeID    string
	TenantID    string
	Reason      string
}

func enqueueNotifyTx(ctx context.Context, tx *sql.Tx, in notifyInput) error {
	// 累计释放量：从唯一事实求和（绝不“每次删除返回成功就 total+=1”）。
	var total int
	if err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM sim_release_facts WHERE charge_id=$1`, in.ChargeID).Scan(&total); err != nil {
		return err
	}
	// 事件 ID 由 (charge, 累计值, 原因) 派生：同一累计值重复通知幂等；
	// 累计增长产生新事件（Governance 对同 ID 异内容返回冲突，绝不能复用）。
	eventID := fmt.Sprintf("evt:%s:%d:%s", in.ChargeID, total, in.Reason)
	payload := fmt.Sprintf(`{"schema_version":1,"reason":%q,"operation_id":%q,"items":[{"charge_id":%q,"quota_code":"gpu.count","released_total":%d}]}`,
		in.Reason, in.OperationID, in.ChargeID, total)
	_, err := tx.ExecContext(ctx,
		`INSERT INTO sim_notify_queue (event_id, charge_id, operation_id, tenant_id, payload_hash, payload_json, reason)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (event_id) DO NOTHING`,
		eventID, in.ChargeID, in.OperationID, in.TenantID, hashOf(payload), payload, in.Reason)
	return err
}

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// 本文件对 auditDriver/auditTx 做白盒测试（SQLite 内存库）：
//
//	Exec/Query/Tx(含事务内 Exec/Query) 拦截后必须把事件追加进 ctx 携带的
//	accumulator（字段：脱敏 SQL 文本、其 SHA256 摘要、方言、读/写标记、
//	掩码标记与规则串）；查询路径 AffectedRows 恒为 -1；
//	IsSinking（审计落库阶段防递归标记）与无 accumulator 的 ctx 必须静默跳过采集；
//	digest/affectedRows 纯函数直接单测。
//
// 该包装层是数据访问审计（sys_data_access_audit_logs）的采集源头，
// 语义见 docs/audit-log-producer-design.md。
package data

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/require"

	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/pkg/audit"
)

// newAuditWrappedClient 构造经 auditDriver 包装的 SQLite 内存库 ent client
// 与对应 accumulator 上下文（迁移用无 accumulator 的裸 ctx，避免 DDL 混入计量）。
func newAuditWrappedClient(t *testing.T) (*ent.Client, context.Context, *[]audit.AuditEvent) {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	drv := entsql.OpenDB(dialect.SQLite, db)
	t.Cleanup(func() { _ = drv.Close() })
	client := ent.NewClient(ent.Driver(&auditDriver{drv}))
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Schema.Create(context.Background()), "SQLite schema 迁移失败")

	events := make([]audit.AuditEvent, 0)
	accCtx := context.WithValue(context.Background(), audit.AccumulatorKey(), &events)
	return client, accCtx, &events
}

// sha256Hex 与 digest 实现一致性参照。
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// TestAuditDriver_CollectsQueryEvent SELECT 查询经包装驱动必须产生
// 读型事件：方言 sqlite、IsWrite=false、AffectedRows=-1、文本为脱敏产物且摘要一致。
func TestAuditDriver_CollectsQueryEvent(t *testing.T) {
	client, accCtx, events := newAuditWrappedClient(t)

	_, err := client.Language.Query().All(accCtx)
	require.NoError(t, err, "空表查询返回空列表而非错误")
	// 查询已下发到驱动层，事件必须被采集
	require.NotEmpty(t, *events, "查询路径必须产生采集事件")
	for _, ev := range *events {
		require.Equal(t, "sqlite3", ev.Dialect)
		require.False(t, ev.IsWrite)
		require.Equal(t, int64(-1), ev.AffectedRows)
		require.True(t, ev.DataMasked)
		require.Equal(t, audit.MaskingRules, ev.MaskingRules)
		require.Equal(t, sha256Hex(ev.SqlText), ev.SqlDigest, "摘要必须与脱敏文本一致")
	}
}

// TestAuditDriver_CollectsWriteEvent INSERT 经包装驱动必须产生
// 写型事件：方言 sqlite、IsWrite=true、文本脱敏且摘要一致。
func TestAuditDriver_CollectsWriteEvent(t *testing.T) {
	client, accCtx, events := newAuditWrappedClient(t)

	_ = client.Language.Create().
		SetLanguageCode("en-x-audit").
		SetLanguageName("English").
		SaveX(accCtx)

	require.NotEmpty(t, *events, "写入路径必须产生采集事件")
	writes := 0
	for _, ev := range *events {
		require.Equal(t, "sqlite3", ev.Dialect)
		require.True(t, ev.DataMasked)
		require.Equal(t, audit.MaskingRules, ev.MaskingRules)
		require.Equal(t, sha256Hex(ev.SqlText), ev.SqlDigest)
		if ev.IsWrite {
			writes++
			require.Contains(t, []int64{-1, 1}, ev.AffectedRows, "写事件行数只允许 -1（取不到）或 1")
		}
	}
	require.NotZero(t, writes, "INSERT 必须产生至少一条写型事件（ent 在 RETURNING 方言上经 Query 通道执行，isWriteStatement 负责纠偏标记）")
}

// TestAuditDriver_TxEvents 事务内语句必须产生方言为 "tx" 的采集事件（读与写各一）。
func TestAuditDriver_TxEvents(t *testing.T) {
	client, accCtx, events := newAuditWrappedClient(t)

	tx, err := client.Tx(accCtx)
	require.NoError(t, err)
	_ = tx.Language.Create().
		SetLanguageCode("de-x-audit").
		SetLanguageName("Deutsch").
		SaveX(accCtx)
	_, _ = tx.Language.Query().All(accCtx)
	require.NoError(t, tx.Commit())

	txWrites, txReads := 0, 0
	for _, ev := range *events {
		if ev.Dialect != "tx" {
			continue
		}
		require.True(t, ev.DataMasked)
		require.Equal(t, audit.MaskingRules, ev.MaskingRules)
		require.Equal(t, sha256Hex(ev.SqlText), ev.SqlDigest)
		if ev.IsWrite {
			txWrites++
		} else {
			txReads++
			require.Equal(t, int64(-1), ev.AffectedRows)
		}
	}
	require.NotZero(t, txWrites, "事务内 INSERT 必须产生 tx 写事件")
	require.NotZero(t, txReads, "事务内 SELECT 必须产生 tx 读事件")
}

// TestAuditDriver_SinkAndNoAccumulator 防递归标记（IsSinking）与
// 无 accumulator 的上下文都必须跳过采集、不落事件、不 panic。
func TestAuditDriver_SinkAndNoAccumulator(t *testing.T) {
	client, _, events := newAuditWrappedClient(t)

	// 落库阶段标记 + accumulator：查询执行但不采集
	sinkEvents := make([]audit.AuditEvent, 0)
	sinkCtx := context.WithValue(
		context.WithValue(context.Background(), audit.AccumulatorKey(), &sinkEvents),
		audit.SinkKey(), true,
	)
	_, _ = client.Language.Query().All(sinkCtx)
	require.Empty(t, sinkEvents, "IsSinking 阶段不得采集")

	// 无 accumulator：执行不 panic、外部 events 不受影响
	_, _ = client.Language.Query().All(context.Background())
	require.Empty(t, *events, "无 accumulator 不得采集")
}

// TestDigest 摘要纯函数：与标准 SHA256 十六进制一致。
func TestDigest(t *testing.T) {
	require.Equal(t, sha256Hex("SELECT 1"), digest("SELECT 1"))
	require.Len(t, digest("x"), 64)
}

// TestIsWriteStatement 写型语句首关键字判定表：DML 关键字（含小写/前导空白/
// 前导括号变体）为写，其余（SELECT/DDL/空串/裸关键字名）为读。
func TestIsWriteStatement(t *testing.T) {
	writes := []string{
		"INSERT INTO t VALUES (1)",
		"insert into t values (1)",
		"  UPDATE t SET a=1",
		"\tDELETE FROM t",
		"(MERGE INTO t USING s ON 1=1)",
		"REPLACE INTO t VALUES (1)",
	}
	reads := []string{
		"SELECT * FROM t",
		"select 1",
		"CREATE TABLE t (a int)",
		"DROP TABLE t",
		"",
		"   ",
		"insertx into t",
	}
	for _, q := range writes {
		require.True(t, isWriteStatement(q), "%q 应判为写型", q)
	}
	for _, q := range reads {
		require.False(t, isWriteStatement(q), "%q 应判为读型", q)
	}
}

// rowsResultStub 实现 RowsAffected 接口的桩。
type rowsResultStub struct {
	n   int64
	err error
}

func (s rowsResultStub) RowsAffected() (int64, error) { return s.n, s.err }

// TestAffectedRows 行数提取的四个分支：正常值、错误、nil 载体、不实现接口的载体。
func TestAffectedRows(t *testing.T) {
	require.Equal(t, int64(5), affectedRows(rowsResultStub{n: 5}))
	require.Equal(t, int64(-1), affectedRows(rowsResultStub{err: context.Canceled}))
	require.Equal(t, int64(-1), affectedRows(nil))
	require.Equal(t, int64(-1), affectedRows(struct{}{}))
}

package data

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
)

// newRedisCacheMonitorRepoSqlite 白盒构造 RedisCacheMonitorRepo：
// 生产构造器 NewRedisCacheMonitorRepo 仅注入 logger 与 redis client，
// 这里以 NopLogger 复刻，redis client 传 miniredis 假实例（或 nil 以走降级分支）。
func newRedisCacheMonitorRepoSqlite(t *testing.T, rdb *redis.Client) *RedisCacheMonitorRepo {
	t.Helper()
	return &RedisCacheMonitorRepo{
		rdb: rdb,
		log: bLogger.NewHelper(bLogger.NopLogger()),
	}
}

// TestRedisCacheMonitorRepoSqlite_GetInfoNilRedis 覆盖降级分支：
// r 为 nil 或 r.rdb 为 nil 时返回空视图而非错误（fail-soft）。
func TestRedisCacheMonitorRepoSqlite_GetInfoNilRedis(t *testing.T) {
	ctx := context.Background()

	// rdb 为 nil
	repo := newRedisCacheMonitorRepoSqlite(t, nil)
	info, err := repo.GetInfo(ctx)
	require.NoError(t, err, "rdb 为 nil 时应返回空视图而非错误")
	require.NotNil(t, info)
	require.Empty(t, info.Sections)
	require.Zero(t, info.DbSize)
	require.Empty(t, info.Slowlog)

	// 接收者为 nil
	var nilRepo *RedisCacheMonitorRepo
	info, err = nilRepo.GetInfo(ctx)
	require.NoError(t, err, "nil 接收者应返回空视图而非错误")
	require.NotNil(t, info)
	require.Empty(t, info.Sections)
	require.Zero(t, info.DbSize)
	require.Empty(t, info.Slowlog)
}

// TestRedisCacheMonitorRepoSqlite_GetInfoFromMiniredis 在 miniredis 上验证
// GetInfo 的三类指标聚合：INFO 解析出的 section/entry 结构、DBSIZE 的 key 计数、
// SLOWLOG 未被 miniredis 实现时走错误分支留空；随后 SetError 注入全命令失败，
// 验证整体 fail-soft（各字段置空、不向调用方返回错误）。
func TestRedisCacheMonitorRepoSqlite_GetInfoFromMiniredis(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	ctx := context.Background()
	require.NoError(t, rdb.Set(ctx, "k1", "v1", 0).Err())
	require.NoError(t, rdb.Set(ctx, "k2", "v2", 0).Err())

	repo := newRedisCacheMonitorRepoSqlite(t, rdb)
	info, err := repo.GetInfo(ctx)
	require.NoError(t, err, "miniredis 正常路径下 GetInfo 不应返回错误")
	require.NotNil(t, info)

	// DBSIZE：当前库 key 计数应为 2
	require.EqualValues(t, 2, info.DbSize, "DBSIZE 应反映 miniredis 中的 2 个 key")

	// INFO：miniredis 返回 # Clients/# Stats 两节，按节名收集 entry key 做断言
	entriesByName := map[string][]string{}
	for _, s := range info.Sections {
		keys := make([]string, 0, len(s.Entries))
		for _, e := range s.Entries {
			keys = append(keys, e.Key)
		}
		entriesByName[s.Name] = keys
	}
	require.Contains(t, entriesByName, "Clients", "miniredis INFO 应含 Clients 节")
	require.Contains(t, entriesByName["Clients"], "connected_clients", "Clients 节应含 connected_clients 条目")
	require.Contains(t, entriesByName, "Stats", "miniredis INFO 应含 Stats 节")
	require.Contains(t, entriesByName["Stats"], "total_connections_received", "Stats 节应含 total_connections_received 条目")
	require.Contains(t, entriesByName["Stats"], "total_commands_processed", "Stats 节应含 total_commands_processed 条目")

	// SLOWLOG：miniredis 未实现该命令 → 错误分支 → 留空
	require.Empty(t, info.Slowlog, "SLOWLOG 命令失败时应留空 Slowlog")

	// 全命令错误注入：三段各自失败，整体仍返回空视图且无错误
	mr.SetError("boom")
	info, err = repo.GetInfo(ctx)
	require.NoError(t, err, "SetError 后 GetInfo 仍应 fail-soft 返回空视图")
	require.NotNil(t, info)
	require.Empty(t, info.Sections)
	require.Zero(t, info.DbSize)
	require.Empty(t, info.Slowlog)
}

// TestRedisCacheMonitorRepoSqlite_ParseInfoSections 直接单测 parseInfoSections：
// 节头归属、CRLF 裁剪、首个冒号切分、畸形节头（空名）终止归属、
// 孤立条目/无冒号行/非节头前缀行被忽略、空输入返回 nil。
func TestRedisCacheMonitorRepoSqlite_ParseInfoSections(t *testing.T) {
	// 常规：两个 section 各自归属自己的条目
	got := parseInfoSections("# A\nk1:v1\nk2:v2\n\n# B\nk3:v3\n")
	require.Len(t, got, 2)
	require.Equal(t, "A", got[0].Name)
	require.Len(t, got[0].Entries, 2)
	require.Equal(t, "k1", got[0].Entries[0].Key)
	require.Equal(t, "v1", got[0].Entries[0].Value)
	require.Equal(t, "k2", got[0].Entries[1].Key)
	require.Equal(t, "v2", got[0].Entries[1].Value)
	require.Equal(t, "B", got[1].Name)
	require.Len(t, got[1].Entries, 1)
	require.Equal(t, "k3", got[1].Entries[0].Key)
	require.Equal(t, "v3", got[1].Entries[0].Value)

	// CRLF 行尾：\r 被裁剪，条目仍可解析
	got = parseInfoSections("# A\r\nk1:v1\r\n")
	require.Len(t, got, 1)
	require.Equal(t, "A", got[0].Name)
	require.Len(t, got[0].Entries, 1)
	require.Equal(t, "k1", got[0].Entries[0].Key)
	require.Equal(t, "v1", got[0].Entries[0].Value)

	// 值中的后续冒号不参与切分：首个冒号为准
	got = parseInfoSections("# A\nhost:port:extra\n")
	require.Len(t, got, 1)
	require.Len(t, got[0].Entries, 1)
	require.Equal(t, "host", got[0].Entries[0].Key)
	require.Equal(t, "port:extra", got[0].Entries[0].Value)

	// Keyspace 形态（db0:keys=...）：按通用 kv 落入，无特殊列
	got = parseInfoSections("# Keyspace\ndb0:keys=1,expires=0,avg_ttl=0\n")
	require.Len(t, got, 1)
	require.Equal(t, "Keyspace", got[0].Name)
	require.Len(t, got[0].Entries, 1)
	require.Equal(t, "db0", got[0].Entries[0].Key)
	require.Equal(t, "keys=1,expires=0,avg_ttl=0", got[0].Entries[0].Value)

	// 畸形节头（"#" 后空名）：current 置 nil，其后条目忽略；后续合法节恢复归属
	got = parseInfoSections("# A\nk1:v1\n# \nok1:dropped\n#nospace\nok2:dropped\n# B\nk2:v2\n")
	require.Len(t, got, 2)
	require.Equal(t, "A", got[0].Name)
	require.Len(t, got[0].Entries, 1)
	require.Equal(t, "k1", got[0].Entries[0].Key)
	require.Equal(t, "B", got[1].Name)
	require.Len(t, got[1].Entries, 1)
	require.Equal(t, "k2", got[1].Entries[0].Key)

	// 孤立条目（无归属节）被忽略
	got = parseInfoSections("orphan:1\n# A\nk1:v1\n")
	require.Len(t, got, 1)
	require.Len(t, got[0].Entries, 1)
	require.Equal(t, "k1", got[0].Entries[0].Key)

	// 无冒号的行被忽略
	got = parseInfoSections("# A\nnocolon\nk1:v1\n")
	require.Len(t, got, 1)
	require.Len(t, got[0].Entries, 1)
	require.Equal(t, "k1", got[0].Entries[0].Key)

	// 空输入 / 纯空白行 → 无节
	require.Nil(t, parseInfoSections(""))
	require.Nil(t, parseInfoSections("\n\n\r\n"))
}

// TestRedisCacheMonitorRepoSqlite_MapSlowLogEntries 直接单测 mapSlowLogEntries：
// 空输入返回 nil；字段一一映射（Id/CreatedAt/DurationUsec/Args/ClientAddr/ClientName）；
// 无效 UTF-8 的 Args 与 ClientName 被净化为 U+FFFD；空 Args 映射为 nil。
func TestRedisCacheMonitorRepoSqlite_MapSlowLogEntries(t *testing.T) {
	// 空输入 → nil
	require.Nil(t, mapSlowLogEntries(nil))
	require.Nil(t, mapSlowLogEntries([]redis.SlowLog{}))

	ts := time.Unix(1735689600, 0)

	// 常规字段映射
	in := []redis.SlowLog{{
		ID:         42,
		Time:       ts,
		Duration:   1500 * time.Microsecond,
		Args:       []string{"GET", "somekey"},
		ClientAddr: "203.0.113.7:51111",
		ClientName: "quota-worker",
	}}
	out := mapSlowLogEntries(in)
	require.Len(t, out, 1)
	require.Equal(t, int64(42), out[0].Id)
	require.NotNil(t, out[0].CreatedAt)
	require.Equal(t, ts.Unix(), out[0].CreatedAt.AsTime().Unix(), "CreatedAt 应保留原时间戳")
	require.Equal(t, int64(1500), out[0].DurationUsec, "DurationUsec 应为原时长的微秒数")
	require.Equal(t, []string{"GET", "somekey"}, out[0].Args)
	require.Equal(t, "203.0.113.7:51111", out[0].ClientAddr)
	require.Equal(t, "quota-worker", out[0].ClientName)

	// 无效 UTF-8 净化：Args 与 ClientName 中的非法字节串替换为 U+FFFD
	in = []redis.SlowLog{{
		ID:         7,
		Time:       ts,
		Duration:   time.Millisecond,
		Args:       []string{"\xff\xfe", "ok"},
		ClientAddr: "198.51.100.9:60001",
		ClientName: "bad\xffname",
	}}
	out = mapSlowLogEntries(in)
	require.Len(t, out, 1)
	require.Len(t, out[0].Args, 2)
	require.Equal(t, "\uFFFD", out[0].Args[0], "整段无效 UTF-8 应被替换为单个 U+FFFD")
	require.Equal(t, "ok", out[0].Args[1], "合法 UTF-8 参数应原样保留")
	require.Equal(t, "bad\uFFFDname", out[0].ClientName, "ClientName 中的无效字节应被替换为 U+FFFD")

	// 空 Args → nil（sanitize 空 slice 分支）
	in = []redis.SlowLog{{ID: 8, Time: ts, Duration: time.Millisecond}}
	out = mapSlowLogEntries(in)
	require.Len(t, out, 1)
	require.Nil(t, out[0].Args)
}

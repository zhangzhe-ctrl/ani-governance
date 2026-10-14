package entgo

import (
	"errors"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"

	paginationV1 "go-wind-admin/pkg/localdeps/go-crud/api/gen/go/pagination/v1"
	paginationCurd "go-wind-admin/pkg/localdeps/go-crud/pagination"
)

// TestCursorFilterKeyStableAndBoundToSort 验证游标绑定指纹：
// 同一筛选 + 同一排序得到同一指纹；筛选、排序列、排序方向任一变化都要换指纹，
// 否则旧游标会被静默套用到另一组结果集上。
func TestCursorFilterKeyStableAndBoundToSort(t *testing.T) {
	req := &paginationV1.PagingRequest{
		FilteringType: &paginationV1.PagingRequest_Query{Query: `{"username__contains":"alice"}`},
	}
	base := CursorFilterKey(req, "id", true)
	if base == "" {
		t.Fatal("CursorFilterKey returned empty")
	}
	if again := CursorFilterKey(req, "id", true); again != base {
		t.Fatalf("same filter and sort must produce the same key: %q vs %q", base, again)
	}

	otherFilter := &paginationV1.PagingRequest{
		FilteringType: &paginationV1.PagingRequest_Query{Query: `{"username__contains":"bob"}`},
	}
	if got := CursorFilterKey(otherFilter, "id", true); got == base {
		t.Error("a different filter must change the key")
	}
	if got := CursorFilterKey(req, "username", true); got == base {
		t.Error("a different sort column must change the key")
	}
	if got := CursorFilterKey(req, "id", false); got == base {
		t.Error("a different sort direction must change the key")
	}
	if got := CursorFilterKey(nil, "id", true); got != "" {
		t.Errorf("nil request must produce an empty key, got %q", got)
	}
}

// TestParseCursorSortValue 验证游标里的排序值按列类型还原：
// 时间列用 FormatCursorTime 往返后仍是同一时刻（纳秒精度不丢），
// nil 明确表示 SQL NULL，非法取值一律报错而不是退化成零值。
func TestParseCursorSortValue(t *testing.T) {
	strPtr := func(v string) *string { return &v }

	// NULL 排序值：必须原样报"是 NULL"，不能被当成空串/零值
	if value, isNull, err := parseCursorSortValue(nil, CursorSortString); err != nil || !isNull || value != nil {
		t.Fatalf("nil sort key must decode as NULL, got (%#v, %v, %v)", value, isNull, err)
	}

	// 时间列往返：FormatCursorTime 与 parseCursorSortValue 必须互逆。
	original := time.Date(2026, 10, 10, 6, 43, 0, 988175700, time.UTC)
	encoded := FormatCursorTime(original)
	decoded, isNull, err := parseCursorSortValue(&encoded, CursorSortTime)
	if err != nil {
		t.Fatalf("parse cursor time failed: %v", err)
	}
	if isNull {
		t.Fatal("time sort value must not decode as NULL")
	}
	gotTime, ok := decoded.(time.Time)
	if !ok {
		t.Fatalf("time sort kind must decode to time.Time, got %T", decoded)
	}
	if !gotTime.Equal(original) {
		t.Errorf("time round trip lost the instant: %v vs %v", gotTime, original)
	}

	// 整型列往返
	decoded, _, err = parseCursorSortValue(strPtr("42"), CursorSortUint)
	if err != nil {
		t.Fatalf("parse cursor uint failed: %v", err)
	}
	if value, ok := decoded.(uint64); !ok || value != 42 {
		t.Errorf("uint sort kind must decode to uint64(42), got %#v", decoded)
	}

	// 字符串列原样返回（空串是合法的非 NULL 值）
	decoded, _, err = parseCursorSortValue(strPtr("alice"), CursorSortString)
	if err != nil {
		t.Fatalf("parse cursor string failed: %v", err)
	}
	if value, ok := decoded.(string); !ok || value != "alice" {
		t.Errorf("string sort kind must decode to string, got %#v", decoded)
	}
	if decoded, isNull, err = parseCursorSortValue(strPtr(""), CursorSortString); err != nil || isNull {
		t.Fatalf("empty string must stay a non-NULL value, got (%#v, %v, %v)", decoded, isNull, err)
	}

	// 非法取值：报错，不静默退化为零值
	if _, _, err = parseCursorSortValue(strPtr("not-a-time"), CursorSortTime); err == nil {
		t.Error("malformed time sort value must be rejected")
	}
	if _, _, err = parseCursorSortValue(strPtr("-1"), CursorSortUint); err == nil {
		t.Error("malformed uint sort value must be rejected")
	}
	if _, _, err = parseCursorSortValue(strPtr("1"), CursorSortKind(99)); err == nil {
		t.Error("unknown sort kind must be rejected")
	}
}

// TestBuildCursorKeysetSelectorDegenerateCases 验证键集谓词的两个分支：
// 排序列即唯一 ID（非空）时退化为单纯 ID 比较；排序值非法时报错（fail-closed）。
func TestBuildCursorKeysetSelectorDegenerateCases(t *testing.T) {
	strPtr := func(v string) *string { return &v }

	spec := CursorSpec[struct{}]{
		SortColumn: "id",
		IDColumn:   "id",
		Desc:       true,
		SortKind:   CursorSortUint,
	}
	if _, err := buildCursorKeysetSelector(spec, &paginationCurd.Cursor{SortKey: strPtr("10"), ID: 10}); err != nil {
		t.Fatalf("degenerate id-only keyset must build: %v", err)
	}

	timeSpec := CursorSpec[struct{}]{
		SortColumn: "created_at",
		IDColumn:   "id",
		Desc:       false,
		SortKind:   CursorSortTime,
	}
	_, err := buildCursorKeysetSelector(timeSpec, &paginationCurd.Cursor{SortKey: strPtr("bogus"), ID: 1})
	if err == nil {
		t.Fatal("invalid sort value must fail closed")
	}
	if errors.Is(err, paginationCurd.ErrInvalidCursor) {
		t.Errorf("keyset error should be a plain error for the caller to wrap, got %v", err)
	}
}

// TestBuildCursorKeysetSelectorNullableSort 用真实 SQL 文本验证可空排序列的四种键集分支：
// 降序（NULL 在前）只需处理"游标在 NULL 区段内"的情形；升序（NULL 在后）在非 NULL
// 游标下必须额外放行 NULL 行，否则跨页会漏掉整个 NULL 区段。
func TestBuildCursorKeysetSelectorNullableSort(t *testing.T) {
	strPtr := func(v string) *string { return &v }

	keysetSQL := func(desc bool, sortKey *string) (string, []any) {
		t.Helper()
		spec := CursorSpec[struct{}]{SortColumn: "nickname", IDColumn: "id", Desc: desc, SortKind: CursorSortString}
		selector, err := buildCursorKeysetSelector(spec, &paginationCurd.Cursor{SortKey: sortKey, ID: 7})
		if err != nil {
			t.Fatalf("build keyset failed: %v", err)
		}
		s := sql.Dialect(dialect.Postgres).Select("*").From(sql.Table("sys_users"))
		selector(s)
		return s.Query()
	}

	// 降序 + 非 NULL 游标：纯值比较，不含 NULL 条件（NULL 行全部已在游标之前）
	query, args := keysetSQL(true, strPtr("alice"))
	if strings.Contains(query, "IS NULL") {
		t.Errorf("desc non-null keyset must not reference NULL: %s", query)
	}
	if !strings.Contains(query, `"id" < `) {
		t.Errorf("desc non-null keyset must keep the id tiebreaker: %s", query)
	}
	if len(args) != 3 {
		t.Errorf("desc non-null keyset args = %#v, want 3 (k, k, id)", args)
	}

	// 降序 + NULL 游标：NULL 区段内按 id 继续推进，并放行其后的全部非 NULL 行
	query, args = keysetSQL(true, nil)
	if !strings.Contains(query, "IS NULL") || !strings.Contains(query, "IS NOT NULL") {
		t.Errorf("desc null keyset must cover both the NULL block and later non-NULL rows: %s", query)
	}
	if len(args) != 1 {
		t.Errorf("desc null keyset args = %#v, want 1 (id only)", args)
	}

	// 升序 + 非 NULL 游标：NULL 行永远在后，必须无条件放行
	query, args = keysetSQL(false, strPtr("alice"))
	if !strings.Contains(query, "IS NULL") || strings.Contains(query, "IS NOT NULL") {
		t.Errorf("asc non-null keyset must include trailing NULL rows exactly once: %s", query)
	}
	if len(args) != 3 {
		t.Errorf("asc non-null keyset args = %#v, want 3 (k, k, id)", args)
	}

	// 升序 + NULL 游标：只剩 NULL 区段内 id 更大的行
	query, args = keysetSQL(false, nil)
	if !strings.Contains(query, "IS NULL") || strings.Contains(query, "IS NOT NULL") {
		t.Errorf("asc null keyset must stay inside the NULL block: %s", query)
	}
	if len(args) != 1 {
		t.Errorf("asc null keyset args = %#v, want 1 (id only)", args)
	}
}

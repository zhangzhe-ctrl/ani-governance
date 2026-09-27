// T08 本地化合同测试：分页过滤链必须通过 pkg/localdeps 的 codec 注册表取得 JSON 编解码器，
// 且该注册表与 encoding/json 的 init 注册是同一进程内唯一实例。

package filter

import (
	"testing"

	"go-wind-admin/pkg/localdeps/go-wind-plugins/encoding"

	paginationV1 "go-wind-admin/pkg/localdeps/go-crud/api/gen/go/pagination/v1"
)

func TestT08FilterResolvesTheLocalizedJSONCodec(t *testing.T) {
	qsc := NewQueryStringConverter()
	if qsc.codec == nil {
		t.Fatal("codec \"json\" is not registered: pkg/localdeps/go-wind-plugins/encoding/json init did not run")
	}
	if name := qsc.codec.Name(); name != "json" {
		t.Errorf("converter codec name = %q, want json", name)
	}
	// 注册表按名字大小写不敏感；两次查找必须命中同一条注册项。
	if encoding.GetCodec("JSON") == nil || encoding.GetCodec("JSON").Name() != qsc.codec.Name() {
		t.Errorf("case-insensitive lookup disagrees with the converter's codec")
	}

	type cursor struct {
		LastID int64 `json:"last_id"`
	}
	raw, err := qsc.codec.Marshal(cursor{LastID: 7})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back cursor
	if err := qsc.codec.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.LastID != 7 {
		t.Errorf("round-trip = %d, want 7", back.LastID)
	}

	// 转换器确实使用这个 codec：非法 JSON 必须报错，不能静默变成空条件。
	if _, err := qsc.QueryStringToMap("{"); err == nil {
		t.Error("malformed JSON was accepted without an error")
	}
	if got, err := qsc.QueryStringToMap(""); err != nil || got != nil {
		t.Errorf("empty query = %#v, %v; want nil, nil", got, err)
	}
}

func TestT08SearchOperatorIsKnownToTheLocalizedConverter(t *testing.T) {
	// SEARCH 是本轮空白/数值/时间检索用例依赖的操作符：本地化后枚举值与字符串映射必须同时可用。
	if !IsValidOperatorString("search") {
		t.Fatal("the localized operator table no longer knows \"search\"")
	}
	if got := ConverterStringToOperator("search"); got != paginationV1.Operator_SEARCH {
		t.Fatalf("ConverterStringToOperator(\"search\") = %v, want %v", got, paginationV1.Operator_SEARCH)
	}
	expr, err := NewQueryStringConverter().Convert(`{"quota_value__search":" "}`)
	if err != nil {
		t.Fatalf("SEARCH query is not convertible by the localized filter layer: %v", err)
	}
	if len(expr.GetConditions()) != 1 {
		t.Fatalf("SEARCH produced %d conditions, want 1", len(expr.GetConditions()))
	}
	cond := expr.GetConditions()[0]
	if cond.GetOp() != paginationV1.Operator_SEARCH {
		t.Errorf("condition op = %v, want SEARCH", cond.GetOp())
	}
	if cond.GetField() != "quota_value" || cond.GetValue() != " " {
		t.Errorf("condition = %q/%q, want quota_value/\" \"", cond.GetField(), cond.GetValue())
	}
}

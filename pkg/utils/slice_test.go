package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFilterBlacklist_RemovesBlacklistedEntries 黑名单中的条目应被滤除。
func TestFilterBlacklist_RemovesBlacklistedEntries(t *testing.T) {
	data := []string{"a", "b", "c", "d", "e"}
	blacklist := []string{"b", "d"}
	got := FilterBlacklist(data, blacklist)
	assert.Equal(t, []string{"a", "c", "e"}, got)
}

// TestFilterBlacklist_NoOverlap 数据与黑名单无交集时，结果与输入一致。
func TestFilterBlacklist_NoOverlap(t *testing.T) {
	data := []string{"a", "b", "c"}
	blacklist := []string{"x", "y"}
	got := FilterBlacklist(data, blacklist)
	assert.Equal(t, []string{"a", "b", "c"}, got)
}

// TestFilterBlacklist_AllBlacklisted 全部命中黑名单时，结果为空切片（非 nil）。
func TestFilterBlacklist_AllBlacklisted(t *testing.T) {
	data := []string{"a", "b"}
	blacklist := []string{"a", "b"}
	got := FilterBlacklist(data, blacklist)
	assert.Empty(t, got)
}

// TestFilterBlacklist_EmptyBlacklist 黑名单为空时，数据全部保留。
func TestFilterBlacklist_EmptyBlacklist(t *testing.T) {
	data := []string{"a", "b", "c"}
	got := FilterBlacklist(data, []string{})
	assert.Equal(t, []string{"a", "b", "c"}, got)
}

// TestFilterBlacklist_EmptyData 数据为空时，结果为空。
func TestFilterBlacklist_EmptyData(t *testing.T) {
	got := FilterBlacklist([]string{}, []string{"a"})
	assert.Empty(t, got)
}

// TestFilterBlacklist_NilBlacklist 黑名单为 nil 不应 panic，数据应全部保留。
func TestFilterBlacklist_NilBlacklist(t *testing.T) {
	data := []string{"a", "b"}
	//nolint:staticcheck // 故意传 nil 测试健壮性
	got := FilterBlacklist(data, nil)
	assert.Equal(t, []string{"a", "b"}, got)
}

// TestFilterBlacklist_DoesNotContainBlacklisted 验证返回切片中确实不存在黑名单项。
func TestFilterBlacklist_DoesNotContainBlacklisted(t *testing.T) {
	data := []string{"keep", "drop1", "keep2", "drop2"}
	blacklist := []string{"drop1", "drop2"}
	got := FilterBlacklist(data, blacklist)
	for _, g := range got {
		for _, b := range blacklist {
			assert.NotEqual(t, b, g, "结果中不应包含黑名单项 %q", b)
		}
	}
}

// TestNumberSliceToString_Normal 数字切片应转为逗号分隔的字符串。
func TestNumberSliceToString_Normal(t *testing.T) {
	got := NumberSliceToString([]uint32{1, 2, 3})
	assert.Equal(t, "1,2,3", got)
}

// TestNumberSliceToString_Empty 空切片应转为空字符串。
func TestNumberSliceToString_Empty(t *testing.T) {
	got := NumberSliceToString([]uint32{})
	assert.Equal(t, "", got)
}

// TestNumberSliceToString_SingleElement 单元素切片不含分隔符。
func TestNumberSliceToString_SingleElement(t *testing.T) {
	got := NumberSliceToString([]uint32{42})
	assert.Equal(t, "42", got)
}

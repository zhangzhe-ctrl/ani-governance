// T08 本地化合同测试：entgo 分页器与游标编解码必须是同一进程实例。
// 写入方（pkg/localdeps/go-crud/pagination 的 SetTokenSecret/EncodeAndSign）与
// 读取方（本包 BuildSelector 里的 VerifyAndDecode/TokenSecret）一旦分裂成两份
// 运行包，下面的签名游标就会静默退化成只带 LIMIT 的查询。

package pagination

import (
	"strings"
	"testing"

	"entgo.io/ent/dialect/sql"

	"go-wind-admin/pkg/localdeps/go-crud/pagination"
)

func withTokenSecret(t *testing.T, secret []byte) {
	t.Helper()
	previous := pagination.TokenSecret()
	pagination.SetTokenSecret(secret)
	t.Cleanup(func() { pagination.SetTokenSecret(previous) })
}

func tokenCursorSQL(t *testing.T, token string, pageSize int) string {
	t.Helper()
	sel := NewTokenPaginator().BuildSelector(token, pageSize)
	s := sql.Select("*").From(sql.Table("plan_quotas"))
	sel(s)
	q, _ := s.Query()
	return q
}

func TestT08TokenPaginatorReadsTheSharedSecretInstance(t *testing.T) {
	withTokenSecret(t, []byte("t08-shared-token-secret-0000000000000000"))
	token := pagination.EncodeAndSign(int64(42), pagination.TokenSecret())
	if !pagination.IsSignedToken(token) {
		t.Fatalf("EncodeAndSign did not produce a signed token: %q", token)
	}
	q := tokenCursorSQL(t, token, 20)
	// ent 默认方言用反引号包裹标识符，游标值以占位符绑定（不拼接进 SQL 文本）。
	if !strings.Contains(q, "WHERE `id` > ?") {
		t.Fatalf("the paginator did not decode a token signed with the global secret; secret instances are split: %q", q)
	}
	if !strings.Contains(q, "LIMIT 20") {
		t.Errorf("page size not applied: %q", q)
	}
}

func TestT08TokenPaginatorFailsClosedOnTamperedOrForeignTokens(t *testing.T) {
	withTokenSecret(t, []byte("t08-shared-token-secret-0000000000000000"))
	valid := pagination.EncodeAndSign(int64(42), pagination.TokenSecret())

	cases := map[string]string{
		"signature-flipped": valid[:len(valid)-1] + "0",
		"payload-extended":  valid + "x",
		"foreign-secret":    pagination.EncodeAndSign(int64(42), []byte("another-secret-0000000000000000000000000000")),
		"legacy-unsigned":   pagination.EncodeAndSign(int64(42), nil),
		"empty":             "",
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			q := tokenCursorSQL(t, token, 5)
			if strings.Contains(q, "WHERE") {
				t.Errorf("tampered token was trusted: %q -> %q", name, q)
			}
			if !strings.Contains(q, "LIMIT 5") {
				t.Errorf("rejected token must still clamp the page size: %q", q)
			}
		})
	}
}

func TestT08TokenPaginatorKeepsUnsignedModeBackwardCompatible(t *testing.T) {
	withTokenSecret(t, nil)
	legacy := pagination.EncodeAndSign(int64(7), nil)
	if pagination.IsSignedToken(legacy) {
		t.Fatalf("no secret must not yield a v2 token: %q", legacy)
	}
	if id, ok := pagination.VerifyAndDecode(legacy, pagination.TokenSecret()); !ok || id != 7 {
		t.Errorf("legacy token without secret = (%d, %v), want (7, true)", id, ok)
	}
}

//go:build quota_pg

package data

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	"go-wind-admin/app/admin/service/internal/data/enttest"
	paginationV1 "go-wind-admin/pkg/localdeps/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/pkg/localdeps/go-crud/pagination"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestPlanQuotaPostgresListCompatibility(t *testing.T) {
	c := enttest.NewQuotaPGClient(t)
	r := newPlanQuotaRepoPostgres(t, c)
	ctx := enttest.NewSystemViewerCtx(context.Background())
	p, e := c.Client().Plan.Create().SetName("paging-fixture").Save(ctx)
	require.NoError(t, e)
	for i, code := range []string{"user.count", "storage.bytes", "gpu.count", "gpu.shared_memory_mib"} {
		value := uint64((i + 1) * 10)
		if i == 3 {
			value = 30
		}
		require.NoError(t, r.Create(ctx, &identityV1.CreatePlanQuotaRequest{Data: &identityV1.PlanQuota{PlanId: &p.ID, QuotaCode: ptr(code), QuotaValue: &value, CreatedBy: ptr(uint32(7))}}))
	}
	all, e := r.List(ctx, &paginationV1.PagingRequest{})
	require.NoError(t, e)
	require.Len(t, all.Items, 4)
	require.NotNil(t, all.Items[0].CreatedAt)
	require.Equal(t, uint32(7), all.Items[0].GetCreatedBy())
	query := func(value string) *paginationV1.PagingRequest {
		return &paginationV1.PagingRequest{FilteringType: &paginationV1.PagingRequest_Query{Query: value}}
	}
	condition := func(field string, op paginationV1.Operator, value string) *paginationV1.PagingRequest {
		return &paginationV1.PagingRequest{FilteringType: &paginationV1.PagingRequest_FilterExpr{FilterExpr: &paginationV1.FilterExpr{Type: paginationV1.ExprType_AND, Conditions: []*paginationV1.FilterCondition{{Field: field, Op: op, ValueOneof: &paginationV1.FilterCondition_Value{Value: value}}}}}}
	}
	cases := []struct {
		name string
		req  *paginationV1.PagingRequest
		want []string
	}{
		{"empty", &paginationV1.PagingRequest{}, []string{"user.count", "storage.bytes", "gpu.count", "gpu.shared_memory_mib"}},
		{"timestamp-offset", condition("created_at", paginationV1.Operator_EQ, all.Items[0].CreatedAt.AsTime().In(time.FixedZone("fixture", 8*3600)).Format(time.RFC3339Nano)), []string{"user.count"}},
		{"timestamp-bound", condition("created_at", paginationV1.Operator_LT, "2000-01-01"), nil},
		{"numeric-query", query(`{"quotaValue__gte":20}`), []string{"storage.bytes", "gpu.count", "gpu.shared_memory_mib"}},
		{"and-or", query(`{"$and":[{"quotaValue__gte":20},{"$or":[{"quotaCode":"gpu.count"},{"quotaCode":"storage.bytes"}]}]}`), []string{"storage.bytes", "gpu.count"}},
		{"numeric-in", query(`{"quotaValue__in":[10,30]}`), []string{"user.count", "gpu.count", "gpu.shared_memory_mib"}},
		{"not-in", condition("quota_type", paginationV1.Operator_NIN, `["STORAGE"]`), []string{"user.count"}},
		{"range", condition("quota_value", paginationV1.Operator_BETWEEN, `[10,20]`), []string{"user.count", "storage.bytes"}},
		{"nullable-neq", condition("quota_type", paginationV1.Operator_NEQ, "STORAGE"), []string{"user.count"}},
		{"null", condition("quota_type", paginationV1.Operator_IS_NULL, ""), []string{"gpu.count", "gpu.shared_memory_mib"}},
		{"not-null", condition("quota_type", paginationV1.Operator_IS_NOT_NULL, ""), []string{"user.count", "storage.bytes"}},
		{"contains", condition("quota_code", paginationV1.Operator_CONTAINS, ".count"), []string{"user.count", "gpu.count"}},
		{"icontains", condition("quota_code", paginationV1.Operator_ICONTAINS, "COUNT"), []string{"user.count", "gpu.count"}},
		{"prefix", condition("quota_code", paginationV1.Operator_STARTS_WITH, "gpu.c"), []string{"gpu.count"}},
		{"iprefix", condition("quota_code", paginationV1.Operator_ISTARTS_WITH, "GPU.C"), []string{"gpu.count"}},
		{"suffix", condition("quota_code", paginationV1.Operator_ENDS_WITH, "bytes"), []string{"storage.bytes"}},
		{"isuffix", condition("quota_code", paginationV1.Operator_IENDS_WITH, "BYTES"), []string{"storage.bytes"}},
		{"exact", condition("quota_code", paginationV1.Operator_EXACT, "gpu.count"), []string{"gpu.count"}},
		{"iexact", condition("quota_code", paginationV1.Operator_IEXACT, "GPU.COUNT"), []string{"gpu.count"}},
		{"regex", condition("quota_code", paginationV1.Operator_REGEXP, `^(user|gpu)\.count$`), []string{"user.count", "gpu.count"}},
		{"iregex", condition("quota_code", paginationV1.Operator_IREGEXP, `^GPU\.C`), []string{"gpu.count"}},
		{"search", condition("quota_type", paginationV1.Operator_SEARCH, "storage"), []string{"storage.bytes"}},
		{"blank-search", condition("quota_type", paginationV1.Operator_SEARCH, " "), []string{"user.count", "storage.bytes", "gpu.count", "gpu.shared_memory_mib"}},
		{"numeric-blank-search", condition("quota_value", paginationV1.Operator_SEARCH, " "), []string{"user.count", "storage.bytes", "gpu.count", "gpu.shared_memory_mib"}},
		{"numeric-search", condition("quota_value", paginationV1.Operator_SEARCH, "10"), []string{"user.count"}},
		{"timestamp-blank-search", condition("created_at", paginationV1.Operator_SEARCH, " "), []string{"user.count", "storage.bytes", "gpu.count", "gpu.shared_memory_mib"}},
		{"timestamp-search", condition("created_at", paginationV1.Operator_SEARCH, all.Items[0].CreatedAt.AsTime().Format("2006-01-02")), []string{"user.count", "storage.bytes", "gpu.count", "gpu.shared_memory_mib"}},
		{"aip", &paginationV1.PagingRequest{FilteringType: &paginationV1.PagingRequest_Filter{Filter: `quota_value > 10 AND quota_value <= 20`}}, []string{"storage.bytes"}},
		{"quoted-literal", condition("quota_code", paginationV1.Operator_EQ, `gpu.count") || @.id > 0 || ("`), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, e := r.List(ctx, tc.req)
			require.NoError(t, e)
			require.Equal(t, uint64(len(tc.want)), got.Total)
			var codes []string
			for _, v := range got.Items {
				codes = append(codes, v.GetQuotaCode())
			}
			require.Equal(t, tc.want, codes)
		})
	}
	t.Run("sorting-page-offset", func(t *testing.T) {
		order := "quota_value desc, quota_code asc"
		req := &paginationV1.PagingRequest{OrderBy: &order, Page: ptr(uint32(2)), PageSize: ptr(uint32(1))}
		got, e := r.List(ctx, req)
		require.NoError(t, e)
		require.Equal(t, uint64(4), got.Total)
		require.Equal(t, "gpu.shared_memory_mib", got.Items[0].GetQuotaCode())
		req = &paginationV1.PagingRequest{OrderBy: ptr(`["-quota_value","quota_code"]`), Offset: ptr(uint64(2)), Limit: ptr(uint32(1))}
		got, e = r.List(ctx, req)
		require.NoError(t, e)
		require.Equal(t, "storage.bytes", got.Items[0].GetQuotaCode())
		req.NoPaging = ptr(true)
		got, e = r.List(ctx, req)
		require.NoError(t, e)
		require.Len(t, got.Items, 4)
		req.Sorting = []*paginationV1.Sorting{{Field: "id", Direction: paginationV1.Sorting_ASC}}
		got, e = r.List(ctx, req)
		require.NoError(t, e)
		require.Equal(t, "user.count", got.Items[0].GetQuotaCode())
	})
	t.Run("token-mask", func(t *testing.T) {
		previous := pagination.TokenSecret()
		pagination.SetTokenSecret([]byte("plan-quota-paging-compatibility-test-secret"))
		t.Cleanup(func() { pagination.SetTokenSecret(previous) })
		token := pagination.EncodeAndSign(int64(all.Items[1].GetId()), pagination.TokenSecret())
		got, e := r.List(ctx, &paginationV1.PagingRequest{Token: &token, Offset: ptr(uint64(1))})
		require.NoError(t, e)
		require.Equal(t, uint64(4), got.Total)
		require.Len(t, got.Items, 1)
		require.Equal(t, "gpu.count", got.Items[0].GetQuotaCode())
		_, e = r.List(ctx, &paginationV1.PagingRequest{Token: ptr(token + "corrupt"), Offset: ptr(uint64(1))})
		require.Error(t, e)
		got, e = r.List(ctx, &paginationV1.PagingRequest{FieldMask: &fieldmaskpb.FieldMask{Paths: []string{"quotaCode", "quota_value"}}})
		require.NoError(t, e)
		require.Nil(t, got.Items[0].Id)
		require.Nil(t, got.Items[0].CreatedAt)
		require.Equal(t, "user.count", got.Items[0].GetQuotaCode())
		detail, e := r.Get(ctx, &identityV1.GetPlanQuotaRequest{QueryBy: &identityV1.GetPlanQuotaRequest_Id{Id: all.Items[0].GetId()}, ViewMask: &fieldmaskpb.FieldMask{Paths: []string{"created_at"}}})
		require.NoError(t, e)
		require.Nil(t, detail.QuotaCode)
		require.NotNil(t, detail.CreatedAt)
	})
	t.Run("invalid-field-does-not-expand", func(t *testing.T) {
		_, e := r.List(ctx, query(fmt.Sprintf(`{"tenant_id":%d}`, p.ID)))
		require.Error(t, e)
	})
}

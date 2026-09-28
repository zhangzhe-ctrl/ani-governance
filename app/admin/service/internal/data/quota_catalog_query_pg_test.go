//go:build quota_pg

package data

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/stretchr/testify/require"

	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/tests/testutil"
	pagination "go-wind-admin/pkg/localdeps/go-crud/api/gen/go/pagination/v1"
	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"
)

func TestQuotaCatalogQueriesPageAndExistInDatabase(t *testing.T) {
	base := testutil.NewQuotaPGClient(t)
	testutil.ResetQuotaFixture(t, base)
	ctx := testutil.NewSystemViewerCtx(context.Background())
	plan, err := base.Client().Plan.Create().SetName("quota-catalog-sql").Save(ctx)
	require.NoError(t, err)
	require.NoError(t, base.Client().PlanQuota.Create().SetPlanID(plan.ID).SetQuotaCode(QuotaCodeGpuCount).SetQuotaValue(3).Exec(ctx))

	var queries []string
	observed := dialect.Debug(base.Driver(), func(parts ...any) {
		queries = append(queries, fmt.Sprint(parts...))
	})
	client := ent.NewClient(ent.Driver(observed))
	repo := &QuotaAdminRepo{entClient: entCrud.NewEntClient(client, base.Driver())}
	page, err := repo.ListDefinitions(ctx, &pagination.PagingRequest{Page: ptr(uint32(1)), PageSize: ptr(uint32(1))})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.GreaterOrEqual(t, page.Total, uint64(3))
	catalogSQL := strings.ToUpper(strings.Join(queries, "\n"))
	require.Contains(t, catalogSQL, "SYS_QUOTA_DEFINITIONS")
	require.Contains(t, catalogSQL, "COUNT(")
	require.Contains(t, catalogSQL, "LIMIT", "bounded page must be applied by PostgreSQL")

	queries = nil
	exists, err := repo.HasPlanQuotaPolicy(ctx, plan.ID, QuotaCodeGpuCount)
	require.NoError(t, err)
	require.True(t, exists)
	policySQL := strings.ToUpper(strings.Join(queries, "\n"))
	require.Contains(t, policySQL, "SYS_PLAN_QUOTAS")
	require.Contains(t, policySQL, "EXISTS", "policy existence must be checked by PostgreSQL")
}

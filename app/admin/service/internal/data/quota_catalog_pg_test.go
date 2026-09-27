//go:build quota_pg

package data

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	paginationV1 "go-wind-admin/pkg/localdeps/go-crud/api/gen/go/pagination/v1"
)

func TestQuotaGpuCatalogPagingLimits(t *testing.T) {
	c := newLedgerPGClient(t)
	r := &QuotaAdminRepo{entClient: c}
	ctx := context.Background()
	all, e := r.ListDefinitions(ctx, &paginationV1.PagingRequest{NoPaging: ptr(true)})
	require.NoError(t, e)
	require.GreaterOrEqual(t, len(all.Items), 3)
	page, e := r.ListDefinitions(ctx, &paginationV1.PagingRequest{Page: ptr(uint32(math.MaxUint32)), PageSize: ptr(uint32(math.MaxUint32))})
	require.NoError(t, e)
	require.Empty(t, page.Items)
	require.Equal(t, all.Total, page.Total)
	page, e = r.ListDefinitions(ctx, &paginationV1.PagingRequest{Offset: ptr(uint64(1)), Limit: ptr(uint32(1))})
	require.NoError(t, e)
	require.Len(t, page.Items, 1)
	require.Equal(t, all.Items[1], page.Items[0])
	require.Equal(t, all.Total, page.Total)
	page, e = r.ListDefinitions(ctx, &paginationV1.PagingRequest{NoPaging: ptr(true), Page: ptr(uint32(math.MaxUint32)), PageSize: ptr(uint32(1))})
	require.NoError(t, e)
	require.Equal(t, all, page)
}

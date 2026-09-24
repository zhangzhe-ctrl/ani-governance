package service

import (
	"context"
	"strconv"

	acc "github.com/zhangzhe-ctrl/ani-accelerator-service/api/gen/go/accelerator/v1"
	catalogv1 "go-wind-admin/api/gen/go/catalog/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
)

// GpuBffLedgerBridge projects only the caller's original ledger and current
// quota account. It does not create an operation, hold capacity or spend quota.
type GpuBffLedgerBridge struct {
	ledger   *data.QuotaLedgerRepo
	accounts *data.QuotaAdminRepo
}

func NewGpuBffLedgerBridge(ledger *data.QuotaLedgerRepo, accounts *data.QuotaAdminRepo) *GpuBffLedgerBridge {
	return &GpuBffLedgerBridge{ledger: ledger, accounts: accounts}
}

func (s *GpuBffLedgerBridge) ResolveGpuUsageRef(ctx context.Context, tenant uint32, owner, resource string) (*acc.GpuUsageRef, error) {
	return resolveGpuUsageRef(ctx, s.ledger, tenant, owner, resource)
}

func (s *GpuBffLedgerBridge) GPUPreviewQuota(ctx context.Context, tenant uint32, items []*catalogv1.GpuPreviewQuotaItem) ([]*catalogv1.GpuPreviewQuotaCheck, error) {
	if tenant == 0 {
		return nil, data.QuotaErrNotFound("tenant not found")
	}
	accounts, err := s.accounts.ListTenantAccounts(ctx, tenant)
	if err != nil {
		return nil, err
	}
	available := map[string]int64{}
	for _, a := range accounts.Items {
		v, e := strconv.ParseInt(a.Available, 10, 64)
		if e != nil || v < 0 {
			return nil, data.QuotaErrInternal("invalid quota account amount")
		}
		available[a.QuotaCode] = v
	}
	out := make([]*catalogv1.GpuPreviewQuotaCheck, 0, len(items))
	for _, q := range items {
		if q == nil || q.Units <= 0 || (q.QuotaCode != GpuPhysicalQuotaCode && q.QuotaCode != GpuSharedQuotaCode) {
			return nil, data.QuotaErrInvalid("invalid GPU quota preview")
		}
		v, found := available[q.QuotaCode]
		state := "INSUFFICIENT"
		if !found {
			state = "NOT_CONFIGURED"
		} else if v >= q.Units {
			state = "SUFFICIENT"
		}
		out = append(out, &catalogv1.GpuPreviewQuotaCheck{QuotaCode: q.QuotaCode, Requested: q.Units, Available: v, Sufficient: found && v >= q.Units, Status: state})
	}
	return out, nil
}

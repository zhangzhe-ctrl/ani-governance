package data

import (
	"context"
	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
)

// ResourceTenantID resolves only a verified login's local tenant identifier.
func (r *TenantRepo) ResourceTenantID(ctx context.Context, id uint32) (string, error) {
	if id == 0 {
		return "", errors.Forbidden("TENANT_REQUIRED", "platform identity cannot query tenant models")
	}
	row, err := r.entClient.Client().Tenant.Get(ctx, id)
	if err != nil {
		log.Errorf("resolve resource UUID for tenant %d: %v", id, err)
		return "", errors.ServiceUnavailable("TENANT_MAPPING_UNAVAILABLE", "tenant mapping unavailable")
	}
	u, err := uuid.Parse(row.ResourceTenantID)
	if err != nil || u == uuid.Nil || u.String() != row.ResourceTenantID {
		log.Errorf("tenant %d has invalid resource UUID: %v", id, err)
		return "", errors.ServiceUnavailable("TENANT_MAPPING_INVALID", "tenant mapping invalid")
	}
	return row.ResourceTenantID, nil
}

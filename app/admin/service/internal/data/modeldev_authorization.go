package data

import (
	"context"
	"errors"

	"go-wind-admin/app/admin/service/internal/data/ent"
	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"
)

var (
	ErrModelDevAuthorizationDenied = errors.New("modeldev create authorization denied")
	ErrModelDevAuthorizationUnavailable = errors.New("modeldev create authorization unavailable")
)

// ModelDevAuthorizationRepo checks the current database grant for the fixed
// CPU-P01 create action. JWT authentication, TenantAccess and Casbin remain
// required before this check; it does not authenticate a user or enable assets.
type ModelDevAuthorizationRepo struct {
	entClient *entCrud.EntClient[*ent.Client]
}

func NewModelDevAuthorizationRepo(client *entCrud.EntClient[*ent.Client]) *ModelDevAuthorizationRepo {
	return &ModelDevAuthorizationRepo{entClient: client}
}

// AuthorizeCreate requires trusted tenant/user IDs from the verified Principal.
// It must run before FindAccepted for both new requests and original-key replay.
func (r *ModelDevAuthorizationRepo) AuthorizeCreate(ctx context.Context, tenantID, userID uint32) error {
	// RED stub: current user, active role membership, exact MODEL/create grant
	// and ALL data scope have not been read from PostgreSQL yet.
	return ErrModelDevAuthorizationUnavailable
}

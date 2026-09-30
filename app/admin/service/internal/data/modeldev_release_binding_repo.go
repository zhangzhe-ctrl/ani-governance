package data

import (
	"context"
	"errors"
	"time"

	"go-wind-admin/app/admin/service/internal/data/ent"
	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"
)

var (
	ErrModelDevBindingGenerationConflict = errors.New("modeldev release binding generation conflict")
	ErrModelDevBindingNotFound           = errors.New("modeldev release binding not found")
)

// ModelDevReleaseBindingScope identifies one tenant's managed preset binding.
// It is derived from trusted context and the persisted tenant mapping, not a
// user's choice of actor, namespace, backend endpoint, or global setting.
type ModelDevReleaseBindingScope struct {
	TenantID         uint32
	ResourceTenantID string
	PresetID         string
}

// ModelDevReleaseBindingTarget is an already resolved immutable catalogue
// reference. This repository does not establish that a release is VERIFIED;
// the managed use case must check catalogue evidence and current authorization.
type ModelDevReleaseBindingTarget struct {
	ReleaseID             string
	ReleaseDigest         string
	NewSubmissionsEnabled bool
}

type ModelDevReleaseBinding struct {
	Scope             ModelDevReleaseBindingScope
	Target            ModelDevReleaseBindingTarget
	Generation        uint64
	UpdatedBy         string
	UpdatedAt         time.Time
	Reason            string
	EvidenceReference string
}

type ModelDevReleaseBindingUpdate struct {
	ExpectedGeneration uint64
	Target             ModelDevReleaseBindingTarget
	Actor              string
	RequestedAt        time.Time
	Reason             string
	EvidenceReference  string
}

type ModelDevReleaseBindingChange struct {
	Before   *ModelDevReleaseBinding
	After    *ModelDevReleaseBinding
	Replayed bool
}

// ModelDevReleaseBindingRepo owns only Governance's current pointer and gate.
// Immutable release content stays in ModelDev. Network resolution is never
// performed inside a binding or acceptance transaction.
type ModelDevReleaseBindingRepo struct {
	entClient *entCrud.EntClient[*ent.Client]
}

func NewModelDevReleaseBindingRepo(client *entCrud.EntClient[*ent.Client]) *ModelDevReleaseBindingRepo {
	return &ModelDevReleaseBindingRepo{entClient: client}
}

func (r *ModelDevReleaseBindingRepo) Get(context.Context, ModelDevReleaseBindingScope) (*ModelDevReleaseBinding, error) {
	return nil, errors.New("modeldev release binding persistence not implemented")
}

// CompareAndSwap creates generation 1 only from expected generation 0. A new
// target requires the observed generation; the same target replays the stored
// generation and audit values. Replays do not replace current authorization.
func (r *ModelDevReleaseBindingRepo) CompareAndSwap(context.Context, ModelDevReleaseBindingScope, ModelDevReleaseBindingUpdate) (*ModelDevReleaseBindingChange, error) {
	return nil, errors.New("modeldev release binding persistence not implemented")
}

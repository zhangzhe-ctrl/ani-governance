package data

import (
	"context"
	"time"

	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ModelDevClientConfig is explicit internal connection material. The server
// identity is fixed to ani-modeldev-service, never supplied by a public request.
type ModelDevClientConfig struct {
	Address, CAFile, CertFile, KeyFile string
	Timeout time.Duration
}

// ModelDevResolveScope is supplied after current Governance authorization and
// its persisted tenant mapping. It is not a public request DTO.
type ModelDevResolveScope struct {
	ResourceTenantID string
	Actor string
}

// ModelDevReleaseSelection is one current binding observation. ModelDev must
// not choose another Release or allocate Governance's binding generation.
type ModelDevReleaseSelection struct {
	ReleaseID string
	ReleaseDigest string
	BindingGeneration uint64
}

// ModelDevResolution is an unpersisted candidate, not a command receipt.
type ModelDevResolution struct {
	Snapshot cpup01.Snapshot
	ExecutionSpecHash string
}

type ModelDevClient struct{}

func NewModelDevClient(ModelDevClientConfig) (*ModelDevClient, func(), error) {
	return &ModelDevClient{}, func() {}, nil
}

func (c *ModelDevClient) Resolve(context.Context, ModelDevResolveScope, cpup01.Intent, ModelDevReleaseSelection, time.Time) (ModelDevResolution, error) {
	return ModelDevResolution{}, status.Error(codes.Unimplemented, "modeldev resolution client not implemented")
}

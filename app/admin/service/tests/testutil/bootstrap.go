// Package testutil provides resources shared by tests across service packages.
// Production commands must not import this package.
package testutil

import (
	"context"

	conf "go-wind-admin/pkg/localdeps/kratos-bootstrap/api/gen/go/conf/v1"
	"go-wind-admin/pkg/localdeps/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/pkg/localdeps/kratos-bootstrap/logger"
)

// NewBootstrapContext supplies a logger and explicit optional configuration to
// real constructors. Tests that depend on authentication or encryption must
// pass the configuration they exercise.
func NewBootstrapContext(cfg *conf.Bootstrap) *bootstrap.Context {
	return bootstrap.NewContextWithParam(context.Background(), nil, cfg, bLogger.NopLogger())
}

package middleware

import (
	"context"

	"go-wind-admin/pkg/localdeps/kratos-authz/engine"
)

func NewContext(ctx context.Context, claims *engine.AuthClaims) context.Context {
	return engine.ContextWithAuthClaims(ctx, claims)
}

func FromContext(ctx context.Context) (*engine.AuthClaims, bool) {
	return engine.AuthClaimsFromContext(ctx)
}

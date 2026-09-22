package auth

import (
	"context"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
)

type ctxKey string

var (
	authClaimsContextKey = ctxKey("authn-claims")
)

func NewContext(parent context.Context, claims *authenticationV1.UserTokenPayload) context.Context {
	if claims != nil && claims.GetUserId() > 0 {
		parent = NewPrincipalContext(parent, &Principal{Type: SubjectUser, ID: claims.GetUserId(), TenantID: claims.GetTenantId(), Roles: claims.GetRoles(), Claims: claims})
	}
	return context.WithValue(parent, authClaimsContextKey, claims)
}

func FromContext(ctx context.Context) (*authenticationV1.UserTokenPayload, error) {
	claims, ok := ctx.Value(authClaimsContextKey).(*authenticationV1.UserTokenPayload)
	if !ok {
		return nil, ErrMissingJwtToken
	}
	return claims, nil
}

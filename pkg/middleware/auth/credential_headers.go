package auth

import (
	"context"
	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/transport"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
)

// CredentialHeaders also protects public user-login endpoints outside the JWT selector.
func CredentialHeaders() middleware.Middleware {
	return func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			if tr, ok := transport.FromServerContext(ctx); ok {
				if htr, ok := tr.(*khttp.Transport); ok && hasSigningHeaders(htr.Request()) {
					if len(htr.Request().Header.Values("Authorization")) > 0 {
						return nil, errors.BadRequest("MIXED_AUTHENTICATION", "choose one authentication method")
					}
					if !allowsAPIKey(tr.Operation()) {
						return nil, errors.Forbidden("USER_REQUIRED", "this operation requires a user identity")
					}
				}
			}
			return next(ctx, req)
		}
	}
}

package server

import (
	"context"
	"github.com/go-kratos/kratos/v2/transport/http"
	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
)

// Handwritten transport adapter keeps real HTTP status and generated domain messages aligned.
// No generated handler is patched: all six routes use the same authentication middleware.
func registerAccessKeyHTTP(s *http.Server, service adminV1.AccessKeyServiceHTTPServer) {
	r := s.Route("/")
	r.GET("/api/v1/auth/api-keys", keyHTTPHandler(adminV1.OperationAccessKeyServiceList, 200, false, service.List))
	r.GET("/api/v1/auth/api-keys/{key_id}", keyHTTPHandler(adminV1.OperationAccessKeyServiceGet, 200, false, service.Get))
	r.POST("/api/v1/auth/api-keys", keyHTTPHandler(adminV1.OperationAccessKeyServiceCreate, 201, true, service.Create))
	r.PUT("/api/v1/auth/api-keys/{key_id}", keyHTTPHandler(adminV1.OperationAccessKeyServiceUpdate, 200, true, service.Update))
	r.DELETE("/api/v1/auth/api-keys/{key_id}", keyHTTPHandler(adminV1.OperationAccessKeyServiceDelete, 200, false, service.Delete))
	r.PUT("/api/v1/auth/api-keys/{key_id}/secret", keyHTTPHandler(adminV1.OperationAccessKeyServiceResetSecret, 200, true, service.ResetSecret))
}
func keyHTTPHandler[Req any, Reply any](operation string, status int, body bool, call func(context.Context, *Req) (*Reply, error)) func(http.Context) error {
	return func(ctx http.Context) error {
		in := new(Req)
		if body {
			if err := ctx.Bind(in); err != nil {
				return err
			}
		}
		if err := ctx.BindQuery(in); err != nil {
			return err
		}
		if err := ctx.BindVars(in); err != nil {
			return err
		}
		http.SetOperation(ctx, operation)
		handler := ctx.Middleware(func(ctx context.Context, req interface{}) (interface{}, error) { return call(ctx, req.(*Req)) })
		out, err := handler(ctx, in)
		if err != nil {
			return err
		}
		return ctx.Result(status, out.(*Reply))
	}
}

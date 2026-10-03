package server

import (
	"context"
	"io"
	"net/http"

	"github.com/go-kratos/kratos/v2/errors"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	adminv1 "go-wind-admin/api/gen/go/admin/service/v1"
	modeldevv1 "go-wind-admin/api/gen/go/modeldev/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/service"
)

func registerModelDevStopHTTP(server *khttp.Server, modeldev *service.ModelDevService) {
	server.Route("/").POST(data.ModelDevStopExecutionPath, func(ctx khttp.Context) error {
		modelDevQueryHeaders(ctx)
		khttp.SetOperation(ctx, adminv1.OperationModelDevServiceStopExecution)
		var in modeldevv1.StopExecutionRequest
		body, err := io.ReadAll(io.LimitReader(ctx.Request().Body, 1))
		if err != nil || len(body) != 0 || ctx.BindVars(&in) != nil || ctx.Request().URL.RawQuery != "" || ctx.Request().URL.ForceQuery {
			return errors.BadRequest("INVALID_MODELDEV_STOP", "invalid modeldev stop request")
		}
		handler := ctx.Middleware(func(c context.Context, v interface{}) (interface{}, error) {
			return modeldev.StopExecution(c, v.(*modeldevv1.StopExecutionRequest))
		})
		out, err := handler(ctx, &in)
		if err != nil {
			return err
		}
		return ctx.Result(http.StatusAccepted, out)
	})
}

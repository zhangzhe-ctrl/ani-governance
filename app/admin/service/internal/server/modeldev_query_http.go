package server

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-kratos/kratos/v2/errors"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	adminv1 "go-wind-admin/api/gen/go/admin/service/v1"
	modeldevv1 "go-wind-admin/api/gen/go/modeldev/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/service"
)

func registerModelDevQueryHTTP(server *khttp.Server, modeldev *service.ModelDevService) {
	route := server.Route("/")
	route.GET(data.ModelDevGetExecutionPath, func(ctx khttp.Context) error {
		modelDevQueryHeaders(ctx)
		khttp.SetOperation(ctx, adminv1.OperationModelDevServiceGetExecution)
		var in modeldevv1.GetExecutionRequest
		if ctx.BindVars(&in) != nil || ctx.Request().URL.RawQuery != "" || ctx.Request().URL.ForceQuery {
			return invalidModelDevQueryHTTP()
		}
		handler := ctx.Middleware(func(c context.Context, v interface{}) (interface{}, error) {
			return modeldev.GetExecution(c, v.(*modeldevv1.GetExecutionRequest))
		})
		out, err := handler(ctx, &in)
		if err != nil {
			return err
		}
		return ctx.Result(http.StatusOK, out)
	})
	route.GET(data.ModelDevListArtifactsPath, func(ctx khttp.Context) error {
		modelDevQueryHeaders(ctx)
		khttp.SetOperation(ctx, adminv1.OperationModelDevServiceListExecutionArtifacts)
		var in modeldevv1.ListExecutionArtifactsRequest
		if ctx.BindVars(&in) != nil {
			return invalidModelDevQueryHTTP()
		}
		query, err := url.ParseQuery(ctx.Request().URL.RawQuery)
		if err != nil {
			return invalidModelDevQueryHTTP()
		}
		for name, values := range query {
			if len(values) != 1 {
				return invalidModelDevQueryHTTP()
			}
			switch name {
			case "page_size":
				size, e := strconv.ParseUint(values[0], 10, 32)
				if e != nil || size > 100 || values[0] != strconv.FormatUint(size, 10) {
					return invalidModelDevQueryHTTP()
				}
				in.PageSize = uint32(size)
			case "page_token":
				if len(values[0]) > 2048 {
					return invalidModelDevQueryHTTP()
				}
				in.PageToken = values[0]
			default:
				return invalidModelDevQueryHTTP()
			}
		}
		handler := ctx.Middleware(func(c context.Context, v interface{}) (interface{}, error) {
			return modeldev.ListExecutionArtifacts(c, v.(*modeldevv1.ListExecutionArtifactsRequest))
		})
		out, err := handler(ctx, &in)
		if err != nil {
			return err
		}
		return ctx.Result(http.StatusOK, out)
	})
	route.GET(data.ModelDevDownloadArtifactPath, func(ctx khttp.Context) error {
		modelDevQueryHeaders(ctx)
		khttp.SetOperation(ctx, adminv1.OperationModelDevServiceAuthorizeArtifactDownload)
		var in modeldevv1.AuthorizeArtifactDownloadRequest
		if ctx.BindVars(&in) != nil || ctx.Request().URL.RawQuery != "" || ctx.Request().URL.ForceQuery {
			return invalidModelDevQueryHTTP()
		}
		handler := ctx.Middleware(func(c context.Context, v interface{}) (interface{}, error) {
			return modeldev.AuthorizeArtifactDownload(c, v.(*modeldevv1.AuthorizeArtifactDownloadRequest))
		})
		out, err := handler(ctx, &in)
		if err != nil {
			return err
		}
		return ctx.Result(http.StatusOK, out)
	})
}

func modelDevQueryHeaders(ctx khttp.Context) {
	ctx.Response().Header().Set("Cache-Control", "no-store")
	ctx.Response().Header().Set("Pragma", "no-cache")
	ctx.Response().Header().Set("Referrer-Policy", "no-referrer")
	// Delegation is minted after current database authorization, never accepted
	// from public caller headers, including a valid authenticated caller.
	for _, name := range []string{"x-ani-authorized-method", "x-ani-data-scope", "x-ani-tenant-id", "x-ani-actor", "x-ani-request-id"} {
		ctx.Request().Header.Del(name)
	}
}
func invalidModelDevQueryHTTP() error {
	return errors.BadRequest("INVALID_MODELDEV_QUERY", "invalid modeldev query")
}

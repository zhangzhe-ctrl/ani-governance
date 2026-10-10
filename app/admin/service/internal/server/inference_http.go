package server

import (
	"context"
	"io"
	"mime"
	nethttp "net/http"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/transport/http"
	admin "go-wind-admin/api/gen/go/admin/service/v1"
	view "go-wind-admin/api/gen/go/inference/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/service"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Strict decoding is required at the public boundary: internal attachment,
// tenant, plan and charge fields must be rejected rather than discarded by a
// permissive generated HTTP decoder. The normal authentication, tenant gate,
// Casbin and audit middleware still execute through ctx.Middleware.
func RegisterInferenceHTTPServer(server *http.Server, inference *service.InferenceService) {
	server.Route("/").POST(data.InferenceCreatePath, func(ctx http.Context) error {
		http.SetOperation(ctx, admin.OperationInferenceServiceCreateInference)
		request := new(view.CreateInferenceRequest)
		if err := decodeInferenceHTTP(ctx.Request(), request); err != nil {
			return err
		}
		handler := ctx.Middleware(func(call context.Context, in interface{}) (interface{}, error) {
			return inference.CreateInference(call, in.(*view.CreateInferenceRequest))
		})
		out, err := handler(ctx, request)
		if err != nil {
			return err
		}
		return ctx.Result(nethttp.StatusAccepted, out)
	})
	server.Route("/").POST(data.InferenceDeletePath, func(ctx http.Context) error {
		http.SetOperation(ctx, admin.OperationInferenceServiceDeleteInference)
		request := new(view.DeleteInferenceRequest)
		if err := decodeInferenceHTTP(ctx.Request(), request); err != nil {
			return err
		}
		path := new(view.DeleteInferenceRequest)
		if ctx.BindVars(path) != nil || path.Data == nil || request.Data == nil || (request.Data.ResourceId != "" && request.Data.ResourceId != path.Data.ResourceId) {
			return errors.BadRequest("INVALID_INFERENCE_REQUEST", "invalid inference resource")
		}
		request.Data.ResourceId = path.Data.ResourceId
		handler := ctx.Middleware(func(call context.Context, in interface{}) (interface{}, error) {
			return inference.DeleteInference(call, in.(*view.DeleteInferenceRequest))
		})
		out, err := handler(ctx, request)
		if err != nil {
			return err
		}
		return ctx.Result(nethttp.StatusAccepted, out)
	})
}

func decodeInferenceHTTP(request *nethttp.Request, message proto.Message) error {
	invalid := errors.BadRequest("INVALID_INFERENCE_REQUEST", "invalid inference request")
	if request == nil || request.Body == nil || request.URL.RawQuery != "" || request.URL.ForceQuery || request.URL.RawPath != "" {
		return invalid
	}
	media, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return invalid
	}
	for _, header := range []string{"X-Ani-Tenant-Id", "X-Tenant-Id", "X-Ani-Actor", "X-Ani-Operator", "X-Resource-Tenant-Id"} {
		if len(request.Header.Values(header)) != 0 {
			return invalid
		}
	}
	raw, err := io.ReadAll(io.LimitReader(request.Body, 65537))
	if err != nil || len(raw) == 0 || len(raw) > 65536 {
		return invalid
	}
	if err = (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(raw, message); err != nil {
		return invalid
	}
	return nil
}

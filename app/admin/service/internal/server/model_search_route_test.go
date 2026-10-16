package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
	adminpb "go-wind-admin/api/gen/go/admin/service/v1"
	catalogv1 "go-wind-admin/api/gen/go/catalog/service/v1"
)

type modelSearchRouteProbe struct {
	adminpb.ModelServiceHTTPServer
	request *catalogv1.SearchExternalModelsRequest
}

func (modelSearchRouteProbe) GetModel(context.Context, *catalogv1.GetModelRequest) (*catalogv1.GetModelResponse, error) {
	return nil, kratoserrors.NotFound("MODEL_ROUTE", "wrong route")
}

func (p *modelSearchRouteProbe) SearchExternalModels(_ context.Context, request *catalogv1.SearchExternalModelsRequest) (*catalogv1.SearchExternalModelsResponse, error) {
	p.request = request
	return &catalogv1.SearchExternalModelsResponse{Items: []*catalogv1.ExternalModelCandidate{}}, nil
}

func TestModelSearchRouteDoesNotMatchModelID(t *testing.T) {
	srv := kratoshttp.NewServer()
	probe := &modelSearchRouteProbe{}
	adminpb.RegisterModelServiceHTTPServer(srv, probe)
	host := httptest.NewServer(srv)
	defer host.Close()

	resp, err := http.Get(host.URL + "/api/v1/models/search?source=modelscope&keyword=qwen&page=2&pageSize=10")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search route status=%d, want %d", resp.StatusCode, http.StatusOK)
	}
	if probe.request == nil || probe.request.GetPage() != 2 || probe.request.GetPageSize() != 10 {
		t.Fatalf("search pagination=%#v, want page=2 pageSize=10", probe.request)
	}

	legacy, err := http.Get(host.URL + "/api/v1/models:search?source=modelscope&keyword=qwen")
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Body.Close()
	if legacy.StatusCode == http.StatusOK {
		t.Fatal("legacy colon search route must not remain registered")
	}
}

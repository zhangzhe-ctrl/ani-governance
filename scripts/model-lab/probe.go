//go:build ignore

// Task-private HTTP password encoder and real mTLS gRPC probe. Run on Fedora.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	modelv1 "github.com/zhangzhe-ctrl/ani-model-service/api/model/v1"
	utilcrypto "go-wind-admin/pkg/localdeps/go-utils/crypto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"io"
	"os"
	"time"
)

func main() {
	encrypt := flag.Bool("encrypt", false, "encode login password from stdin")
	addr := flag.String("address", "127.0.0.1:17990", "gRPC address")
	ca := flag.String("ca", "", "CA file")
	cert := flag.String("cert", "", "client cert")
	key := flag.String("key", "", "client key")
	tenant := flag.String("tenant", "", "trusted tenant")
	rpcTenant := flag.String("rpc-tenant", "", "request tenant")
	actor := flag.String("actor", "governance:user:7", "actor")
	rpc := flag.String("rpc", "ListModels", "RPC method")
	limit := flag.Int("limit", 100, "RPC limit")
	state := flag.String("status", "", "model status")
	filter := flag.String("filter", "", "unsupported filter")
	duplicate := flag.Bool("duplicate", false, "duplicate tenant metadata")
	flag.Parse()
	if *encrypt {
		plain, err := io.ReadAll(os.Stdin)
		if err != nil {
			panic(err)
		}
		value, err := utilcrypto.AesEncrypt(plain, utilcrypto.DefaultAESKey, nil)
		if err != nil {
			panic(err)
		}
		fmt.Println(base64.StdEncoding.EncodeToString(value))
		return
	}
	pem, err := os.ReadFile(*ca)
	if err != nil {
		panic(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		panic("empty CA")
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: "ani-model-service"}
	if *cert != "" {
		c, err := tls.LoadX509KeyPair(*cert, *key)
		if err != nil {
			panic(err)
		}
		tc.Certificates = []tls.Certificate{c}
	}
	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(credentials.NewTLS(tc)))
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	md := metadata.Pairs("x-ani-tenant-id", *tenant, "x-ani-actor", *actor, "x-ani-request-id", "99999999-9999-4999-8999-999999999999")
	if *duplicate {
		md.Append("x-ani-tenant-id", *tenant)
	}
	ctx, cancel := context.WithTimeout(metadata.NewOutgoingContext(context.Background(), md), 3*time.Second)
	defer cancel()
	tid := *rpcTenant
	if tid == "" {
		tid = *tenant
	}
	req := &modelv1.ListModelsRequest{TenantId: tid, Status: *state, Page: &modelv1.CursorPageRequest{Limit: int32(*limit)}}
	switch *filter {
	case "cursor":
		req.Page.Cursor = "unsupported"
	case "keyword":
		req.Keyword = "unsupported"
	case "source":
		req.Source = "upload"
	case "capability":
		req.Capability = "chat"
	}
	out := new(modelv1.ListModelsResponse)
	var input any = req
	if *rpc == "DeleteModel" {
		input = &modelv1.DeleteModelRequest{TenantId: tid, ModelId: "99999999-9999-4999-8999-999999999999"}
	}
	err = conn.Invoke(ctx, "/model.v1.ModelService/"+*rpc, input, out)
	result := map[string]any{"code": status.Code(err).String(), "models": len(out.Models)}
	if err != nil {
		result["error"] = status.Convert(err).Message()
	}
	if err = json.NewEncoder(os.Stdout).Encode(result); err != nil {
		panic(err)
	}
}

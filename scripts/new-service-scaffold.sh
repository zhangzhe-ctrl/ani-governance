#!/usr/bin/env bash
# new-service-scaffold.sh — 业务服务接入骨架生成
#
# 用法（仓库根目录执行——本脚本只写文件，不做生成/构建）：
#   scripts/new-service-scaffold.sh \
#     -n Model -d catalog -m MODEL \
#     -r github.com/zhangzhe-ctrl/ani-model-service/api/model/v1 \
#     -p /api/v1/models -s ani-model-service
#
#   -n  服务名，PascalCase（如 Model、Network）
#   -d  领域包段：proto package 与 api/gen/go 子目录（如 catalog）
#   -m  identityV1.Module 枚举后缀（如 MODEL）
#   -r  下游 Go API import 路径（如 github.com/zhangzhe-ctrl/ani-model-service/api/model/v1）
#   -p  HTTP 路由（如 /api/v1/models）
#   -s  下游 mTLS ServerName（下游证书 DNS SAN，如 ani-model-service）
#
# 生成四个骨架文件（已存在则拒绝覆盖）：
#   api/protos/<d>/service/v1/<entity>.proto        领域消息（无 HTTP 注解）
#   api/protos/admin/service/v1/i_<entity>.proto    治理 BFF 代理（含路由）
#   app/admin/service/internal/data/<entity>_client.go    出站 mTLS 客户端
#   app/admin/service/internal/service/<entity>_service.go 入站代理服务
#
# 生成后必须手工完成：wire 装配四处锚点、module_mapping 登记、Api 表登记。
# 流程与不变量全文见 docs/service-integration.md。
# `gow api`、`make build_only`、运行验收按仓库执行环境约定运行（见 AGENTS.md）。

set -euo pipefail

NAME=""; DOMAIN=""; MODULE=""; DOWNSTREAM=""; ROUTE=""; SERVERNAME=""
while getopts "n:d:m:r:p:s:" opt; do
  case "$opt" in
    n) NAME="$OPTARG" ;;
    d) DOMAIN="$OPTARG" ;;
    m) MODULE="$OPTARG" ;;
    r) DOWNSTREAM="$OPTARG" ;;
    p) ROUTE="$OPTARG" ;;
    s) SERVERNAME="$OPTARG" ;;
    *) echo "usage: $0 -n Name -d domain -m MODULE -r downstream/import -p /route -s server-name" >&2; exit 2 ;;
  esac
done

for v in NAME DOMAIN MODULE DOWNSTREAM ROUTE SERVERNAME; do
  [ -n "${!v}" ] || { echo "error: missing -$v" >&2; exit 2; }
done

# PascalCase 校验（避免把 kebab/snake 混进 Go 标识符）
if ! [[ "$NAME" =~ ^[A-Z][A-Za-z0-9]*$ ]]; then
  echo "error: -n must be PascalCase (got '$NAME')" >&2; exit 2
fi
if ! [[ "$MODULE" =~ ^[A-Z][A-Z0-9_]*$ ]]; then
  echo "error: -m must be UPPER_SNAKE (got '$MODULE')" >&2; exit 2
fi
if ! [[ "$ROUTE" =~ ^/[a-zA-Z0-9/._-]*$ ]]; then
  echo "error: -p must look like /api/v1/things (got '$ROUTE')" >&2; exit 2
fi

entity="$(echo "${NAME:0:1}" | tr 'A-Z' 'a-z')${NAME:1}"      # model
ENTITY_UPPER="$(echo "$entity" | tr 'a-z' 'A-Z')"              # MODEL
DOMAINV1="${DOMAIN}v1"                                         # catalogv1
# 下游包别名：import 路径倒数第二段 + v1（github.com/org/ani-model-service/api/model/v1 → modelv1）
ALIAS="$(basename "$(dirname "$DOWNSTREAM")")v1"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
P_DOMAIN="$ROOT/api/protos/$DOMAIN/service/v1/$entity.proto"
P_BFF="$ROOT/api/protos/admin/service/v1/i_$entity.proto"
P_CLIENT="$ROOT/app/admin/service/internal/data/${entity}_client.go"
P_SERVICE="$ROOT/app/admin/service/internal/service/${entity}_service.go"

for f in "$P_DOMAIN" "$P_BFF" "$P_CLIENT" "$P_SERVICE"; do
  if [ -e "$f" ]; then
    echo "error: refuse to overwrite existing file: $f" >&2; exit 1
  fi
done

mkdir -p "$(dirname "$P_DOMAIN")" "$(dirname "$P_BFF")" \
         "$(dirname "$P_CLIENT")" "$(dirname "$P_SERVICE")"

# ── 1. 领域 proto（无 HTTP 注解） ─────────────────────────────
cat > "$P_DOMAIN" <<EOF
syntax = "proto3";
package ${DOMAIN}.service.v1;
import "gnostic/openapi/v3/annotations.proto";

// TODO: 按下游真实合同收窄。治理侧只声明需要转发的消息与边界，
// 参照 api/protos/catalog/service/v1/model.proto 的有界列表先例。
message List${NAME}sRequest {
  optional uint32 limit = 1 [json_name = "limit", (gnostic.openapi.v3.property) = {minimum: 1 maximum: 100 default: {number: 100}}];
}
message ${NAME} {
  string id = 1 [json_name = "id"];
}
message List${NAME}sResponse { repeated ${NAME} ${entity}s = 1 [json_name = "${entity}s"]; }
EOF

# ── 2. 治理 BFF 代理 proto（路由只在这里） ─────────────────────
cat > "$P_BFF" <<EOF
syntax = "proto3";
package admin.service.v1;
import "google/api/annotations.proto";
import "${DOMAIN}/service/v1/${entity}.proto";
import "gnostic/openapi/v3/annotations.proto";

// ${NAME} queries through Governance authentication and authorization.
service ${NAME}Service {
  rpc List${NAME}s(${DOMAIN}.service.v1.List${NAME}sRequest) returns (${DOMAIN}.service.v1.List${NAME}sResponse) {
    option (google.api.http) = { get: "${ROUTE}" };
    option (gnostic.openapi.v3.operation) = {
      summary: "List the authenticated tenant's ${entity} catalog"
      // TODO: 写明鉴权、订阅（Module_${MODULE}）、拒绝语义与错误码；
      // 模板见 api/protos/admin/service/v1/i_model.proto。
      responses: { response_or_reference: [
        { name: "400" value: { response: { description: "Invalid or unsupported query parameter" } } },
        { name: "401" value: { response: { description: "Missing, invalid or revoked login" } } },
        { name: "403" value: { response: { description: "Permission, tenant or subscription denied" } } },
        { name: "503" value: { response: { description: "${NAME} transport unavailable" } } },
        { name: "504" value: { response: { description: "Connected ${NAME} call exceeded the deadline" } } }
      ] }
    };
  }
}
EOF

# ── 3. 出站 mTLS 客户端 ───────────────────────────────────────
cat > "$P_CLIENT" <<EOF
package data

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	${ALIAS} "${DOWNSTREAM}"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type ${NAME}ClientConfig struct {
	Address, CAFile, CertFile, KeyFile string
	Timeout                            time.Duration
}
type ${NAME}Client struct {
	client          ${ALIAS}.${NAME}ServiceClient
	timeout         time.Duration
	connectionState func() connectivity.State
}

// New${NAME}Client constructs a fail-closed mTLS client; server identity is fixed.
func New${NAME}Client(c ${NAME}ClientConfig) (*${NAME}Client, func(), error) {
	if c.Address == "" || c.Timeout <= 0 {
		return nil, nil, fmt.Errorf("${entity} address and positive timeout are required")
	}
	pem, err := os.ReadFile(c.CAFile)
	if err != nil {
		return nil, nil, fmt.Errorf("read ${entity} CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, nil, fmt.Errorf("${entity} CA contains no certificates")
	}
	cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("load governance client certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, nil, err
	}
	trustedName := false
	for _, name := range leaf.DNSNames {
		if name == "ani-governance" {
			trustedName = true
		}
	}
	if !trustedName {
		return nil, nil, fmt.Errorf("governance certificate requires exact DNS SAN ani-governance")
	}
	// This fixed internal contract has no DNS TXT service-config authority.
	conn, err := grpc.NewClient(c.Address, grpc.WithDisableServiceConfig(), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{cert}, ServerName: "${SERVERNAME}",
	})))
	if err != nil {
		return nil, nil, err
	}
	return &${NAME}Client{client: ${ALIAS}.New${NAME}ServiceClient(conn), timeout: c.Timeout, connectionState: conn.GetState}, func() { _ = conn.Close() }, nil
}

func ${NAME}ConfigFromEnv() (${NAME}ClientConfig, error) {
	timeout := 3 * time.Second
	if v := os.Getenv("ANI_${ENTITY_UPPER}_TIMEOUT"); v != "" {
		var err error
		timeout, err = time.ParseDuration(v)
		if err != nil {
			return ${NAME}ClientConfig{}, err
		}
	}
	return ${NAME}ClientConfig{Address: os.Getenv("ANI_${ENTITY_UPPER}_ADDR"), CAFile: os.Getenv("ANI_${ENTITY_UPPER}_CA"), CertFile: os.Getenv("ANI_${ENTITY_UPPER}_CERT"), KeyFile: os.Getenv("ANI_${ENTITY_UPPER}_KEY"), Timeout: timeout}, nil
}

// TODO: 按下游真实 RPC 调整方法名与请求/响应字段（先例见 model_client.go.ListModels）。
func (c *${NAME}Client) List${NAME}s(ctx context.Context, tenant string, user uint32, limit uint32) (*${ALIAS}.List${NAME}sResponse, error) {
	id, err := uuid.Parse(tenant)
	if err != nil || id == uuid.Nil || id.String() != tenant || user == 0 {
		return nil, fmt.Errorf("invalid trusted ${entity} identity")
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	// Rebuild metadata; never append inbound/public identity headers.
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("x-ani-tenant-id", tenant, "x-ani-actor", "governance:user:"+strconv.FormatUint(uint64(user), 10), "x-ani-request-id", uuid.NewString()))
	reply, err := c.client.List${NAME}s(ctx, &${ALIAS}.List${NAME}sRequest{TenantId: tenant, Page: &${ALIAS}.CursorPageRequest{Limit: int32(limit)}})
	// A disconnected transport can spend the entire deadline reconnecting.
	// Report that dependency outage as 503; a connected, slow RPC remains 504.
	if status.Code(err) == codes.DeadlineExceeded && c.connectionState != nil && c.connectionState() != connectivity.Ready {
		return nil, status.Errorf(codes.Unavailable, "${entity} transport unavailable: %v", err)
	}
	return reply, err
}
EOF

# ── 4. 入站代理服务 ───────────────────────────────────────────
cat > "$P_SERVICE" <<EOF
package service

import (
	"context"

	"github.com/go-kratos/kratos/v2/errors"
	klog "github.com/go-kratos/kratos/v2/log"
	$(basename "$DOWNSTREAM") "$(basename "$DOWNSTREAM")"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	adminv1 "go-wind-admin/api/gen/go/admin/service/v1"
	${DOMAINV1} "go-wind-admin/api/gen/go/${DOMAIN}/service/v1"
	"go-wind-admin/pkg/middleware/auth"
)

type ${NAME}Lister interface {
	List${NAME}s(context.Context, string, uint32, uint32) (*${ALIAS}.List${NAME}sResponse, error)
}
// ResourceTenantResolver 已在包内定义（model_service.go），复用勿重复声明。
type ${NAME}Service struct {
	adminv1.Unimplemented${NAME}ServiceServer
	client  ${NAME}Lister
	tenants ResourceTenantResolver
}

func New${NAME}Service(client ${NAME}Lister, tenants ResourceTenantResolver) *${NAME}Service {
	return &${NAME}Service{client: client, tenants: tenants}
}

// TODO: 按下游合同实现严格白名单校验——未知/重复/空参数一律拒绝
//（先例见 model_service.go.validateModelQuery）。
func (s *${NAME}Service) List${NAME}s(ctx context.Context, req *${DOMAINV1}.List${NAME}sRequest) (*${DOMAINV1}.List${NAME}sResponse, error) {
	operator, err := auth.FromContext(ctx)
	if err != nil || operator == nil {
		return nil, errors.Unauthorized("INVALID_LOGIN", "login required")
	}
	if operator.GetTenantId() == 0 || operator.GetUserId() == 0 {
		return nil, errors.Forbidden("TENANT_REQUIRED", "tenant user required")
	}
	if req == nil {
		return nil, errors.BadRequest("INVALID_QUERY", "request required")
	}
	limit := uint32(100)
	if req.Limit != nil {
		limit = req.GetLimit()
	}
	if limit < 1 || limit > 100 {
		return nil, errors.BadRequest("INVALID_LIMIT", "limit must be between 1 and 100")
	}
	tenant, err := s.tenants.ResourceTenantID(ctx, operator.GetTenantId())
	if err != nil {
		return nil, err
	}
	reply, err := s.client.List${NAME}s(ctx, tenant, operator.GetUserId(), limit)
	if err != nil {
		return nil, map${NAME}Error(err)
	}
	if reply == nil {
		return nil, errors.ServiceUnavailable("${ENTITY_UPPER}_UNAVAILABLE", "${entity} catalog unavailable")
	}
	out := &${DOMAINV1}.List${NAME}sResponse{${NAME}s: make([]*${DOMAINV1}.${NAME}, 0, len(reply.${NAME}s))}
	for _, m := range reply.${NAME}s {
		if m == nil {
			return nil, errors.ServiceUnavailable("${ENTITY_UPPER}_INVALID_RESPONSE", "invalid ${entity} catalog response")
		}
		// TODO: 逐字段映射领域消息（先例见 model_service.go.ListModels）。
		out.${NAME}s = append(out.${NAME}s, &${DOMAINV1}.${NAME}{Id: m.Id})
	}
	return out, nil
}

func map${NAME}Error(err error) error {
	klog.Errorf("${entity} catalog RPC failed: %v", err)
	switch status.Code(err) {
	case codes.DeadlineExceeded:
		return errors.New(504, "${ENTITY_UPPER}_TIMEOUT", "${entity} catalog timed out")
	case codes.InvalidArgument:
		return errors.BadRequest("INVALID_QUERY", "${entity} catalog rejected query")
	case codes.PermissionDenied:
		return errors.Forbidden("${ENTITY_UPPER}_ACCESS_DENIED", "${entity} access denied")
	default:
		return errors.ServiceUnavailable("${ENTITY_UPPER}_UNAVAILABLE", "${entity} catalog unavailable")
	}
}
EOF

echo "generated:"
echo "  $P_DOMAIN"
echo "  $P_BFF"
echo "  $P_CLIENT"
echo "  $P_SERVICE"
echo
echo "手工装配清单（脚本不代做）："
echo "  1. wiring_ent.go:246 register:service 锚点后：ConfigFromEnv → New${NAME}Client → cleanups → service.New${NAME}Service（失败路径先 rollback()）"
echo "  2. rest_server.go:175 register:param 锚点后：${entity}Service *service.${NAME}Service,"
echo "  3. rest_server.go:254 register:route 锚点后：adminV1.Register${NAME}ServiceHTTPServer(srv, ${entity}Service)"
echo "  4. wiring_ent.go:297 register:rest-arg 锚点后：${entity}Service,"
echo "  5. pkg/constants/module_mapping.go：\"${NAME}Service\": identityV1.Module_${MODULE}"
echo "  6. Api 表登记 (path=${ROUTE}, method=GET) + 权限 + 套餐（参照 scripts/bootstrap-model-access.sql 写专项脚本）"
echo
echo "生成与编译（在约定的执行环境运行，见 AGENTS.md）："
echo "  gow api && make build_only"
echo "  之后按 docs/service-integration.md §10 验收清单做真实链路验证"

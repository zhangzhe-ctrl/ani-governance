# Makefile for managing the Go microservices project

# load environment variables from .env file if it exists
ifneq (,$(wildcard .env))
    include .env
    export
endif

CURRENT_DIR	:= $(patsubst %/,%,$(dir $(abspath $(lastword $(MAKEFILE_LIST)))))
ROOT_DIR	:= $(dir $(realpath $(lastword $(MAKEFILE_LIST))))

SRCS_MK		:= $(foreach dir, app, $(wildcard $(dir)/*/*/Makefile))

.PHONY: help gen ent build api openapi pgv init all vendor dep test cover vet lint docker \
		register install-dev install-prod pm2-deploy

# show environment variables
env:
	echo "CURRENT_DIR: $(CURRENT_DIR)"
	echo "ROOT_DIR: $(ROOT_DIR)"
	echo "SRCS_MK: $(SRCS_MK)"

# 固定开发工具版本（migration/patches/T15/T15-tool-lock.json）：
# 版本来自本机二进制的 go version -m 构建信息，不用 @latest，不改应用依赖选择，不进生产镜像。
PROTOC_GEN_GO_VER ?= v1.36.11
PROTOC_GEN_GO_GRPC_VER ?= v1.6.2
PROTOC_GEN_GO_HTTP_VER ?= v2.0.0-20260404020628-f149714c1d54
PROTOC_GEN_GO_ERRORS_VER ?= v2.0.0-20260404020628-f149714c1d54
PROTOC_GEN_OPENAPI_VER ?= v0.7.1
PROTOC_GEN_VALIDATE_VER ?= v1.3.3
BUF_VER ?= v1.60.0
ENT_VER ?= v0.14.6

# initialize develop environment
init: plugin cli

# install protoc plugin
plugin:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VER)
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC_VER)
	go install github.com/go-kratos/kratos/cmd/protoc-gen-go-http/v2@$(PROTOC_GEN_GO_HTTP_VER)
	go install github.com/go-kratos/kratos/cmd/protoc-gen-go-errors/v2@$(PROTOC_GEN_GO_ERRORS_VER)
	go install github.com/google/gnostic/cmd/protoc-gen-openapi@$(PROTOC_GEN_OPENAPI_VER)
	go install github.com/envoyproxy/protoc-gen-validate@$(PROTOC_GEN_VALIDATE_VER)
	# protoc-gen-go-redact 已接管到本仓库 pkg/localdeps/go-wind-toolkit/protoc-gen-go-redact
	# （锁定版本 v0.0.0-20260831125122-5bb4931991b2），从本地源码构建到 tools/bin，
	# 不再 go install 外部模块；生成链见 make api-redact。
	bash scripts/build-redact-plugin.sh

# install cli tools
cli:
	@echo 'kratos CLI 未钉版本：本机无该二进制可核验，见 migration/patches/T15/T15-tool-lock.json 的 unresolved_entries（本仓在用命令不需要它）'
	go install github.com/google/gnostic@$(PROTOC_GEN_OPENAPI_VER)   # 同一模块，与 protoc-gen-openapi 一起钉版
	go install github.com/bufbuild/buf/cmd/buf@$(BUF_VER)
	go install entgo.io/ent/cmd/ent@$(ENT_VER)
	@echo 'golangci-lint 未钉版本：本机无该二进制可核验，见 tool-lock unresolved_entries；make lint 需要时由使用方显式安装'
	@echo 'gow 不再从 github.com/tx7do 安装：本仓库在用命令已接管到 tools/localdeps/gow，用 make gow 构建到 tools/bin/gow'

.PHONY: gow tools-integration

# build the localized gow from this module's sources (in-use commands: api, ent, run, version)
gow:
	go build -trimpath -o tools/bin/gow ./tools/localdeps/gow/cmd/gow

# protoc-driven redact integration cases: an explicit developer-tool entry, never part of
# the business test gate or a production image. Missing protoc FAILS (no skip).
tools-integration: gow redact-plugin
	command -v protoc >/dev/null 2>&1 || { echo 'protoc is required for make tools-integration; install the task-pinned protoc (see docs) and put it on PATH'; exit 1; }
	go test -tags tools_integration -count=1 -timeout=900s ./pkg/localdeps/go-wind-toolkit/protoc-gen-go-redact/

# download dependencies of module
dep:
	go mod download

# create vendor
vendor:
	go mod vendor

# run tests
test:
	go test ./...

# run coverage tests
cover:
	go test -v ./... -coverprofile=coverage.out

# run static analysis
vet:
	go vet

# run lint
lint:
	golangci-lint run

# generate ent code
ent:
	$(foreach dir, $(dir $(realpath $(SRCS_MK))),\
      cd $(dir);\
      make ent;\
    )
	cd app/admin/service && go run ./cmd/schema > schema.sql

# generate code
gen: ent api openapi

# register a new CRUD module into the hand-written wiring (usage: make register ENTITY=product)
register:
	go run ./tools/register -entity $(ENTITY)

# generate protobuf api go code
# 业务模板用 ../tools/bin/protoc-gen-go-redact 生成脱敏代码，先确保该二进制与本地源码一致。
api: redact-plugin pgv
	cd api && \
	buf generate

# PGV 单独一遍：文件级范围由 api/buf.validate.gen.yaml 的清单固定（T15），clean:false 不动共享输出根。
pgv:
	cd api && \
	buf generate --template buf.validate.gen.yaml

# build the localized protoc-gen-go-redact plugin from pkg/localdeps sources
redact-plugin:
	bash scripts/build-redact-plugin.sh

# generate the localized redact Proto into the takeover package.
# The Proto source is api/localdeps/redact and buf.redact.gen.yaml is the only
# template that writes that target, so redact.pb.go is generated exactly once.
# BUF must point at a verified buf v1.60.0; the script refuses any other version.
api-redact:
	bash scripts/generate-redact.sh

# generate the localized pagination Proto into pkg/localdeps.
# The Proto source is api/localdeps/pagination and this is the only template that
# writes that target, so pagination is generated exactly once. BUF must point at a
# verified buf v1.60.0; the script refuses any other version.
api-pagination:
	bash scripts/generate-pagination.sh

# generate the localized bootstrap conf Proto into pkg/localdeps.
# The Proto source is api/localdeps/bootstrap and buf.bootstrap.conf.gen.yaml is the only
# template that writes that target, so the 17 conf files are generated exactly once.
# BUF must point at a verified buf v1.60.0; the script refuses any other version.
api-bootstrap-conf:
	bash scripts/generate-bootstrap-conf.sh

# generate protobuf api OpenAPI v3 docs.
openapi:
	cd api && \
	buf generate --template buf.admin.openapi.gen.yaml
	python3 scripts/finalize-aksk-openapi.py

# build all service applications
build: api openapi
	$(foreach dir, $(dir $(realpath $(SRCS_MK))),\
      cd $(dir);\
      make build;\
    )

# only build all service applications without generating api and openapi
build_only:
	$(foreach dir, $(dir $(realpath $(SRCS_MK))),\
      cd $(dir);\
      make build_only;\
    )

# export configuration to etcd
export:
	cfgexp \
		--type=etcd \
		--addr=localhost:2379 \
		--proj=$(PROJECT_NAME)

# generate & build all service applications
all:
	$(foreach dir, $(dir $(realpath $(SRCS_MK))),\
      cd $(dir);\
      make app;\
    )

# build docker image
docker:
	$(foreach dir, $(dir $(realpath $(SRCS_MK))),\
      cd $(dir);\
      make docker;\
    )

# ============================================================================
# Script Commands - 脚本命令
# ============================================================================

# install development environment (Unix/Linux/macOS)
install-dev:
	echo "Installing development environment..."
	bash scripts/env/install_unix_dev.sh

# install production environment (Unix/Linux/macOS)
install-prod:
	echo "Installing production environment..."
	bash scripts/env/install_unix_prod.sh

# install golang only
install-golang:
	echo "Installing Golang..."
	bash scripts/env/install_golang.sh

# deploy services with PM2
pm2-deploy:
	echo "Deploying services with PM2..."
	bash scripts/deploy/pm2_service.sh

# show help
help:
	echo ""
	echo "Usage:"
	echo " make [target]"
	echo ""
	echo "Targets:"
	awk '/^[a-zA-Z\-_0-9]+:/ { \
	helpMessage = match(lastLine, /^# (.*)/); \
		if (helpMessage) { \
			helpCommand = substr($$1, 0, index($$1, ":")-1); \
			helpMessage = substr(lastLine, RSTART + 2, RLENGTH); \
			printf "\033[36m%-22s\033[0m %s\n", helpCommand,helpMessage; \
		} \
	} \
	{ lastLine = $$0 }' $(MAKEFILE_LIST)

.DEFAULT_GOAL := help

# Build the PostgreSQL-only data initialization and API catalog tool (no generation).
.PHONY: build_admin
build_admin:
	go build -trimpath -ldflags "-s -w" -o bin/admin ./app/admin/service/cmd/admin

# GPU quota slice: source generation and software-only gates. Run on the task's
# authorized Fedora host with its private caches and explicitly migrated PG DSN.
.PHONY: verify-quota-ent verify-gpu verify-gpu-regressions verify-gpu-audit
verify-quota-ent:
	bash scripts/verify-quota-schema.sh
	python3 scripts/check-quota-ent.py

verify-gpu: verify-quota-ent verify-gpu-regressions verify-gpu-audit

verify-gpu-regressions:
	@test -n "$(QUOTA_LAB_PG_DSN)" || (echo 'QUOTA_LAB_PG_DSN required'; exit 1)
	go version
	go list -m github.com/zhangzhe-ctrl/ani-accelerator-service
	bash scripts/verify-gpu-format.sh
	go build -o /dev/null ./app/admin/service/cmd/server
	go build -tags quota_lab -o /dev/null ./app/admin/service/cmd/server
	go build -o /dev/null ./app/admin/service/cmd/admin
	# The taken-over transport tests need a queue broker: scripts/verify-gpu-redis.sh
	# starts a loopback-only, non-persistent container for this recipe and exports
	# ANI_TEST_REDIS_URI. Those tests fail rather than skip when it is absent, so the
	# variable must come from here and not from a developer shell.
	bash scripts/verify-gpu-redis.sh run -- go test ./app/admin/service/internal/data ./app/admin/service/internal/service ./app/admin/service/internal/server ./pkg/...
	go test -tags quota_pg ./app/admin/service/internal/data -run 'TestQuota|TestPlanQuota' -count=1 -timeout=15m
	go test -tags quota_pg ./app/admin/service/internal/service -run 'TestPlanQuota' -count=1 -timeout=10m
	go test -race -tags quota_pg ./app/admin/service/internal/data -run 'TestQuotaEnt|TestQuotaPostgresConcurrentLimit|TestQuotaPostgresCancelClaimRace|TestQuotaPostgresLeaseGenerationGuard|TestQuotaPostgresPolicyChanges|TestQuotaGpu|TestQuotaProcess' -count=1 -timeout=20m
	go test -race ./app/admin/service/internal/service -run 'TestGpu|TestQuotaDurable|TestAccelerator' -count=1 -timeout=10m
	QUOTA_LAB_PG_DSN="$${QUOTA_LAB_REGRESSION_DSN:-$$QUOTA_LAB_PG_DSN}" bash scripts/accelerator-acceptance/lab-regression.sh

verify-gpu-audit:
	bash scripts/verify-gpu-audit.sh

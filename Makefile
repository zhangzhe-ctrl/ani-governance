# Makefile for managing the Go microservices project

# load environment variables from .env file if it exists
ifneq (,$(wildcard .env))
    include .env
    export
endif

CURRENT_DIR	:= $(patsubst %/,%,$(dir $(abspath $(lastword $(MAKEFILE_LIST)))))
ROOT_DIR	:= $(dir $(realpath $(lastword $(MAKEFILE_LIST))))

SRCS_MK		:= $(foreach dir, app, $(wildcard $(dir)/*/*/Makefile))

.PHONY: help gen ent build api openapi init all vendor dep test cover vet lint docker \
		register install-dev install-prod pm2-deploy

# show environment variables
env:
	echo "CURRENT_DIR: $(CURRENT_DIR)"
	echo "ROOT_DIR: $(ROOT_DIR)"
	echo "SRCS_MK: $(SRCS_MK)"

# initialize develop environment
init: plugin cli

# install protoc plugin
plugin:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	go install github.com/go-kratos/kratos/cmd/protoc-gen-go-http/v2@latest
	go install github.com/go-kratos/kratos/cmd/protoc-gen-go-errors/v2@latest
	go install github.com/google/gnostic/cmd/protoc-gen-openapi@latest
	go install github.com/envoyproxy/protoc-gen-validate@latest
	go install github.com/tx7do/go-wind-toolkit/protoc-gen-go-redact@v0.0.0-20260831125122-5bb4931991b2

# install cli tools
cli:
	go install github.com/go-kratos/kratos/cmd/kratos/v2@latest
	go install github.com/google/gnostic@latest
	go install github.com/bufbuild/buf/cmd/buf@latest
	go install entgo.io/ent/cmd/ent@latest
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	go install github.com/tx7do/go-wind-toolkit/gowind/cmd/gow@v1.0.3

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
api:
	cd api && \
	buf generate

# generate the localized pagination Proto into pkg/localdeps.
# The Proto source is api/localdeps/pagination and this is the only template that
# writes that target, so pagination is generated exactly once. BUF must point at a
# verified buf v1.60.0; the script refuses any other version.
api-pagination:
	bash scripts/generate-pagination.sh

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
	go test ./app/admin/service/internal/data ./app/admin/service/internal/service ./app/admin/service/internal/server ./pkg/...
	go test -tags quota_pg ./app/admin/service/internal/data -run 'TestQuota|TestPlanQuota' -count=1 -timeout=15m
	go test -tags quota_pg ./app/admin/service/internal/service -run 'TestPlanQuota' -count=1 -timeout=10m
	go test -race -tags quota_pg ./app/admin/service/internal/data -run 'TestQuotaEnt|TestQuotaPostgresConcurrentLimit|TestQuotaPostgresCancelClaimRace|TestQuotaPostgresLeaseGenerationGuard|TestQuotaPostgresPolicyChanges|TestQuotaGpu|TestQuotaProcess' -count=1 -timeout=20m
	go test -race ./app/admin/service/internal/service -run 'TestGpu|TestQuotaDurable|TestAccelerator' -count=1 -timeout=10m
	QUOTA_LAB_PG_DSN="$${QUOTA_LAB_REGRESSION_DSN:-$$QUOTA_LAB_PG_DSN}" bash scripts/accelerator-acceptance/lab-regression.sh

verify-gpu-audit:
	bash scripts/verify-gpu-audit.sh

# Validation commands and evidence scope

All commands were executed on Fedora in the task work directories with
`GOWORK=off GOMAXPROCS=2 GOFLAGS=-p=2` and task-private module/build caches.
Only the affected test names were selected; package test discovery did not run
unrelated tests. All cluster commands targeted `gov-model-20260919-01` on the
user-authorized `172.16.101.10–12` cluster.

Governance targeted tests (selected across the implementation and final fixes):

```sh
go test ./app/admin/service/internal/data -run 'Test(ModelClientIdentityAndCancellation|ModelClientTransportOutage|ResourceTenantUUIDPersistence)$'
go test ./app/admin/service/internal/service -run 'Test(ModelListTrustedIdentity|ModelQueryContract)$'
go test ./pkg/middleware/auth -run TestModelAuthorizationTenantDomain
go vet ./app/admin/service/internal/data ./app/admin/service/internal/service ./app/admin/service/internal/server ./app/admin/service/cmd/server ./pkg/middleware/auth
CGO_ENABLED=0 go build -trimpath -o "$R/ani-governance" ./app/admin/service/cmd/server
```

Model targeted tests:

```sh
go test ./internal/server ./internal/service ./cmd/ani-model-service -run 'Test(GovernanceMTLSBoundary|CatalogReadinessLiveProbe|CatalogListContract|Principal.*|Readiness.*|BuildApp.*|CatalogStartupRequiresIdentityConfiguration)$'
go vet ./internal/server ./internal/service ./cmd/ani-model-service
CGO_ENABLED=0 go build -trimpath -o "$R/ani-model-service" ./cmd/ani-model-service
```

The archive contains the actual per-run output; commands above consolidate the
selected checks, not a claim that a whole repository suite ran. Compilation,
generation, startup and acceptance failures were diagnosed and only their
affected checks repeated. Initial go mod tidy explored unrelated upstream test
dependencies and hit download EOF; targeted `-mod=mod` resolution supplied the
required build closure. Full dependency-test suites were not run.

`generate-model-slice.sh` generated the declared backend closure. SHA-256 lists
before/after a second generation were identical. gnostic 0.7.1 omits property
constraints from GET parameter schemas; the generated operation description
explicitly carries the range/default/status constraints and response statuses.
No generated Go or OpenAPI output was hand-edited.

`model-lab/accept.py` stages: setup, fixture, login, contract, recovery, revoke,
document. They use real captcha/login HTTP with a Cookie jar, real PostgreSQL,
and real mTLS. Model fixtures contain A=105, B=3, empty=0 rows. Each HTTP rejection
that should stop in Governance records zero change in the Model ListModels
request counter. The public projection is compared with the database's actual
ordered rows. Recovery tests lock only the fixture table, scale only task Model
or task Model DB deployments, and restore them in finally blocks.

No full `make verify`, `go test ./...`, platform suite, frontend/TS check, full
race/lint, remote push, PR, CI, public artifact publication, main-branch merge,
old-data migration, production switch or old-resource deletion was performed.

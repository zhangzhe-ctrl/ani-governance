#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
: "${QUOTA_LAB_PG_DSN:?restricted task PostgreSQL DSN required}"
: "${GUARD_TEST_PG_DSN:?exclusive guard PostgreSQL DSN required}"
: "${QUOTA_RACE_DATA_PG_DSN:?exclusive race data PostgreSQL DSN required}"
: "${QUOTA_RACE_SERVICE_PG_DSN:?exclusive race service PostgreSQL DSN required}"
test "${QUOTA_PG_EXCLUSIVE:-}" = 1 || { echo 'QUOTA_PG_EXCLUSIVE=1 required' >&2; exit 1; }
test "${GUARD_TEST_PG_EXCLUSIVE:-}" = 1 || { echo 'GUARD_TEST_PG_EXCLUSIVE=1 required' >&2; exit 1; }
evidence=${ANI_CI_EVIDENCE_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/ani-ci-integration.XXXXXXXX")}
mkdir -p "$evidence"
log="$evidence/go-tests.jsonl"
: > "$log"
go test -json -tags integration -count=1 -run '^(TestTenantMutationGuard|TestTenantMutationGuardAccessKey|TestPositionDataScopeGuard|TestOrgUnitPathExpansionTenantBounded|TestTenantGuardDeleteOneCreate)$' ./app/admin/service/internal/data/ent >> "$log"
go test -json -tags quota_pg -count=1 -timeout=20m ./app/admin/service/internal/data >> "$log"
bash scripts/ci/with-redis.sh run -- go test -json -tags quota_pg -count=1 -timeout=10m ./app/admin/service/internal/server >> "$log"
bash scripts/ci/with-redis.sh run -- go test -json -tags quota_pg -count=1 -timeout=10m ./app/admin/service/internal/service >> "$log"
python3 scripts/ci/assert-test-results.py --required tests/manifests/critical-tests.json --log "$log" --summary "$evidence/critical-results.json"
bash scripts/ci/with-redis.sh run -- go test -count=1 ./app/admin/service/internal/service ./app/admin/service/internal/server
QUOTA_LAB_PG_DSN="$QUOTA_RACE_DATA_PG_DSN" go test -race -tags quota_pg -count=1 -timeout=20m -run '^(TestQuotaPostgresConcurrentLimit|TestQuotaPostgresCancelClaimRace|TestQuotaPostgresLeaseGenerationGuard)$' ./app/admin/service/internal/data
QUOTA_LAB_PG_DSN="$QUOTA_RACE_SERVICE_PG_DSN" go test -race -tags quota_pg -count=1 -timeout=20m -run '^TestQuotaDispatchRetryAfterLostAck$' ./app/admin/service/internal/service

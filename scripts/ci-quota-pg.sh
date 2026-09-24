#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
# Use a disposable service PostgreSQL supplied by CI. This is explicit test
# provisioning, never an application startup path.
: "${PGHOST:?}" "${PGPORT:?}" "${PGUSER:?}"
psql -X -v ON_ERROR_STOP=1 -d postgres -f sql/gpu/ops/task_roles.sql
for source in migrations/20260921134442_initial.sql migrations/20260922190000_quota_expand.sql sql/quota/001_catalog_and_backfill.sql migrations/20260922190100_quota_constraints.sql migrations/20260923130000_gpu_sqlc.sql migrations/20260923130100_gpu_no_rls_catalog.sql migrations/20260923130200_gpu_sync_ref.sql migrations/20260923143455_drop_geo_location.sql; do
  PGUSER=gov_acc_migrate psql -X -v ON_ERROR_STOP=1 -d gov_acc_data -f "$source"
done
PGUSER=gov_acc_migrate psql -X -v ON_ERROR_STOP=1 -d gov_acc_data -v migrate_role=gov_acc_migrate -v runtime_role=gov_acc_runtime -f sql/gpu/ops/runtime_grants.sql
PGUSER=gov_acc_runtime psql -X -v ON_ERROR_STOP=1 -d gov_acc_data -f sql/gpu/ops/rls_audit.sql
createdb -O gov_acc_migrate -T gov_acc_data gov_acc_lab
psql -X -v ON_ERROR_STOP=1 -d postgres -v database_name=gov_acc_lab -v runtime_role=gov_acc_runtime -f sql/gpu/ops/restrict_database.sql

set -euo pipefail
r=/home/chabking/gov-ent-review-fix-20260924
old=/home/chabking/gov-acc-v12-01-20260923/evidence/gov-data
container=gov-ent-review-fix-pg
cd "$r/source"
sha256sum "$old/old-populated.dump" "$old/new-populated.dump" > "$r/evidence/historical-inputs.sha256"
for pair in 'gov_review_upgrade old-populated.dump' 'gov_review_projection new-populated.dump'; do
 read -r db dump <<< "$pair"
 podman exec "$container" createdb -U postgres -O gov_acc_migrate "$db"
 podman exec -i "$container" pg_restore -U gov_acc_migrate -d "$db" --no-owner --no-acl --exit-on-error < "$old/$dump" > "$r/evidence/$db-restore.log" 2>&1
 if [[ "$db" == gov_review_upgrade ]]; then
   for migration in 20260923130000_gpu_sqlc.sql 20260923130100_gpu_no_rls_catalog.sql 20260923130200_gpu_sync_ref.sql; do
     podman exec -i --workdir /work "$container" psql -U gov_acc_migrate -X -v ON_ERROR_STOP=1 -d "$db" -f "migrations/$migration" >> "$r/evidence/$db-restore.log" 2>&1
   done
 fi
 podman exec -i --workdir /work "$container" psql -U gov_acc_migrate -X -v ON_ERROR_STOP=1 -d "$db" -f migrations/20260923143455_drop_geo_location.sql >> "$r/evidence/$db-restore.log" 2>&1
 podman exec -i --workdir /work "$container" psql -U gov_acc_migrate -X -v ON_ERROR_STOP=1 -d "$db" -v migrate_role=gov_acc_migrate -v runtime_role=gov_acc_runtime -f sql/gpu/ops/runtime_grants.sql >> "$r/evidence/$db-restore.log" 2>&1
 podman exec -i --workdir /work "$container" psql -U postgres -X -v ON_ERROR_STOP=1 -d postgres -v database_name="$db" -v runtime_role=gov_acc_runtime -f sql/gpu/ops/restrict_database.sql >> "$r/evidence/$db-restore.log" 2>&1
 podman exec -i --workdir /work "$container" psql -U gov_acc_runtime -X -v ON_ERROR_STOP=1 -d "$db" -f sql/gpu/ops/rls_audit.sql >> "$r/evidence/$db-restore.log" 2>&1
done
# The restored source contains acknowledged terminal rows. Explicitly model a
# lost projection ACK on this disposable copy only; the original dump is immutable.
podman exec -i --workdir /work "$container" psql -U gov_acc_runtime -X -v ON_ERROR_STOP=1 -d gov_review_projection -f sql/gpu/ops/restore_pending_projection.sql > "$r/evidence/projection-fault.log" 2>&1
exec 9>"$r/verify.lock"
flock 9
export GOWORK=off GOMODCACHE="$r/mod" GOCACHE="$r/build" GOMAXPROCS=2 GOFLAGS=-p=2 GOPROXY=https://proxy.golang.org,direct
export QUOTA_LAB_PG_DSN='postgres://gov_acc_runtime@127.0.0.1:25435/gov_review_upgrade?sslmode=disable'
QUOTA_UPGRADE_VERIFY=1 go test -tags quota_pg ./app/admin/service/internal/data -run '^TestQuotaGpuOldPopulatedUpgradeReplay$' -count=1 -v -timeout=5m > "$r/evidence/upgrade.log" 2>&1
export QUOTA_LAB_PG_DSN='postgres://gov_acc_runtime@127.0.0.1:25435/gov_review_projection?sslmode=disable'
QUOTA_RESTORE_VERIFY=1 go test -tags quota_pg ./app/admin/service/internal/data -run '^TestQuotaGpuRestoredProjectionContinuation$' -count=1 -v -timeout=5m > "$r/evidence/restore.log" 2>&1
printf '0\n' > "$r/evidence/history.exit"
cat "$r/evidence/upgrade.log" "$r/evidence/restore.log"

set -euo pipefail
r=/home/chabking/gov-ent-review-fix-20260924
exec 9>"$r/verify.lock"
flock 9
cd "$r/source"
export GOWORK=off GOMODCACHE="$r/mod" GOCACHE="$r/build" GOMAXPROCS=2 GOFLAGS=-p=2 GOPROXY=https://proxy.golang.org,direct QUOTA_LAB_PG_DSN='postgres://gov_acc_runtime@127.0.0.1:25435/gov_acc_data?sslmode=disable'
set +e
go test -tags quota_pg ./app/admin/service/internal/data -run '^TestPlanQuotaPostgresListCompatibility$|^TestQuotaEnt|^TestQuotaGpuProductionEntComposition$' -count=1 -v -timeout=10m > "$r/evidence/focused.log" 2>&1
code=$?
echo "$code" > "$r/evidence/focused.exit"
tail -35 "$r/evidence/focused.log"
exit "$code"

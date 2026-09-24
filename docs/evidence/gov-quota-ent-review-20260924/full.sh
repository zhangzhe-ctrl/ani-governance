set -euo pipefail
r=/home/chabking/gov-ent-review-fix-20260924
exec 9>"$r/verify.lock"
flock 9
cd "$r/source"
export GOWORK=off GOMODCACHE="$r/mod" GOCACHE="$r/build" GOMAXPROCS=2 GOFLAGS=-p=2 GOPROXY=https://proxy.golang.org,direct
export QUOTA_LAB_PG_DSN='postgres://gov_acc_runtime@127.0.0.1:25435/gov_acc_data?sslmode=disable'
export QUOTA_LAB_REGRESSION_DSN='postgres://gov_acc_runtime@127.0.0.1:25435/gov_acc_lab?sslmode=disable'
export QUOTA_LAB_PG_ADMIN_DSN='postgres://postgres@127.0.0.1:25435/postgres?sslmode=disable'
export QUOTA_LAB_CERTS_DIR="$r/lab/certs"
export GOVULNCHECK=/home/chabking/gov-acc-v12-01-20260923/acc/.tools/bin/govulncheck
export GITLEAKS=/home/chabking/gov-acc-v12-01-20260923/acc/.tools/bin/gitleaks
{ go version; git rev-parse HEAD; git status --porcelain; date -u +%FT%TZ; } > "$r/evidence/full-environment.txt"
set +e
make verify-gpu > "$r/evidence/verify.log" 2>&1
code=$?
echo "$code" > "$r/evidence/verify.exit"
date -u +%FT%TZ > "$r/evidence/verify.finished"
tail -45 "$r/evidence/verify.log"
exit "$code"

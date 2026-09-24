#!/usr/bin/env bash
set -euo pipefail
export GOWORK=off GOMAXPROCS=2 GOFLAGS=-p=2
if [[ -n ${GOV_ACC_TASK_ROOT:-} ]]; then
  lab_dir="$GOV_ACC_TASK_ROOT/task/lab"
  export QUOTA_LAB_PG_DSN
  QUOTA_LAB_PG_DSN=$(cat "$lab_dir/gov-dsn")
  export QUOTA_LAB_PG_ADMIN_DSN
  QUOTA_LAB_PG_ADMIN_DSN=$(cat "$lab_dir/admin-dsn")
  export QUOTA_LAB_CERTS_DIR="$lab_dir/certs"
fi
: "${QUOTA_LAB_PG_DSN:?explicit restricted runtime DSN required}"
: "${QUOTA_LAB_PG_ADMIN_DSN:?explicit fixture administrator DSN required}"
: "${QUOTA_LAB_CERTS_DIR:?independent lab certificates required}"
test -s "$QUOTA_LAB_CERTS_DIR/ca.pem"
go test -tags quota_lab ./app/admin/service/internal/quotalab/simulator -run '^TestSimPG_' -count=1 -v

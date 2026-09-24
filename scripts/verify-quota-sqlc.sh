#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
: "${SQLC:?set SQLC to the pinned sqlc v1.30.0 executable}"
[[ "$("$SQLC" version)" = v1.30.0 ]]
"$SQLC" compile
snapshot=$(mktemp -d)
trap 'rm -rf "$snapshot"' EXIT
cp -a app/admin/service/internal/data/quotasql "$snapshot/quotasql"
"$SQLC" generate
diff -ru "$snapshot/quotasql" app/admin/service/internal/data/quotasql
python3 scripts/check-quota-sql.py
sha256sum sqlc.yaml app/admin/service/schema.sql app/admin/service/internal/data/quotasql/*.go app/admin/service/internal/data/quotasql/queries/*.sql
"$SQLC" version
sha256sum "$SQLC"

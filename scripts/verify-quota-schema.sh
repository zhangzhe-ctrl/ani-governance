#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export GOWORK=off
snapshot=$(mktemp -d)
trap 'rm -rf "$snapshot"' EXIT
cp -a app/admin/service/internal/data/ent "$snapshot/ent"
cp app/admin/service/schema.sql "$snapshot/schema.sql"
(cd app/admin/service && go run entgo.io/ent/cmd/ent@v0.14.6 generate \
 --feature privacy --feature entql --feature sql/modifier --feature sql/upsert --feature sql/lock ./internal/data/ent/schema \
 && go run ./cmd/schema > schema.sql)
diff -ru "$snapshot/ent" app/admin/service/internal/data/ent
diff -u "$snapshot/schema.sql" app/admin/service/schema.sql

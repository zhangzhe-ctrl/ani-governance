#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
test -f app/admin/service/schema.sql || { echo 'schema.sql missing' >&2; exit 1; }
test -d app/admin/service/internal/data/ent/schema || { echo 'Ent schema missing' >&2; exit 1; }
work=$(mktemp -d "${TMPDIR:-/tmp}/ani-ent-check.XXXXXXXX")
trap 'rm -rf "$work"' EXIT
git archive --format=tar HEAD | tar -x -C "$work"
(
  cd "$work/app/admin/service"
  GOWORK=off go run entgo.io/ent/cmd/ent@v0.14.6 generate \
    --feature privacy --feature entql --feature sql/modifier --feature sql/upsert --feature sql/lock \
    ./internal/data/ent/schema
  GOWORK=off go run ./cmd/schema > schema.sql
)
diff -ru app/admin/service/internal/data/ent "$work/app/admin/service/internal/data/ent"
diff -u app/admin/service/schema.sql "$work/app/admin/service/schema.sql"

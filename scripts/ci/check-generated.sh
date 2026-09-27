#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
command -v protoc >/dev/null || { echo 'pinned protoc is required' >&2; exit 1; }
command -v buf >/dev/null || { echo 'pinned buf is required' >&2; exit 1; }
test "$(buf --version)" = 1.60.0 || { echo 'buf version mismatch' >&2; exit 1; }
work=$(mktemp -d "${TMPDIR:-/tmp}/ani-generation.XXXXXXXX")
trap 'rm -rf "$work"' EXIT
git archive --format=tar HEAD | tar -x -C "$work"
run_pass() {
  (
    cd "$work"
    GOWORK=off make api
    cd app/admin/service
    GOWORK=off go run entgo.io/ent/cmd/ent@v0.14.6 generate \
      --feature privacy --feature entql --feature sql/modifier --feature sql/upsert --feature sql/lock \
      ./internal/data/ent/schema
    GOWORK=off go run ./cmd/schema > schema.sql
  )
}
run_pass
cp -a "$work/api" "$work/api-pass-one"
cp -a "$work/app/admin/service/internal/data/ent" "$work/ent-pass-one"
cp "$work/app/admin/service/schema.sql" "$work/schema-pass-one.sql"
run_pass
diff -ru "$work/api-pass-one" "$work/api"
diff -ru "$work/ent-pass-one" "$work/app/admin/service/internal/data/ent"
diff -u "$work/schema-pass-one.sql" "$work/app/admin/service/schema.sql"
diff -ru api "$work/api"
diff -ru app/admin/service/internal/data/ent "$work/app/admin/service/internal/data/ent"
diff -u app/admin/service/schema.sql "$work/app/admin/service/schema.sql"

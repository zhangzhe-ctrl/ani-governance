#!/usr/bin/env bash
set -euo pipefail
# Execute only on the task-authorized remote build host; no Wire or Model slice.
export GOWORK=off GOMAXPROCS=2 GOFLAGS=-p=2
cd "$(dirname "$0")/.."
BUF=${BUF:?set BUF to verified buf v1.60.0}
test "$("$BUF" --version)" = 1.60.0
(cd api && "$BUF" generate --template buf.aksk.gen.yaml)
(cd app/admin/service && go run entgo.io/ent/cmd/ent@v0.14.6 generate --feature privacy --feature entql --feature sql/modifier --feature sql/upsert --feature sql/lock ./internal/data/ent/schema)
(cd app/admin/service && go run ./cmd/schema > schema.sql)
(cd api && "$BUF" generate --template buf.admin.openapi.gen.yaml)
python3 scripts/finalize-aksk-openapi.py

#!/usr/bin/env bash
set -euo pipefail
# Run on Fedora only. gow api traverses unrelated contracts; use its underlying
# Buf/Ent generators for this bounded backend closure, without clean or TS output.
export GOWORK=off GOMAXPROCS=2 GOFLAGS=-p=2
cd "$(dirname "$0")/.."
BUF=${BUF:?set BUF to a verified buf v1.60.0 executable}
test "$("$BUF" --version)" = 1.60.0
(cd api && "$BUF" generate --template buf.model.gen.yaml)
(cd app/admin/service && go run entgo.io/ent/cmd/ent@v0.14.6 generate --feature privacy --feature entql --feature sql/modifier --feature sql/upsert --feature sql/lock ./internal/data/ent/schema)
# The aggregate OpenAPI document is embedded for final API inventory seeding.
GOBIN=${MODEL_TOOLS_DIR:?set a task-private tools directory} go install github.com/google/gnostic/cmd/protoc-gen-openapi@v0.7.1
(cd api && PATH="$MODEL_TOOLS_DIR:$PATH" "$BUF" generate --template buf.admin.openapi.gen.yaml)

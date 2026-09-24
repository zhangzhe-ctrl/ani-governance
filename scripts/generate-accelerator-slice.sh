#!/usr/bin/env bash
set -euo pipefail
export GOWORK=off GOMAXPROCS=2 GOFLAGS=-p=2
cd "$(dirname "$0")/.."
BUF=${BUF:?set BUF to the pinned buf v1.60.0 binary}
test "$("$BUF" --version)" = 1.60.0
(cd api && "$BUF" generate --template buf.accelerator.gen.yaml)
(cd api && "$BUF" generate --template buf.admin.openapi.gen.yaml)

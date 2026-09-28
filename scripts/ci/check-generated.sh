#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
command -v protoc >/dev/null || { echo 'pinned protoc is required' >&2; exit 1; }
command -v buf >/dev/null || { echo 'pinned buf is required' >&2; exit 1; }
test "$(buf --version)" = 1.60.0 || { echo 'buf version mismatch' >&2; exit 1; }
source_repo=$PWD
sha=$(git rev-parse HEAD)
work=$(mktemp -d "${TMPDIR:-/tmp}/ani-generation.XXXXXXXX")
mkdir -p "$work/repo" "$work/manifests"
cleanup() {
  local rc=$? cleanup_rc=0
  trap - EXIT
  if [[ -n "${ANI_CI_EVIDENCE_DIR:-}" ]]; then
    mkdir -p "$ANI_CI_EVIDENCE_DIR" || cleanup_rc=$?
    if test "$cleanup_rc" -eq 0; then
      cp "$work"/manifests/*.json "$ANI_CI_EVIDENCE_DIR/" 2>/dev/null || cleanup_rc=$?
    fi
  fi
  rm -rf "$work" || cleanup_rc=$?
  if test "$rc" -ne 0; then exit "$rc"; fi
  exit "$cleanup_rc"
}
trap cleanup EXIT
compare="$source_repo/scripts/ci/compare-generated.py"
python3 "$compare" snapshot --repo "$source_repo" --sha "$sha" --output "$work/manifests/generation-baseline.json"
git archive --format=tar HEAD | tar -x -C "$work/repo"
run_pass() {
  (
    cd "$work/repo"
    GOWORK=off make api
    cd app/admin/service
    GOWORK=off go run entgo.io/ent/cmd/ent@v0.14.6 generate \
      --feature privacy --feature entql --feature sql/modifier --feature sql/upsert --feature sql/lock \
      ./internal/data/ent/schema
    GOWORK=off go run ./cmd/schema > schema.sql
  )
}
run_pass
python3 "$compare" snapshot --repo "$work/repo" --sha "$sha" --output "$work/manifests/generation-first.json"
run_pass
python3 "$compare" snapshot --repo "$work/repo" --sha "$sha" --output "$work/manifests/generation-second.json"
python3 "$compare" compare \
  --baseline "$work/manifests/generation-baseline.json" \
  --first "$work/manifests/generation-first.json" \
  --second "$work/manifests/generation-second.json"

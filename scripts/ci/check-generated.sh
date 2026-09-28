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
# The baseline snapshot is the current worktree. Apply its tracked edits and
# untracked source/generated files to the isolated archive before regenerating.
python3 "$compare" overlay --repo "$source_repo" --target "$work/repo"
python3 "$compare" snapshot --repo "$work/repo" --sha "$sha" --output "$work/manifests/generation-staged.json"
python3 "$compare" compare \
  --baseline "$work/manifests/generation-baseline.json" \
  --first "$work/manifests/generation-staged.json" \
  --second "$work/manifests/generation-staged.json"
python3 "$compare" snapshot --repo "$source_repo" --sha "$sha" --output "$work/manifests/generation-source-after-overlay.json"
python3 "$compare" compare \
  --baseline "$work/manifests/generation-baseline.json" \
  --first "$work/manifests/generation-source-after-overlay.json" \
  --second "$work/manifests/generation-source-after-overlay.json"
run_pass() {
  (
    cd "$work/repo"
    GOWORK=off make gen
  )
}
echo 'generation: first complete make gen pass'
run_pass
python3 "$compare" snapshot --repo "$work/repo" --sha "$sha" --output "$work/manifests/generation-first.json"
echo 'generation: second complete make gen pass'
run_pass
python3 "$compare" snapshot --repo "$work/repo" --sha "$sha" --output "$work/manifests/generation-second.json"
python3 "$compare" compare \
  --baseline "$work/manifests/generation-baseline.json" \
  --first "$work/manifests/generation-first.json" \
  --second "$work/manifests/generation-second.json"
python3 "$compare" snapshot --repo "$source_repo" --sha "$sha" --output "$work/manifests/generation-source-final.json"
python3 "$compare" compare \
  --baseline "$work/manifests/generation-baseline.json" \
  --first "$work/manifests/generation-source-final.json" \
  --second "$work/manifests/generation-source-final.json"
log_dir=${ANI_CI_EVIDENCE_DIR:-$work/manifests}
mkdir -p "$log_dir"
echo 'generation: localized gow tests'
if GOWORK=off go test -json -count=1 ./tools/... > "$log_dir/gow-tests.jsonl"; then
  printf '0\n' > "$log_dir/gow-tests.rc"
else
  rc=$?
  printf '%s\n' "$rc" > "$log_dir/gow-tests.rc"
  exit "$rc"
fi
echo 'generation: redact plugin integration'
if GOWORK=off make tools-integration > "$log_dir/tools-integration.log" 2>&1; then
  printf '0\n' > "$log_dir/tools-integration.rc"
else
  rc=$?
  printf '%s\n' "$rc" > "$log_dir/tools-integration.rc"
  exit "$rc"
fi

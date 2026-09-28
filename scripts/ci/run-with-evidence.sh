#!/usr/bin/env bash
# One command, one immutable evidence step. Raw files remain outside upload root.
set -uo pipefail
cd "$(dirname "$0")/../.." || exit 2

step=${1:-}
shift || exit 2
if [[ ! "$step" =~ ^[a-z0-9-]+$ || "${1:-}" != -- ]]; then
  echo 'usage: run-with-evidence.sh STEP -- COMMAND [ARGS...]' >&2
  exit 2
fi
shift
test "$#" -gt 0 || { echo 'missing command' >&2; exit 2; }
: "${ANI_CI_EVIDENCE_ROOT:?public evidence root required}"
: "${ANI_CI_RAW_EVIDENCE_ROOT:?raw evidence root required}"
repo=$(pwd -P)
public_root=$(realpath -m "$ANI_CI_EVIDENCE_ROOT")
raw_root=$(realpath -m "$ANI_CI_RAW_EVIDENCE_ROOT")
case "$public_root/" in "$repo/"* ) echo 'public evidence must be outside repository' >&2; exit 2;; esac
case "$raw_root/" in "$repo/"* ) echo 'raw evidence must be outside repository' >&2; exit 2;; esac
test "$public_root" != "$raw_root" || { echo 'raw and public evidence roots must differ' >&2; exit 2; }
umask 077
mkdir -p "$public_root" "$raw_root"
public="$public_root/$step"
raw="$raw_root/$step"
mkdir -m 700 "$public" "$raw" || { echo "evidence step already exists: $step" >&2; exit 2; }
sha=$(git rev-parse HEAD) || exit 2
printf '%s\n' "$sha" > "$raw/source.sha"
printf '%s\n' "$sha" > "$public/source.sha"
printf '%s\n' 'running' > "$public_root/job-status.txt"
printf '%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$raw/started-at.txt"
cp "$raw/started-at.txt" "$public/started-at.txt"
printf '%q ' "$@" > "$raw/command.txt"
printf '\n' >> "$raw/command.txt"
python3 scripts/ci/redact-evidence.py "$raw/command.txt" "$public/command.txt" || exit 2
export ANI_CI_EVIDENCE_DIR="$raw" ANI_CI_RESOURCE_LOG="$raw/resources.log"
set +e
"$@" > "$raw/stdout.log" 2> "$raw/stderr.log"
command_rc=$?
set -e
printf '%s\n' "$command_rc" > "$raw/command.rc"
cp "$raw/command.rc" "$public/command.rc"
printf '%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$raw/ended-at.txt"
cp "$raw/ended-at.txt" "$public/ended-at.txt"

evidence_rc=0
for name in stdout.log stderr.log go-tests.jsonl critical-results.json \
            generation-baseline.json generation-first.json generation-second.json \
            gow-tests.jsonl gow-tests.rc tools-integration.log tools-integration.rc resources.log; do
  if test -f "$raw/$name"; then
    python3 scripts/ci/redact-evidence.py "$raw/$name" "$public/$name" || evidence_rc=$?
  fi
done
(
  cd "$raw" || exit 1
  for file in *; do test ! -f "$file" || sha256sum "$file"; done
) > "$public/raw-sha256.txt" || evidence_rc=$?
printf '%s\n' "$evidence_rc" > "$public/evidence.rc"
if test "$command_rc" -eq 0 && test "$evidence_rc" -eq 0; then
  printf '%s\n' 'pass' > "$public_root/job-status.txt"
else
  printf '%s\n' 'fail' > "$public_root/job-status.txt"
fi
cat "$public/stdout.log" 2>/dev/null || true
cat "$public/stderr.log" >&2 2>/dev/null || true
printf 'evidence step=%s sha=%s command_rc=%s evidence_rc=%s\n' "$step" "$sha" "$command_rc" "$evidence_rc"
if test "$command_rc" -ne 0; then exit "$command_rc"; fi
exit "$evidence_rc"

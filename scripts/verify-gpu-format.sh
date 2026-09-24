#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
# Frozen list includes all changed Go files and works in source archives. Git
# checkouts additionally cover subsequent changes from the accepted base.
mapfile -t sources < scripts/gpu-format-files.txt
if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  base=${GOV_GPU_BASE:-0fbe1a69e49cc23dc7a1696b62f68c34a7c6a48a}
  git cat-file -e "$base^{commit}"
  mapfile -t extra < <(git diff --name-only --diff-filter=ACMR "$base" -- '*.go'; git ls-files --others --exclude-standard '*.go')
  sources+=("${extra[@]}")
fi
unformatted=$(gofmt -l "${sources[@]}")
if [[ -n "$unformatted" ]]; then
  printf '%s\n' "$unformatted"
  exit 1
fi
if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then git diff --check; fi

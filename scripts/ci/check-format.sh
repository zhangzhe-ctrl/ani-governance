#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
mapfile -d '' -t sources < <(git ls-files -z -- '*.go')
((${#sources[@]})) || { echo 'no tracked Go source' >&2; exit 1; }
unformatted=$(gofmt -l "${sources[@]}")
if [[ -n "$unformatted" ]]; then
  printf '%s\n' "$unformatted" >&2
  exit 1
fi
git diff --check

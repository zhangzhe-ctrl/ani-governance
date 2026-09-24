#!/usr/bin/env bash
# Usage: bash run_checked.sh /absolute/task/evidence-name go test ...
# Environment variables should be set separately; never pass secret values as arguments.
set -euo pipefail
if (( $# < 2 )); then echo 'usage: run_checked.sh evidence-prefix command [args...]' >&2; exit 2; fi
prefix=$1; shift
mkdir -p "$(dirname "$prefix")"
printf '%q ' "$@" > "${prefix}.command"; printf '\n' >> "${prefix}.command"
date -u +%FT%TZ > "${prefix}.started"
set +e
"$@" 2>&1 | tee "${prefix}.log"
pipeline_status=("${PIPESTATUS[@]}")
set -e
status=${pipeline_status[0]}
# Also fail when evidence cannot be written, even if the command passed.
if (( status == 0 && pipeline_status[1] != 0 )); then status=${pipeline_status[1]}; fi
printf '%s\n' "$status" > "${prefix}.exit"
date -u +%FT%TZ > "${prefix}.finished"
exit "$status"

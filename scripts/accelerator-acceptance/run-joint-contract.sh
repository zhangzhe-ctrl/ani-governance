#!/usr/bin/env bash
set -euo pipefail
: "${GOV_ACC_JOINT_DIR:?isolated B-layer directory required}"
: "${GOMODCACHE:?task-owned module cache required}"
: "${GOCACHE:?task-owned build cache required}"
: "${GOV_ACC_EVIDENCE_DIR:?evidence directory required}"
test "$(hostname)" = fedora
export GOWORK=off GOMAXPROCS=2 GOFLAGS=-p=2
export GOPROXY=https://proxy.golang.org,direct
mkdir -p "$GOV_ACC_EVIDENCE_DIR"
run_dir="$GOV_ACC_EVIDENCE_DIR/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir "$run_dir"
date -u --iso-8601=seconds > "$run_dir/started_at"
git rev-parse HEAD > "$run_dir/governance-head"
git status --porcelain > "$run_dir/worktree-status"
go version > "$run_dir/go-version"
go list -m -json github.com/zhangzhe-ctrl/ani-accelerator-service > "$run_dir/accelerator-module.json"
git ls-files --cached --others --exclude-standard -z -- '*.go' '*.proto' '*.sql' '*.yaml' '*.mod' '*.sum' 'scripts/*' | sort -zu | xargs -0 sha256sum > "$run_dir/runtime-source.sha256"
python3 scripts/accelerator-acceptance/prepare-joint-config.py "$GOV_ACC_JOINT_DIR"
command=(go test -tags gpu_joint ./app/admin/service/tests/gpucontract -run '^TestJoint(SoftwareContract|MixedCharges|SyncFailureRecovery|DispatchFailuresAndStop|ResolvedSnapshotRace|PolicyRevocationRecovery)$' -count=1 -v -timeout=12m)
if test "${GOV_ACC_JOINT_RACE:-0}" = 1; then
    command=(go test -race "${command[@]:2}")
fi
printf '%q ' "${command[@]}" > "$run_dir/command"
printf '\n' >> "$run_dir/command"
set +e
"${command[@]}" > "$run_dir/test.log" 2>&1
result=$?
set -e
printf '%s\n' "$result" > "$run_dir/exit_code"
date -u --iso-8601=seconds > "$run_dir/finished_at"
cat "$run_dir/test.log"
if test "$result" -eq 0; then
    cp "$GOV_ACC_JOINT_DIR/joint-contract-result.json" "$run_dir/"
fi
exit "$result"

#!/usr/bin/env bash
# A fresh loopback Redis container for one test run. stop/uri require its state path.
set -euo pipefail
engine=${ANI_CONTAINER_CLI:-docker}
image=${ANI_TEST_REDIS_IMAGE:-redis:7.4.6}
state=${ANI_TEST_REDIS_STATE_DIR:-}
die() { echo "with-redis: $*" >&2; exit 1; }
record_resource() {
  if test -n "${ANI_CI_RESOURCE_LOG:-}"; then
    printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >> "$ANI_CI_RESOURCE_LOG"
  fi
}

owned() {
  test -n "$state" && test -f "$state/id" && test -f "$state/owner" || return 1
  local id owner actual
  id=$(cat "$state/id")
  owner=$(cat "$state/owner")
  actual=$($engine inspect --format '{{index .Config.Labels "ani-governance-run"}}' "$id" 2>/dev/null) || return 1
  test "$owner" = "$actual"
}

start() {
  test -n "$state" || die 'ANI_TEST_REDIS_STATE_DIR is required for start'
  mkdir -m 700 "$state" 2>/dev/null || die 'state directory already exists; refusing reuse'
  local owner name id address
  owner=$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')
  name="ani-governance-redis-$owner"
  printf '%s\n' "$owner" > "$state/owner"
  printf '%s\n' "$name" > "$state/name"
  id=$($engine run -d --name "$name" --label "ani-governance-run=$owner" \
    -p 127.0.0.1::6379 "$image" --save '' --appendonly no --shutdown-timeout 5) || die 'container start failed'
  printf '%s\n' "$id" > "$state/id"
  owned || die 'new container ownership mismatch'
  record_resource "redis start id=$id label=ani-governance-run=$owner image=$image"
  address=$($engine port "$id" 6379/tcp | head -1)
  case "$address" in 127.0.0.1:*) ;; *) die "non-loopback mapping: $address";; esac
  printf 'redis://%s/0\n' "$address" > "$state/uri"
  for _ in $(seq 1 50); do
    if $engine exec "$id" redis-cli --no-auth-warning PING >/dev/null 2>&1; then cat "$state/uri"; return 0; fi
    sleep 0.2
  done
  die 'Redis never answered PING'
}

stop() {
  owned || die 'refusing to remove container without matching run label'
  local id owner
  id=$(cat "$state/id")
  owner=$(cat "$state/owner")
  record_resource "redis cleanup_requested id=$id label=ani-governance-run=$owner"
  if ! $engine rm -f "$id" >/dev/null; then
    record_resource "redis cleanup_rc=1 id=$id label=ani-governance-run=$owner"
    die 'container cleanup failed'
  fi
  rm "$state/id" "$state/owner" "$state/name" "$state/uri"
  rmdir "$state"
  record_resource "redis cleanup_rc=0 id=$id label=ani-governance-run=$owner"
}

uri() { owned || die 'missing or foreign run container'; cat "$state/uri"; }

case "${1:-}" in
  start) start ;;
  stop) stop ;;
  uri) uri ;;
  run)
    shift
    test "${1:-}" = -- && shift
    test "$#" -gt 0 || die 'run -- requires a command'
    test -z "$state" || die 'run creates its own state; do not set ANI_TEST_REDIS_STATE_DIR'
    root=${ANI_TEST_REDIS_STATE_ROOT:-${TMPDIR:-/tmp}}
    mkdir -p "$root"
    state="$root/ani-redis-$(head -c 8 /dev/urandom | od -An -tx1 | tr -d ' \n')"
    export ANI_TEST_REDIS_URI
    cleanup_run() {
      local command_rc=$?
      trap - EXIT INT TERM
      local cleanup_rc=0
      if owned; then
        stop || cleanup_rc=$?
      elif test -d "$state" && test ! -e "$state/id"; then
        rm -f "$state/owner" "$state/name" "$state/uri"
        rmdir "$state" || cleanup_rc=$?
        record_resource "redis empty_state_cleanup_rc=$cleanup_rc state=$state"
      fi
      if test "$cleanup_rc" -ne 0; then echo 'with-redis: owned container cleanup failed' >&2; fi
      if test "$command_rc" -ne 0; then exit "$command_rc"; fi
      exit "$cleanup_rc"
    }
    trap 'exit 130' INT
    trap 'exit 143' TERM
    trap cleanup_run EXIT
    ANI_TEST_REDIS_URI=$(start)
    "$@"
    ;;
  *) die 'expected start, stop, uri, or run -- COMMAND' ;;
esac

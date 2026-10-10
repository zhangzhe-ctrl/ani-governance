#!/usr/bin/env bash
set -euo pipefail
# Public release API only. The authoritative Proto stays in api/protos; the
# temporary copy prevents a failed plugin from touching any managed output.
cd "$(dirname "$0")/.."
export GOWORK=off
BUF=${BUF:-buf}
test "$("$BUF" --version)" = 1.60.0
go version -m "$(command -v "$BUF")" | grep -Eq 'mod[[:space:]]+github.com/bufbuild/buf[[:space:]]+v1.60.0'
work=$(mktemp -d "${TMPDIR:-/tmp}/quota-api.XXXXXXXX")
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/api/protos/quota/service/v1" "$work/api/quota/gen/go"
cp api/protos/quota/service/v1/quota_release.proto "$work/api/protos/quota/service/v1/"
cp api/buf.quota-release.gen.yaml "$work/api/"
printf 'version: v2\nmodules:\n  - path: protos\n' > "$work/api/buf.yaml"
dest=api/quota/gen/go
mkdir -p "$dest"
before=$(find "$dest" -type f -print0 | sort -z | xargs -0 -r sha256sum)
(cd "$work/api" && "$BUF" generate --template buf.quota-release.gen.yaml)
test "$before" = "$(find "$dest" -type f -print0 | sort -z | xargs -0 -r sha256sum)" || {
  echo 'quota API output changed during generation; no output copied' >&2
  exit 1
}
cp -R "$work/api/quota/gen/go/." "$dest/"

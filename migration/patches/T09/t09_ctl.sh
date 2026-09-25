#!/usr/bin/env bash
# T09 control: generate the same api tree twice, before and after the takeover,
# in throwaway directories, then classify every differing file.
#
#   pre  = api/ exactly as committed at HEAD (external BSR buf.build/go-wind/redact
#          dep + the go-install'ed external protoc-gen-go-redact plugin).
#   post = api/ as it now stands (local redact Proto, local plugin binary).
#
# Both runs use the same buf, the same protoc-gen-go pin and the same toolchain;
# only the wiring differs, so any diff between the two trees is attributable to
# the takeover. The third comparison (pre-generated vs committed HEAD) measures
# the regeneration noise that already exists in this repository independently of
# T09, which is what makes the pre/post comparison interpretable.
set -uo pipefail
REPO=/home/ubuntu/Workspace/ani-governance
D=/home/ubuntu/tx7do-pilot-run/T09
# matched plugin bins: identical except the redact plugin
export GOWORK=off GOFLAGS=-p=2
BUFCMD="go run github.com/bufbuild/buf/cmd/buf@v1.60.0"

rm -rf "$D/pre" "$D/post"; mkdir -p "$D/pre" "$D/post"
( cd "$REPO" && git archive HEAD api ) | tar -x -C "$D/pre"
cp -a "$REPO/api" "$D/post/api"
mkdir -p "$D/post/pkg/localdeps/go-wind-toolkit/protoc-gen-go-redact"

echo "pre  : BSR redact dep line in buf.yaml       = $(grep -c "^  - 'buf.build/go-wind/redact'" "$D/pre/api/buf.yaml" || true)"
echo "post : BSR redact dep line in buf.yaml       = $(grep -c "^  - 'buf.build/go-wind/redact'" "$D/post/api/buf.yaml" || true)"
echo "pre  : BSR redact entry in buf.lock          = $(grep -c 'owner: go-wind' "$D/pre/api/buf.lock" || true)"
echo "post : BSR redact entry in buf.lock          = $(grep -c 'owner: go-wind' "$D/post/api/buf.lock" || true)"
echo "pre  : plugin line                          = $(grep -n 'local:.*redact' "$D/pre/api/buf.gen.yaml")"
echo "post : plugin line                          = $(grep -n 'local:.*redact' "$D/post/api/buf.gen.yaml")"

run() { # ws path-prefix templates...
  local ws="$1" pfx="$2"; shift 2
  for t in "$@"; do
    ( cd "$ws/api" && PATH="$pfx" $BUFCMD generate --template "$t" ) >>"$ws/gen.log" 2>&1
    echo "exit($t)=$? $(basename "$t")" | tee -a "$ws/gen.log"
  done
}

# the pre-change run must be able to reach the externally installed plugin, exactly
# as it was when the committed .pb.redact.go files were produced.
PRE_PATH=$D/prepath:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin
# the post-change run gets a PATH with no protoc-gen-go-redact at all: only the
# repository-built binary addressed by ../tools/bin can serve it.
POST_PATH=$D/postpath:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin
test -x /home/ubuntu/go/bin/protoc-gen-go-redact || { echo "FAIL: external plugin missing, pre-run is not a real control"; exit 1; }
test -x "$REPO/tools/bin/protoc-gen-go-redact" || { echo "FAIL: local plugin binary not built"; exit 1; }

: > "$D/pre/gen.log"; : > "$D/post/gen.log"
echo "### pre-change generation (HEAD wiring)"
run "$D/pre"  "$PRE_PATH"  buf.gen.yaml
echo "### post-change generation (T09 wiring: localized redact Proto + local plugin)"
cp -a "$REPO/tools" "$D/post/tools"
run "$D/post" "$POST_PATH" buf.redact.gen.yaml buf.gen.yaml

for w in pre post; do
    if grep -qE '\)=[1-9]' "$D/$w/gen.log"; then
        echo "FAIL: $w generation reported a non-zero exit:"; grep -E '\)=[1-9]' "$D/$w/gen.log"
        tail -5 "$D/$w/gen.log"; exit 1
    fi
    echo "$w: all templates generated with exit 0"
done

# committed HEAD copy of the generated tree, so the noise measurement does not
# depend on the state of the working tree
rm -rf "$D/head"; mkdir -p "$D/head"
( cd "$REPO" && git archive HEAD api ) | tar -x -C "$D/head"

echo
echo "=== 1. regeneration noise that exists without T09 (pre-generated vs committed HEAD) ==="
( cd "$D" && diff -rq pre/api/gen/go head/api/gen/go ) > "$D/T09-noise-baseline.txt" 2>&1
echo "noise files: $(wc -l < "$D/T09-noise-baseline.txt") (see T09-noise-baseline.txt)"

echo
echo "=== 2. pre vs post generated api trees (only the takeover may change this) ==="
( cd "$D" && diff -rq pre/api/gen/go post/api/gen/go ) > "$D/T09-ctl-trees.diff" 2>&1
cat "$D/T09-ctl-trees.diff"

echo
echo "=== 3. redact runtime generation copy count and equality with the repo ==="
find "$D/post" -name 'redact.pb.go' | sed "s|$D/post/||"
diff -u "$D/post/pkg/localdeps/go-wind-toolkit/protoc-gen-go-redact/redact/v1/redact.pb.go" \
        "$REPO/pkg/localdeps/go-wind-toolkit/protoc-gen-go-redact/redact/v1/redact.pb.go" \
  && echo "repo redact.pb.go equals throwaway regeneration: byte-identical"
test ! -e "$D/post/api/gen/go/redact" && echo "no redact copy under api/gen/go: yes"


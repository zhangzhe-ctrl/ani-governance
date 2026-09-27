#!/usr/bin/env bash
set -euo pipefail
# T09 本地化脱敏 Proto 生成：输入只有 api/localdeps/redact 模块，
# 输出到 pkg/localdeps/go-wind-toolkit/protoc-gen-go-redact（全仓唯一一份 redact.pb.go，
# 不会在 api/gen/go/redact 出现副本）。业务 Proto 由各业务模板生成，不重复生成脱敏 Proto。
# 模板为 clean:false：该目录另有手写的 interface.go 与 stream.go，生成后按固定清单核对落点。
# 顺序：先出 redact.pb.go，再构建依赖它的插件二进制（生成器 import 运行包的 redact.E_* 符号）。
export GOWORK=off GOMAXPROCS=2 GOFLAGS=-p=2
cd "$(dirname "$0")/.."
BUF=${BUF:?set BUF to a verified buf v1.60.0 executable}
test "$("$BUF" --version)" = 1.60.0
OUT=pkg/localdeps/go-wind-toolkit/protoc-gen-go-redact

(cd api && "$BUF" generate --template buf.redact.gen.yaml)
bash scripts/build-redact-plugin.sh

for f in redact/v1/redact.pb.go redact/v1/interface.go redact/v1/stream.go; do
    test -f "$OUT/$f" || { echo "missing $OUT/$f after generation" >&2; exit 1; }
done
test ! -e api/gen/go/redact || {
    echo "unexpected duplicate redact output at api/gen/go/redact" >&2
    exit 1
}

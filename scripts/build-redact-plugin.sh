#!/usr/bin/env bash
set -euo pipefail
# T09 接管后的 protoc-gen-go-redact：插件二进制由本仓库源码构建，取代
# 原先 make plugin 里的 `go install github.com/tx7do/go-wind-toolkit/protoc-gen-go-redact@...`。
# api/buf.gen.yaml 以 ../tools/bin/protoc-gen-go-redact 显式调用该二进制，
# 因此不依赖 $PATH 里是否存在外部安装的同名插件。
# redact/v1/redact.pb.go 是随仓库提交的生成物；若缺失先执行 make api-redact。
export GOWORK=off GOMAXPROCS=2
# GOFLAGS 必须追加而不是覆盖：调用方的 -mod=readonly 一旦丢失，宿主 go env 里的
# -mod=mod 就会生效并把 go.mod 的 indirect 标记改写掉（T13 实测发生过一次）。
export GOFLAGS="${GOFLAGS:--mod=readonly} -p=2"
cd "$(dirname "$0")/.."
SRC=pkg/localdeps/go-wind-toolkit/protoc-gen-go-redact
test -f "$SRC/redact/v1/redact.pb.go" || {
    echo "missing $SRC/redact/v1/redact.pb.go: run 'bash scripts/generate-redact.sh' first" >&2
    exit 1
}
mkdir -p tools/bin
go build -o tools/bin/protoc-gen-go-redact "./$SRC"

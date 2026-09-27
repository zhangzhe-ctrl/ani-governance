#!/usr/bin/env bash
set -euo pipefail
# T08 本地化分页 Proto 生成：输入只有 api/localdeps/pagination 模块，
# 输出到 pkg/localdeps/go-crud/api/gen/go（唯一一份分页生成物）。
# 业务 Proto 由各业务模板生成（inputs 只选 protos），不会重复生成分页。
# clean:false，不运行 post-generate-clean.sh（该脚本只处理业务生成目录的既有噪声）。
export GOWORK=off GOMAXPROCS=2 GOFLAGS=-p=2
cd "$(dirname "$0")/.."
BUF=${BUF:?set BUF to a verified buf v1.60.0 executable}
test "$("$BUF" --version)" = 1.60.0
(cd api && "$BUF" generate --template buf.pagination.gen.yaml)

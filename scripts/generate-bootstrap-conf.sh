#!/usr/bin/env bash
set -euo pipefail
# T10 本地化配置 Proto 生成：输入只有 api/localdeps/bootstrap 模块（conf/v1/*.proto，
# kratos-bootstrap/api@v0.0.45 protos/conf/v1 的逐字节副本），输出到
# pkg/localdeps/kratos-bootstrap/api/gen/go（全仓唯一一份 conf 生成物）。
# 业务 Proto 由各业务模板生成（inputs 只选 protos），不会重复生成 conf。
# clean:false，不运行 post-generate-clean.sh（该脚本只处理业务生成目录的既有噪声）。
export GOWORK=off GOMAXPROCS=2 GOFLAGS=-p=2
cd "$(dirname "$0")/.."
BUF=${BUF:?set BUF to a verified buf v1.60.0 executable}
test "$("$BUF" --version)" = 1.60.0
(cd api && "$BUF" generate --template buf.bootstrap.conf.gen.yaml)

# 落点核对：17 个文件与归档 gen/go/conf/v1 同名同号，且不得在 api/gen/go 下出现副本。
want=$(cd api/localdeps/bootstrap/conf/v1 && ls *.proto | wc -l)
got=$(cd pkg/localdeps/kratos-bootstrap/api/gen/go/conf/v1 && ls *.pb.go | wc -l)
test "$got" = "$want" || { echo "FAIL: generated $got conf pb, expected $want"; exit 1; }
test ! -e api/gen/go/conf && echo "no conf copy under api/gen/go: yes" || {
  echo "FAIL: the business templates must not generate conf"; exit 1; }

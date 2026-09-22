#!/usr/bin/env bash
set -euo pipefail
# QUOTA-GPU-LOCAL-01 定向生成：生产配额与实验 GPU Proto 分别生成。
# 固定输入清单与版本；clean:false，不运行 post-generate-clean.sh。
# 实验路由（quota_lab）不进入正式 OpenAPI；本脚本不生成 OpenAPI。
export GOWORK=off GOMAXPROCS=2 GOFLAGS=-p=2
cd "$(dirname "$0")/.."
BUF=${BUF:?set BUF to a verified buf v1.60.0 executable}
test "$("$BUF" --version)" = 1.60.0
(cd api && "$BUF" generate --template buf.quota.gen.yaml)
(cd api && "$BUF" generate --template buf.quota-lab.gen.yaml)
(cd app/admin/service && go run entgo.io/ent/cmd/ent@v0.14.6 generate \
  --feature privacy --feature entql --feature sql/modifier --feature sql/upsert --feature sql/lock \
  ./internal/data/ent/schema)
(cd app/admin/service && go run ./cmd/schema > schema.sql)

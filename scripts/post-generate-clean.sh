#!/usr/bin/env bash
#
# 生成后清理：抹平 `make api` / `make openapi` 必然产生的非本次改动噪声。
#
# 背景（2026-09-21 实测）：
#   api/gen/go/ 下的生成物是从上游 fork 继承的，其原始生成工具链**未被记录**，
#   而本仓存在两套生成路径：
#     - api/buf.gen.yaml          全量生成，protoc-gen-go-grpc 用 PATH 上的版本
#     - api/buf.network.gen.yaml  Network 切片，显式钉 protoc-gen-go-grpc@v1.6.0
#     - api/buf.model.gen.yaml    Model 切片（model 接入已暂摘，暂不可用）
#   因此全量生成会覆盖切片产出，产生与本次改动无关的差异。
#
# 两类噪声（都无功能影响，但会污染 review）：
#   1) 版本/命名漂移：i_network_grpc 的版本头，
#      以及 i_tenant / i_user / i_task / i_server_monitor 的 http handler
#      序号（protoc-gen-go-http 按方法名全局计数，输入集变化即漂移）。
#      → 直接还原为 HEAD 版本，保持与既有生成物一致。
#   2) 空壳 validate：i_network / catalog 的 *.pb.validate.go
#      在仓库中原本不存在，且对应 proto 的 validate.rules 数为 0，
#      生成内容只有 imports 兜底、Validate() 恒返回 nil。
#      → 删除，维持仓库既有文件集合。
#
# 用法（仓库根目录，或传入仓库根路径）：
#   bash scripts/post-generate-clean.sh [repo_root]
#
# 注意：本脚本只处理"必然噪声"。若你确实有意改动 Network 切片，
# 请改用 scripts/generate-network-slice.sh，不要依赖本脚本。

set -euo pipefail

REPO="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
cd "$REPO"

GEN=api/gen/go

# 1) 还原版本/命名漂移文件
DRIFT_FILES=(
  "$GEN/admin/service/v1/i_network_grpc.pb.go"
  "$GEN/admin/service/v1/i_server_monitor_http.pb.go"
  "$GEN/admin/service/v1/i_task_http.pb.go"
  "$GEN/admin/service/v1/i_tenant_http.pb.go"
  "$GEN/admin/service/v1/i_user_http.pb.go"
)

restored=0
for f in "${DRIFT_FILES[@]}"; do
  if git ls-files --error-unmatch "$f" >/dev/null 2>&1; then
    if ! git diff --quiet -- "$f"; then
      git checkout -- "$f"
      echo "restored: $f"
      restored=$((restored + 1))
    fi
  fi
done

# 2) 删除空壳 validate 文件（仓库中原本不存在）
NOOP_VALIDATE=(
  "$GEN/admin/service/v1/i_network.pb.validate.go"
  "$GEN/catalog/service/v1/vpc.pb.validate.go"
)

removed=0
for f in "${NOOP_VALIDATE[@]}"; do
  if [ -f "$f" ] && ! git ls-files --error-unmatch "$f" >/dev/null 2>&1; then
    rm -f "$f"
    echo "removed: $f"
    removed=$((removed + 1))
  fi
done

echo "post-generate clean done: restored=$restored removed=$removed"

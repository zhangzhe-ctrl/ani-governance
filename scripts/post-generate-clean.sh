#!/usr/bin/env bash
#
# 已退役：本脚本原先在 `make api` 之后抹平"必然噪声"——
#   1) 对 5 个文件执行 `git checkout --`（版本头/protoc-gen-go-http 序号漂移）；
#   2) 删除 2 个仓库中原本不存在的空壳 `*.pb.validate.go`。
#
# 噪声的成因是当时存在两套互相覆盖的生成入口（主模板用 PATH 上的插件并走 managed go_package，
# 切片模板钉旧版本并带显式 go_package），并不是无法修正的固有属性。T15 已把 `make api` 收敛为
# 委托 `tools/bin/gow api` 的同一条已验收链：先校验插件与输入，再在隔离暂存副本中生成，
# 成功后按受管清单写回；实测两入口从干净状态执行的结果与已提交树逐字节一致。
#
# 继续保留这个会改文件的脚本等于保留一条"生成后再把手改/把产物删掉"的路径，
# 与本轮约束（不得用 git checkout 吞掉工作区修改、不得筛除未批准 validator 凑数）直接冲突，
# 因此改为拒绝执行。需要核对生成结果请直接用：
#   tools/bin/gow api && git status --porcelain api app pkg
set -uo pipefail

cat >&2 <<'MSG'
scripts/post-generate-clean.sh 已退役，不再修改任何文件。

原因：它曾用 `git checkout --` 还原生成漂移、并删除"空壳" *.pb.validate.go，
这两件事在单一已验收生成链（make api → tools/bin/gow api：隔离暂存副本内生成并在写回前完成 OpenAPI 后处理）
下都不应发生；保留它就保留了一条在生成之后改动正式产物或掩盖差异的路径。

要确认生成是否一致，请执行完整链后检查工作区：
  make api
  git status --porcelain api app/admin/service/cmd/server/assets pkg/localdeps
若该命令仍产生差异，请把差异当作缺陷上报，而不是清理它。
MSG
exit 1

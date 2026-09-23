# Network 2026-09-19 实验脚本

此目录保留 Network VPC 读取实验，依赖已完成的 Model 实验及其 `gov-model-20260919-01` namespace。只在 Fedora 的任务私有目录执行。`R` 是 Network 实验目录，`OLD` 是保留的 Model 实验目录；二者各自的 `work/governance` 可以使用当前根目录布局或历史 `backend/` 布局，入口分别按 `go.mod` 定位，不能将 `OLD` 一律改成当前源码路径。

`accept.py`、`forward.sh` 仅修复源码目录定位，保留固定 namespace、证书、验收用例和恢复行为。原 `build-images.sh` 已随 Atlas 拆分删除，网络镜像改用 `scripts/lab/build-images.sh` 的 `ANI_NETWORK_BIN` 选项构建。`validation-commands.md` 是历史执行记录，里面的 `backend/` 是当时的工作目录。目录迁移未重新执行这些验收。

`collect.py` **仅适用于迁移前的历史 Governance 工作树**。它保留 `604d0fab96e423c65f5c042a7f87bad5050cb021` / `e481e968d3cc2f17bc4c6a736c438428519b09a0` 基线，以及登录、授权、租户映射和 Model 源码未改动的断言。迁移后的 `R/work/governance/go.mod` 布局会在集群读取或证据写入前拒绝执行；不能通过替换路径、删除断言或更换基线，将本次目录搬迁记为原实验复验通过。`OLD` helper 的目录定位仍兼容两种布局。

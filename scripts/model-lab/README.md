# Model 2026-09-19 实验脚本

> **已暂停（2026-09-21）**：model 接入已暂摘——`ani-model-service` 依赖、出站
> `ModelClient`、`GET /api/v1/models` 路由、`i_model.proto` 及其生成物均已删除。
> 本目录脚本在重接前**不可运行**（引用已删 proto、已下线接口与已摘的
> `ANI_MODEL_*` 环境变量；`probe.go` 虽带 `//go:build ignore` 不会破坏构建，
> 但 import 的模块已不在 go.mod）。整个目录保留作**重接基线**：重接时按
> [docs/service-integration.md](../../docs/service-integration.md) 重建 proto 与
> 客户端/服务实现，再恢复本目录夹具与本 README 所述流程。
> `scripts/network-lab/accept.py` 仍 import 本目录的 `accept.py` 作为
> 请求/登录/会话基础设施，这一耦合在重接时一并评估。

此目录保留 `gov-model-20260919-01` 实验的部署、验收与证据采集流程。只在 Fedora 的任务私有目录执行；固定 namespace、集群、证书、输入和用例仍属于该次实验，不是通用部署入口。`validation-commands.md` 是历史执行记录，目录迁移不代表重新验收通过。

脚本的 `R` 参数指向实验目录，Governance 源码放在 `R/work/governance`。入口按 `go.mod` 分别识别当前仓库根目录和历史 `backend/` 目录，读取该源码快照中的脚本、配置和 OpenAPI。当前源码布局下从仓库根目录运行，例如 `python3 scripts/model-lab/accept.py "$R" contract`；历史快照的脚本仍在 `backend/scripts/model-lab/`。

`collect.py` 保留原用例、生成一致性和文件齐备要求。当前布局只采集显式列出的后端源码及构建文件，不递归采集整个仓库。运行目录没有完整原始证据时不得使用此脚本声称通过；源码路径兼容不等于当前版本的运行验收。

# 独占开发镜像构建

`build-images.sh` 构建 Governance 服务、admin 和 Atlas 镜像，并可导入明确指定的 Kind 集群。先确认源码、Docker 构建上下文、任务独占产物目录与镜像标签，再运行。

设置 `ANI_LAB_RUN` 为任务目录，设置 `ANI_ATLAS_BIN` 为经核验的 Atlas v1.3.0；导入时还需 `KIND_CLUSTER`。执行 `bash scripts/dev/lab/build-images.sh --no-import` 仅构建；省略 `--no-import` 会导入指定集群。可选 `ANI_NETWORK_BIN` 构建额外 Network 镜像。脚本不自动下载 Atlas，不部署工作负载，不清理旧镜像。

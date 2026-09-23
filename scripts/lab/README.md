# 隔离实验镜像构建

构建实验用镜像并导入 kind 节点。与早期"一个 busybox 镜像塞进 server + admin + Atlas + migrations"的做法不同，现在按职责分成三个镜像：

| 镜像 | 来源 | 内容 | 用途 |
|---|---|---|---|
| `ani-governance:lab-<commit>` | 根 Dockerfile `--target runtime-server` | distroless + `/app/bin/server` + `/app/configs` | 服务 Deployment |
| `ani-governance-admin:lab-<commit>` | 根 Dockerfile `--target runtime-admin` | distroless + `/app/bin/admin` | 一次性 `init` / `check` / `sync-apis` Job |
| `ani-atlas:v1.3.0-<migrations摘要>` | `scripts/deploy/atlas/build-image.sh` | busybox + `/bin/atlas` + `/app/migrations` | 一次性结构迁移 Job |

可选的 network 镜像（`ANI_NETWORK_BIN`）沿用 busybox + `/app/server` 的形态，由调用方提供二进制。

## 前置

- 本仓源码可构建（Dockerfile 的 builder 阶段需要拉取依赖，默认代理见 Dockerfile 的 `ARG GOPROXY`）。
- kind 集群存在（默认 `kind-test`，可用 `KIND_CLUSTER` 覆盖）。
- Atlas 二进制：构建 Atlas 镜像需要 `v1.3.0`（与 `go.mod` 的 `arigaio/atlas` 一致），用 `ANI_ATLAS_BIN` 指定或放在 `/home/ubuntu/.local/share/ani-network-service/bin/atlas-v1.3.0`。**脚本不自动下载**，找不到就退出。

## 命令

```bash
# 构建三镜像并导入 kind，产物写 ANI_LAB_RUN（默认 ./build/lab）
bash scripts/lab/build-images.sh

# 只构建不导入
bash scripts/lab/build-images.sh --no-import

# 额外构建 network 镜像
ANI_NETWORK_BIN=/path/to/network bash scripts/lab/build-images.sh
```

产物：

- `$ANI_LAB_RUN/images.json`：`{"governance": ..., "governance-admin": ..., "atlas": ...[, "network": ...]}`，供 `scripts/aksk-lab/lab.py deploy` 读取。
- `$ANI_LAB_RUN/evidence/built-images.txt`：镜像 tag 与 imageID。
- `$ANI_LAB_RUN/application-images.tar`：导入 kind 用的归档。

## 注意

- 标签带 git 提交（工作区有改动时加 `-dirty`），保证运行中的镜像能追溯到源码。
- 运行时镜像是 distroless：**没有 shell**，任何"一个 shell 串跑多步"的 Job 都不成立；迁移与初始化按 `initContainers` 串行编排，见 `scripts/aksk-lab/lab.py` 的 `deploy`。
- 这些镜像只用于隔离实验，不会修改已有应用 Deployment；实验结束用常规 `kubectl delete ns <namespace>` 回收。

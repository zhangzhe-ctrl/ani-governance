# Atlas 拆分与实验脚本重建（ATLAS-SPLIT-01）

2026-09-23，承接 IMAGE-SLIM-01。用户决定：产出实验镜像的旧脚本是实验脚本，可以删除；Atlas 从应用镜像里拆出去；重写一套实验脚本；本任务无需逐项确认，直接计划 → 执行 → 测试 → 提交推送。

## 0. 现状与依据（本机实测）

- kind 的 `ani-system` 命名空间里 `deploy/ani-governance` 运行 `ani-governance:anisystem-43033829cffe5da4` **554 MB**，分层：`COPY server admin /app/` 271 MB、`COPY atlas /bin/atlas` **126 MB**、`COPY migrations` 434 kB、busybox 4.55 MB。
- 该 Pod **没有 initContainer**，容器命令只有 `-c /app/configs` → **运行期不调用 Atlas**，126 MB 纯属打包冗余。
- Atlas 之所以被塞进应用镜像，是因为旧实验脚本 `scripts/aksk-lab/build-images.sh:9-19` 用 busybox 的 `/bin/sh` 在一个 Job 里连跑 `atlas migrate` + `admin init` + `admin check`（`scripts/aksk-lab/lab.py:210-211`）。
- IMAGE-SLIM-01 之后的应用镜像是 distroless（无 shell），**不可能**再沿用"一个 shell 串跑完"的做法，必须拆镜像 + 拆 Job。
- Atlas 二进制来源：`/home/ubuntu/.local/share/ani-network-service/bin/atlas-v1.3.0`（125,569,080 B，带同名 `.sha256`），与 `go.mod` 的 `arigaio/atlas v1.3.0` 一致；`migrations/` 目录 4 个文件（3 个迁移 + `atlas.sum`）。

## 1. 阶段 1：新增独立 Atlas 镜像资产

- [x] **1.1** 新增 `scripts/deploy/atlas/Dockerfile`：`FROM busybox:1.37`；`COPY atlas /bin/atlas`；`COPY migrations /app/migrations/`；`WORKDIR /app`；`ENTRYPOINT ["/bin/atlas"]`。
  - 用 busybox 而非 distroless：Atlas 需要读 `atlas.sum`/迁移文件与可能的临时目录，busybox 体积小（4.55 MB）且保留排查用的 `sh`；它只在一次性迁移 Job 中使用，不承载服务流量。
- [x] **1.2** 新增 `scripts/deploy/atlas/build-image.sh`：
  - Atlas 二进制定位顺序：`ANI_ATLAS_BIN` → `PATH` 里的 `atlas` → `/home/ubuntu/.local/share/ani-network-service/bin/atlas-v1.3.0`；找不到直接报错退出，不下载。
  - 校验 `atlas version` 含 `v1.3.0`（与 `go.mod` 一致），否则报错。
  - 镜像 tag：`ani-atlas:v1.3.0-<migrations sha256 前 12 位>`；同时打 `ani-atlas:latest` 之外的稳定标签由调用方指定。
  - 输出镜像名到 `images.json` 片段由调用方（实验脚本）合并。
- [x] **1.3** 确认 `.dockerignore` 未排除 `migrations/`（当前未排除；不要为了瘦身把它排除，Atlas 镜像构建上下文需要它）。
- [x] **1.4** 记录：Atlas 二进制不在仓库内，镜像只能在有该二进制的构建环境构建；脚本要给出明确报错而不是静默产出空镜像。

## 2. 阶段 2：删除旧实验镜像脚本

- [x] **2.1** 删除 `scripts/aksk-lab/build-images.sh`（554 MB 镜像的直接来源，`busybox + server + admin + atlas + migrations`）。
- [x] **2.2** 删除 `scripts/network-lab/build-images.sh`（同类实验镜像脚本；`scripts/network-lab/README.md:5` 已说明该目录是历史隔离实验、未随目录迁移重跑）。
- [x] **2.3** 保留 `lab.py` / `collect.py` / `README.md` 等验收逻辑本体，只改引用（见阶段 3、4）。

## 3. 阶段 3：新实验脚本

- [x] **3.1** 新增 `scripts/lab/build-images.sh`（替换被删的两个脚本）：
  - 从 Governance 仓库根构建 `runtime-server` → `ani-governance:<binhash>`；`runtime-admin` → `ani-governance-admin:<binhash>`。
  - 调用 `scripts/deploy/atlas/build-image.sh` 产出 `ani-atlas:<version>-<mighash>`。
  - 可选 `--with-network <network-bin>`：由调用方提供的 network 二进制打 `ani-network-service:<binhash>`（沿用旧脚本能力，不依赖另一仓源码）。
  - 写 `images.json`：`{"governance": ..., "governance-admin": ..., "atlas": ...[, "network": ...]}`。
  - `docker save` + 导入当前 kind 节点（沿用旧脚本的导入方式），并记录 imageID 到 `evidence/built-images.txt`。
  - `set -euo pipefail`；失败即退出并提示。
- [x] **3.2** 新增 `scripts/lab/README.md`：说明前置（kind、已构建二进制、Atlas 二进制）、命令顺序、产物与恢复方式（不改动既有 Deployment）。

## 4. 阶段 4：lab.py / collect.py 适配

- [x] **4.1** `scripts/aksk-lab/lab.py:203-214` 的 `governance-init` Job 拆分（distroless/busybox 镜像不再有可承载整串 shell 的 governance 镜像）：
  - `governance-migrate` Job（image `images['atlas']`）：用 **串行 initContainers** 依次跑 `atlas migrate status --dir file://migrations --url $ANI_DATABASE_DSN`、`--dry-run apply`、`apply`，最后 `restartPolicy: Never`、`backoffLimit: 0`。
  - `governance-admin` Job（image `images['governance-admin']`）：initContainer 跑 `/app/bin/admin init --username admin --password-file /run/secrets/admin-password`，主容器跑 `/app/bin/admin check`（两者都只连 PostgreSQL）。
  - 依赖顺序：migrate Job `complete` → admin Job `complete` → 授权 SQL → network 迁移（保持现有顺序，`lab.py:216-222`）。
- [x] **4.2** `lab.py:226` governance Deployment 命令：`/app/server` → `/app/bin/server`（distroless 镜像内路径）。
- [x] **4.3** `scripts/aksk-lab/collect.py:53`：`sha256sum /app/server` → `/app/bin/server`。
- [x] **4.4** `collect.py:57-59`：Pod 内已不存在 `/app/admin`；改为把 admin 镜像的二进制摘要记录到 runtime 证据（用构建产物 `bin/admin` 的 sha256 与 admin 镜像内 `/app/bin/admin` 的一致性由构建脚本保证，证据里记 `images.json` 的 tag 与构建摘要）。
- [x] **4.5** `python3 -m py_compile` 语法校验两个文件。

## 5. 阶段 5：文档同步

- [x] **5.1** `docs/deployment.md` §2（Atlas 迁移）：补充独立 Atlas 镜像的构建与 Job 用法，并明确**服务镜像不含 Atlas、不含 shell**；宿主机 `scripts/atlas.sh` 的用法保持不变。
- [x] **5.2** `scripts/aksk-lab/README.md:26-32`：命令里的 `bash scripts/aksk-lab/build-images.sh` 换成新脚本路径。
- [x] **5.3** `scripts/README.md:18`：实验入口表补 `lab/README.md`，去掉已删除脚本的指向。
- [x] **5.4** `docs/image-size-reduction-plan.md:129`、`:141`、`:180`、`:200`：把对 `scripts/aksk-lab/build-images.sh` 的引用改成新脚本/Atlas 镜像，避免文档指向已删除文件。
- [x] **5.5** `docs/evidence/aksk-vpc-20260922/README.md`：**不改**（历史证据，保留当次实际执行的命令）。

## 6. 阶段 6：验收

本机可验证：

- [x] **6.1** `bash -n` 每个新增/修改的 shell 脚本；`python3 -m py_compile scripts/aksl-lab/*.py`（注意拼写：实际目录 `scripts/aksk-lab`）。
- [x] **6.2** 构建 Atlas 镜像成功；`docker run --rm <atlas-image> /bin/atlas version` 输出 v1.3.0；镜像内 `/app/migrations` 含 3 个迁移与 `atlas.sum`。
- [x] **6.3** 新实验脚本在"仅 governance + atlas"模式下跑通到产出 `images.json` 与 kind 导入（network 部分跳过）。
- [x] **6.4** 只读链路验证：`kubectl port-forward` kind 内 postgres 后，`docker run --rm --network host <atlas-image> migrate status --dir file://migrations --url <DSN>` 返回，确认 Atlas 镜像能连库读到迁移目录（**只跑 status，不 apply**）。
- [x] **6.5** admin 镜像 `docker run --rm <admin-image>` 仍输出 usage；server 镜像启动仍按预期因缺少 `ANI_ACCESS_KEY_ENCRYPTION_KEY_FILE` 报错（IMAGE-SLIM-01 已验证，回归确认）。

本机不可验证（`not_verified`，附命令）：

- [ ] **6.6** kind 内完整实验闭环：`governance-migrate` → `governance-admin` → network 迁移 → deployment → AK/SK 验收用例（需要 network 仓二进制与完整 lab 环境）。
- [ ] **6.7** 真实部署改用三镜像后 `ani-system` 的滚动更新与回滚。

## 7. 风险与回退

| # | 风险 | 处置 |
|---|---|---|
| R1 | 删掉 build-images.sh 后旧证据复现命令失效 | 历史证据文件不动；新脚本路径写入 `scripts/aksk-lab/README.md` 与 `scripts/lab/README.md` |
| R2 | Atlas 二进制不在仓库，换机器无法构建 | 脚本显式报错并打印所需路径与版本；不自动下载 |
| R3 | lab.py 的 Job 拆分改动量大（37 KB 文件） | 只改 deploy 阶段的 Job 与路径；其余阶段不动，用 py_compile + 结构审查兜底 |
| R4 | 迁移 Job 与 admin Job 串行依赖 | 用 `kubectl wait --for=condition=complete` 显式等待，保持原顺序 |
| R5 | Atlas 镜像 tag 与迁移版本漂移 | tag 带 atlas 版本 + migrations 摘要，摘要变化即换镜像 |

## 8. 执行状态

| 阶段 | 状态 |
|---|---|
| 1 Atlas 镜像资产 | **已完成**：`ani-atlas:v1.3.0-d5e329c5169f`，`docker run` 输出 `atlas version v1.3.0` |
| 2 删除旧实验脚本 | **已完成**：`git rm scripts/aksk-lab/build-images.sh scripts/network-lab/build-images.sh` |
| 3 新实验脚本 | **已完成**：`scripts/lab/build-images.sh`，实测产出三镜像并导入 kind 节点（节点内可见 `ani-atlas` 37.6 MiB / `ani-governance-admin` 8.5 MiB） |
| 4 lab.py/collect.py | **已完成**：migrate/admin 两个 Job（initContainers 串行）、`/app/bin/server`、`diagnose` 目标更新；`py_compile` 通过 |
| 5 文档 | **已完成**：deployment.md、scripts/README.md、两个 lab README、image-size-reduction-plan.md 引用 |
| 6 验收 | 6.1-6.5 **已完成**；6.6/6.7 `not_verified` |

## 9. 验收实测（2026-09-23）

- Atlas 镜像连真实库只读验证：`kubectl port-forward` 到 kind 内 postgres 后
  `docker run --rm --network host ani-atlas:v1.3.0-d5e329c5169f migrate status --dir file://migrations --url <DSN>`
  输出 `Migration Status: PENDING / Executed Files: 0 / Pending Files: 3` → 镜像能连库、能读到 3 个迁移与 `atlas.sum`。**只跑 status，不 apply。**
- 实验镜像冒烟：`ani-governance-admin:lab-*` 输出 usage；`ani-governance:lab-*` 启动加载 5 个配置并按预期要求 `ANI_ACCESS_KEY_ENCRYPTION_KEY_FILE`。
- 镜像体积：`ani-governance:lab-*` 165 MB、`ani-governance-admin:lab-*` 38.7 MB、`ani-atlas:v1.3.0-*` 170 MB（仅迁移 Job 使用，不进服务 Deployment）。

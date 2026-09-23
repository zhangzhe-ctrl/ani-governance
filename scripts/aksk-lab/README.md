# AK/SK VPC 隔离验收

仅在本任务授权的 `ssh ubuntu` 执行，使用 `kind-kind-test` context，任务目录默认 `/home/ubuntu/workspace/aksk-vpc-20260922`、独立 namespace `aksk-vpc-20260922`、NodePort `30188`。脚本不清空共享资源。首次 `prepare` 遇到已有 namespace 会拒绝覆盖；失败后先检查任务自身状态，再从对应阶段继续。

## 制品与环境

将两仓本批源码分别同步到任务目录的 `governance`、`network`；本地只编辑、审查和提交，不运行生成或测试。记录源码基线及 SHA256 清单。ubuntu 使用 Go 1.26.7、gow v1.0.3、Buf 1.60.0、Atlas 1.3.0；定向生成入口为 `scripts/generate-aksk-slice.sh`，不用 Wire 或 Model 生成脚本。

远端构建示例（从 Governance 根目录执行，共用任务锁，限制并发）：

```bash
source /home/ubuntu/.local/share/ani-network-service/env.sh
export PATH=/home/ubuntu/go/bin:$PATH
export GOMAXPROCS=2 GOFLAGS=-p=2
export ANI_AKSK_LAB_RUN=/home/ubuntu/workspace/aksk-vpc-20260922
export BUF=/home/ubuntu/.local/share/ani-network-service/bin/buf
bash scripts/generate-aksk-slice.sh
CGO_ENABLED=0 go build -o "$ANI_AKSK_LAB_RUN/bin/governance" ./app/admin/service/cmd/server
CGO_ENABLED=0 go build -o "$ANI_AKSK_LAB_RUN/bin/admin" ./app/admin/service/cmd/admin
```

Network 从自己的根目录编译 `./cmd/ani-network-service` 到 `bin/network`，编译 `./scripts/vpc-read-probe` 到 `bin/network-probe`。网络查询制品使用 `ANI_NETWORK_MODE=vpc-read`，没有 Kubernetes 写权限，不创建云网络。

## 空库到真实查询

下面命令全部在 ubuntu 的 Governance 源码目录执行。先核对 kind 节点已装载预期 PostgreSQL 18、Redis 7 和 BusyBox 1.37 镜像，最终记录实际 imageID。`scripts/lab/build-images.sh` 构建**三个**镜像（服务、运维 CLI、Atlas）并按 git 提交打标签、导入当前 kind 节点；不会改已有应用 Deployment。Atlas 二进制不在仓库内，脚本找不到就报错退出。

```bash
python3 scripts/aksk_vpc_client.py --self-test
python3 scripts/aksk-lab/lab.py prepare
bash scripts/lab/build-images.sh
python3 scripts/aksk-lab/lab.py deploy
python3 scripts/aksk-lab/lab.py setup
python3 scripts/aksk-lab/lab.py contract
python3 scripts/aksk-lab/lab.py mtls
python3 scripts/aksk-lab/lab.py final_checks
python3 scripts/aksk-lab/lab.py security_checks
python3 scripts/aksk-lab/collect.py
```

`deploy` 先创建 `governance-migrate` Job（Atlas 镜像：status → dry-run → apply 串行），再创建 `governance-admin` Job（运维镜像：init → check 串行），之后才启动服务；运行时镜像是 distroless，没有 shell，每一步都是独立容器。服务使用无 DDL 权限的 Governance 运行角色；Network 使用只读运行角色。PostgreSQL 使用本任务 PVC，Redis 是独立实例。`setup` 经真实 HTTP 创建专用套餐（DASHBOARD/OPM/SYSTEM/NETWORK）、租户及管理员和租户角色，调用既有 `bootstrap-network-access.sql` 配置 `network:vpc:get` 后重载策略；VPC 是明确的数据库 fixture，不代表数据面验收。

`contract` 经租户管理员 JWT 调用 `POST /api/v1/auth/api-keys`，检查真实 HTTP 201，再由标准库 Python 客户端签名查询持久化 VPC。覆盖协议篡改、身份、套餐、跨租户管理/资源、FieldMask、生命周期和用户 JWT 回归。`mtls` 直接探测同一个 Network Pod 的 TLS 边界；`final_checks` 检查重启、密文、日志和审计。

所有随机密码、主密钥、测试 SK/JWT 和证书私钥在远端 `private/`（0700）和文件（0600）或 Kubernetes Secret，不能复制到版本库，也不要 `cat` 到共享输出。验收结果仅输出 case/status/HTTP 等非敏感字段，位于 `evidence/acceptance.jsonl`；失败详细命令输出存于 `private/last-command-error.txt`。不得把单个通过结果当成所有阶段完成。

## 复用已经创建的 Key 运行客户端

完成 `contract` 后，可在 ubuntu 执行下面命令；凭据直接由受限状态文件传给客户端，不打印或放进命令行参数：

```bash
python3 - <<'PY'
import json, os, subprocess
from pathlib import Path
root = Path('/home/ubuntu/workspace/aksk-vpc-20260922')
s = json.loads((root/'private/state.json').read_text())
env = dict(os.environ, ANI_BASE_URL=s['base'], ANI_ACCESS_KEY=s['key']['ak'],
           ANI_SECRET_KEY=s['key']['sk'], ANI_VPC_ID=s['tenants']['a']['vpc'],
           ANI_ALLOW_HTTP_FOR_TEST='1')
subprocess.run(['python3', str(root/'governance/scripts/aksk_vpc_client.py')], env=env, check=True)
PY
```

一般部署使用 HTTPS origin、租户管理员创建的 `data.access_key/secret_key` 和真实 VPC ID；不设置 HTTP 测试开关。自签 CA 用 `SSL_CERT_FILE`，不关闭证书校验，不跟随重定向。完整管理报文及 FieldMask 例外见 [执行文档](../../docs/aksk-vpc-execution-plan.md)。

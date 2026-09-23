#!/usr/bin/env bash
# 隔离实验镜像：服务镜像 / 运维 CLI 镜像 / Atlas 迁移镜像（可选 network 镜像）。
#
# 与已删除的 scripts/aksk-lab/build-images.sh 的区别：
#   - 不再把 server、admin、atlas、migrations 塞进同一个 busybox 镜像；
#   - server/admin 由根 Dockerfile 的 runtime-server / runtime-admin 目标构建（distroless，无 shell）；
#   - Atlas 由 scripts/deploy/atlas/build-image.sh 单独构建，只在迁移 Job 中使用。
#
# 用法：
#   bash scripts/lab/build-images.sh                 # 构建三镜像并导入 kind
#   bash scripts/lab/build-images.sh --no-import     # 只构建，不导入 kind
#   ANI_NETWORK_BIN=/path/to/network bash scripts/lab/build-images.sh
#
# 环境变量：
#   ANI_LAB_RUN       产物目录（默认 ./build/lab），写 images.json 与 evidence/built-images.txt
#   ANI_NETWORK_BIN   network 服务二进制；给出时额外构建 ani-network-service 镜像
#   KIND_CLUSTER      kind 集群名（默认 kind-test）
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "${SCRIPT_DIR}/../.." && pwd)
cd "${REPO_ROOT}"

RUN_DIR="${ANI_LAB_RUN:-${REPO_ROOT}/build/lab}"
KIND_CLUSTER="${KIND_CLUSTER:-kind-test}"
IMPORT=1
for arg in "$@"; do
  case "${arg}" in
    --no-import) IMPORT=0 ;;
    *) printf 'unknown argument: %s\n' "${arg}" >&2; exit 1 ;;
  esac
done

err() { printf '%s\n' "$*" >&2; exit 1; }

# 标签带源码提交，保证"跑在 Pod 里的镜像"能追溯到源码。
commit="$(git rev-parse --short=12 HEAD 2>/dev/null || echo nogit)"
if ! git diff --quiet 2>/dev/null || ! git diff --quiet --cached 2>/dev/null; then
  commit="${commit}-dirty"
fi

mkdir -p "${RUN_DIR}/evidence"

printf 'Info: building governance server image\n'
gov_image="ani-governance:lab-${commit}"
docker build --quiet --target runtime-server \
  --build-arg SERVICE_NAME=admin \
  --build-arg APP_VERSION="${commit}" \
  -t "${gov_image}" -f Dockerfile . >/dev/null

printf 'Info: building governance admin CLI image\n'
admin_image="ani-governance-admin:lab-${commit}"
docker build --quiet --target runtime-admin \
  --build-arg SERVICE_NAME=admin \
  --build-arg APP_VERSION="${commit}" \
  -t "${admin_image}" -f Dockerfile . >/dev/null

printf 'Info: building atlas migration image\n'
atlas_image="$(bash "${SCRIPT_DIR}/../deploy/atlas/build-image.sh" | tail -1)"

images="{\"governance\":\"${gov_image}\",\"governance-admin\":\"${admin_image}\",\"atlas\":\"${atlas_image}\""

if [[ -n "${ANI_NETWORK_BIN:-}" ]]; then
  [[ -f "${ANI_NETWORK_BIN}" ]] || err "ANI_NETWORK_BIN not a file: ${ANI_NETWORK_BIN}"
  printf 'Info: building network image from %s\n' "${ANI_NETWORK_BIN}"
  net_hash="$(sha256sum "${ANI_NETWORK_BIN}" | cut -c1-16)"
  net_image="ani-network-service:lab-${net_hash}"
  net_dir="$(mktemp -d)"
  trap 'rm -rf "${net_dir}"' EXIT
  cp "${ANI_NETWORK_BIN}" "${net_dir}/server"
  chmod +x "${net_dir}/server"
  printf 'FROM busybox:1.37\nCOPY server /app/server\nUSER 65532:65532\nENTRYPOINT ["/app/server"]\n' > "${net_dir}/Dockerfile"
  docker build --quiet --pull=false --network=none -t "${net_image}" "${net_dir}" >/dev/null
  images="${images},\"network\":\"${net_image}\""
fi

images="${images}}"
printf '%s\n' "${images}" > "${RUN_DIR}/images.json"

if [[ "${IMPORT}" -eq 1 ]]; then
  nodes=""
  if command -v kind >/dev/null 2>&1; then
    nodes="$(kind get nodes --name "${KIND_CLUSTER}" 2>/dev/null || true)"
  else
    # kind CLI 可选；集群节点仍是同名容器，直接用 docker 定位。
    nodes="$(docker ps --filter "name=${KIND_CLUSTER}-" --format '{{.Names}}' 2>/dev/null || true)"
  fi
  if [[ -n "${nodes}" ]]; then
    printf 'Info: importing images into kind nodes\n'
    docker save ${gov_image} ${admin_image} ${atlas_image} ${net_image:-} -o "${RUN_DIR}/application-images.tar"
    while IFS= read -r node; do
      [[ -n "${node}" ]] && docker exec -i "${node}" ctr -n k8s.io images import - < "${RUN_DIR}/application-images.tar"
    done <<< "${nodes}"
  else
    err "no nodes found for cluster ${KIND_CLUSTER}; rerun with --no-import or set KIND_CLUSTER"
  fi
fi

docker image inspect ${gov_image} ${admin_image} ${atlas_image} ${net_image:-} \
  --format '{{.RepoTags}} {{.Id}}' > "${RUN_DIR}/evidence/built-images.txt"

printf 'Images written to %s/images.json:\n%s\n' "${RUN_DIR}" "${images}"

#!/usr/bin/env bash
# 构建独立的 Atlas 迁移镜像（不把 Atlas 塞进应用镜像）。
#
# Atlas 二进制不在本仓库内，脚本只做定位与校验，不下载：
#   ANI_ATLAS_BIN=<path> 或 PATH 里有 atlas，或放在
#   /home/ubuntu/.local/share/ani-network-service/bin/atlas-<version>。
# 版本必须与 go.mod 的 arigaio/atlas 一致（当前 v1.3.0）。
# 镜像标签带 Atlas 版本与 migrations 摘要，迁移内容变化即换镜像。
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "${SCRIPT_DIR}/../../.." && pwd)
cd "${REPO_ROOT}"

ATLAS_VERSION="v1.3.0"
ATLAS_IMAGE_REPO="${ATLAS_IMAGE_REPO:-ani-atlas}"

err() { printf '%s\n' "$*" >&2; exit 1; }

atlas_bin="${ANI_ATLAS_BIN:-}"
if [[ -z "${atlas_bin}" ]]; then
  if command -v atlas >/dev/null 2>&1; then
    atlas_bin="$(command -v atlas)"
  else
    candidate="/home/ubuntu/.local/share/ani-network-service/bin/atlas-${ATLAS_VERSION}"
    [[ -x "${candidate}" ]] && atlas_bin="${candidate}"
  fi
fi
[[ -n "${atlas_bin}" ]] || err "atlas binary not found; set ANI_ATLAS_BIN or install atlas ${ATLAS_VERSION} on PATH"
[[ -x "${atlas_bin}" ]] || err "atlas binary is not executable: ${atlas_bin}"

actual_version="$("${atlas_bin}" version 2>/dev/null | head -1 | awk '{print $NF}')"
[[ "${actual_version}" == "${ATLAS_VERSION}" ]] || err "atlas version mismatch: want ${ATLAS_VERSION}, got '${actual_version}' (${atlas_bin})"

[[ -d migrations ]] || err "migrations directory not found at ${REPO_ROOT}/migrations"

migrations_digest="$(find migrations -type f | LC_ALL=C sort | xargs sha256sum | sha256sum | cut -c1-12)"
image="${ATLAS_IMAGE_REPO}:${ATLAS_VERSION}-${migrations_digest}"

build_dir="$(mktemp -d)"
trap 'rm -rf "${build_dir}"' EXIT
cp "${atlas_bin}" "${build_dir}/atlas"
chmod +x "${build_dir}/atlas"
cp -R migrations "${build_dir}/migrations"

docker build --quiet --pull=false --network=none -t "${image}" \
  -f "${SCRIPT_DIR}/Dockerfile" "${build_dir}" >/dev/null

printf '%s\n' "${image}"

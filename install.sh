#!/usr/bin/env bash
# Local prebuilt installer. No Go build is needed on the VPS.
set -euo pipefail
if [[ $EUID -ne 0 ]]; then exec sudo -- bash "$0" "$@"; fi
if ! command -v python3 >/dev/null 2>&1; then
  echo '检测到缺少 python3，自动安装…'
  if command -v apt-get >/dev/null 2>&1; then apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends python3 ca-certificates
  elif command -v dnf >/dev/null 2>&1; then dnf install -y python3 ca-certificates
  elif command -v yum >/dev/null 2>&1; then yum install -y python3 ca-certificates
  elif command -v apk >/dev/null 2>&1; then apk add --no-cache python3 ca-certificates
  elif command -v pacman >/dev/null 2>&1; then pacman -Sy --noconfirm --needed python ca-certificates
  elif command -v zypper >/dev/null 2>&1; then zypper --non-interactive install -y python3 ca-certificates
  else echo '未找到包管理器，需先安装 python3'; exit 1; fi
fi
MIKU_PACKAGE_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
if [[ "${1:-}" == --docker ]]; then
  shift
  MIKU_DOCKER_PROJECT_DIR="${MIKU_DOCKER_INSTALL_DIR:-/opt/miku-gate-docker}"
  python3 -B "${MIKU_PACKAGE_DIR}/docker/stage.py" "${MIKU_PACKAGE_DIR}" "${MIKU_DOCKER_PROJECT_DIR}"
  exec bash "${MIKU_DOCKER_PROJECT_DIR}/install-docker.sh" "$@"
fi
exec python3 -B "${MIKU_PACKAGE_DIR}/scripts/install.py" --directory "${MIKU_PACKAGE_DIR}" "$@"

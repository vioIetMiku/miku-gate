#!/usr/bin/env bash
# Local prebuilt installer. No Go build is needed on the VPS.
set -euo pipefail
if [[ $EUID -ne 0 ]]; then exec sudo -- bash "$0" "$@"; fi
if ! command -v python3 >/dev/null 2>&1; then
  python_log=$(mktemp "${TMPDIR:-/tmp}/mikugate-python.XXXXXXXX")
  chmod 600 "$python_log"
  echo '正在准备 Python 运行环境，请稍候…'
  if ! (
  echo '检测到缺少 python3，自动安装…'
  if command -v apt-get >/dev/null 2>&1; then apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends python3 ca-certificates
  elif command -v dnf >/dev/null 2>&1; then dnf install -y python3 ca-certificates
  elif command -v yum >/dev/null 2>&1; then yum install -y python3 ca-certificates
  elif command -v apk >/dev/null 2>&1; then apk add --no-cache python3 ca-certificates
  elif command -v pacman >/dev/null 2>&1; then pacman -Sy --noconfirm --needed python ca-certificates
  elif command -v zypper >/dev/null 2>&1; then zypper --non-interactive install -y python3 ca-certificates
  else echo '未找到包管理器，需先安装 python3'; exit 1; fi
  ) >"$python_log" 2>&1; then
    echo "Python 环境安装失败，日志：$python_log" >&2
    tail -n 15 "$python_log" >&2
    exit 1
  fi
  rm -f -- "$python_log"
fi
MIKU_PACKAGE_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
source "$MIKU_PACKAGE_DIR/scripts/banner.sh"
if [[ "${1:-}" != --docker ]]; then
  existing_docker=$(python3 -B "$MIKU_PACKAGE_DIR/scripts/install_mode.py")
  if [[ -n "$existing_docker" ]]; then
    if [[ $# -gt 0 ]]; then
      echo '检测到 Docker 安装。更新请使用原命令加 --docker；保留数据卸载请用 miku uninstall。Docker 与原生数据目录不同，切换部署方式需要另行迁移。' >&2
      exit 2
    fi
    echo '检测到已有 Docker 安装，将保留数据并更新 Docker 程序。'
    export MIKU_DOCKER_INSTALL_DIR="$existing_docker"
    set -- --docker
  fi
fi
if [[ "${1:-}" == --docker ]]; then
  shift
  python3 -B "$MIKU_PACKAGE_DIR/scripts/progress.py" '正在准备 Docker 运行环境' bash "$MIKU_PACKAGE_DIR/scripts/setup-docker.sh"
  MIKU_DOCKER_PROJECT_DIR="${MIKU_DOCKER_INSTALL_DIR:-/opt/miku-gate-docker}"
  # Validate options before staging files into an existing deployment.
  if [[ "${1:-}" == --split ]]; then
    shift
    [[ $# -eq 0 ]] || { echo '--split 不接受其他参数' >&2; exit 2; }
  elif [[ "${1:-}" == --backend-only ]]; then
    shift
    set -- backend "${@}"
  fi
  python3 -B "${MIKU_PACKAGE_DIR}/docker/stage.py" "${MIKU_PACKAGE_DIR}" "${MIKU_DOCKER_PROJECT_DIR}"
  exec bash "${MIKU_DOCKER_PROJECT_DIR}/install-docker.sh" "$@"
fi
exec python3 -B "${MIKU_PACKAGE_DIR}/scripts/install.py" --directory "${MIKU_PACKAGE_DIR}" "$@"

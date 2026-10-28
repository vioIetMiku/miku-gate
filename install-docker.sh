#!/usr/bin/env bash
# Default deployment: independent frontend and backend Compose projects.
set -euo pipefail
umask 077
MIKU_DOCKER_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
cd -- "$MIKU_DOCKER_DIR"
scope=all
if [[ "${1:-}" == backend || "${1:-}" == --backend-only ]]; then
  scope=backend
  shift
fi
action="${1:-up}"
if (($#)); then shift; fi
case "$action" in
  -h|--help|help)
    echo '用法：bash install-docker.sh [up|build|start|restart|update|info|status|logs|stop|down|backup|quick [参数]|sub|password]'
    echo '独立维护：bash install-docker.sh frontend [start|restart|info|status|logs|stop|down|check]'
    echo '仅操作后端：bash install-docker.sh backend [up|start|restart|update|info|status|logs|stop|down]'
    echo '默认 up：复用或构建后端镜像并启动后端及独立前端，端口冲突自动避让。要求 Linux、Docker Engine、Compose v2、Python 3.10+ 和 TUN。'
    exit 0 ;;
  up|build|info|quick|sub|password|status|logs|start|stop|restart|backup|update|down|frontend) ;;
  *) echo "未知命令：$action" >&2; exit 2 ;;
esac
if [[ "$action" != quick && "$action" != frontend && $# -ne 0 ]]; then echo '此命令不接受额外参数' >&2; exit 2; fi
[[ "$(uname -s)" == Linux ]] || { echo 'Docker 模式需要 Linux VPS。' >&2; exit 1; }
command -v python3 >/dev/null 2>&1 || { echo '请先安装 Python 3.10+。' >&2; exit 1; }
python3 -c 'import sys; sys.exit(0 if sys.version_info >= (3, 10) else "需要 Python 3.10+")'
command -v docker >/dev/null 2>&1 || { echo '请先安装 Docker Engine 和 Compose v2，见 docs/DOCKER.md。' >&2; exit 1; }
docker compose version >/dev/null
if [[ "$action" == frontend ]]; then
  exec python3 -B "$MIKU_DOCKER_DIR/scripts/frontend.py" "$@"
fi
[[ "$(docker info --format '{{.OSType}}')" == linux ]] || { echo '需要可访问的 Linux Docker Engine。' >&2; exit 1; }
if [[ "$(docker info --format '{{json .SecurityOptions}}')" == *rootless* ]]; then
  echo '网络命名空间需要 rootful Docker；不支持 rootless 模式。' >&2; exit 1
fi
if [[ -z "${MIKU_BINARY_SOURCE:-}" && ! -f bin/miku-linux-amd64 ]]; then
  export MIKU_BINARY_SOURCE=source
fi
case "${MIKU_BINARY_SOURCE:-prebuilt}" in prebuilt|source) ;; *) echo 'MIKU_BINARY_SOURCE 只能是 prebuilt 或 source' >&2; exit 2 ;; esac
compose=(docker compose --project-directory "$MIKU_DOCKER_DIR" -f "$MIKU_DOCKER_DIR/compose.yaml")
"${compose[@]}" config --quiet

prepare_image() {
  local force="${1:-0}" cached='' arch=''
  export MIKU_BUILD_KEY=unknown
  if command -v python3 >/dev/null 2>&1 && [[ -f docker/build-key.py ]]; then
    case "$(docker info --format '{{.Architecture}}')" in
      x86_64|amd64) arch=amd64 ;;
      aarch64|arm64) arch=arm64 ;;
      *) echo '仅支持 amd64 / arm64' >&2; return 2 ;;
    esac
    MIKU_BUILD_KEY=$(python3 -B docker/build-key.py "$MIKU_DOCKER_DIR" "$arch" "${MIKU_BINARY_SOURCE:-prebuilt}")
    cached=$(docker image inspect --format '{{index .Config.Labels "io.github.mikugate.build-key"}}' miku-gate:local 2>/dev/null || true)
    if [[ "$force" == 0 && "${MIKU_DOCKER_REFRESH:-0}" != 1 && "$cached" == "$MIKU_BUILD_KEY" ]]; then
      echo '构建输入未变化，复用本地镜像；跳过构建和远程基础镜像检查。'
      return
    fi
  fi
  if [[ "${MIKU_DOCKER_REFRESH:-0}" == 1 ]]; then
    "${compose[@]}" build --pull --no-cache
  else
    "${compose[@]}" build
  fi
}

frontend() { python3 -B "$MIKU_DOCKER_DIR/scripts/frontend.py" "$@"; }
frontend_exists() { [[ -f frontend/.runtime/state.json || -f frontend/.runtime/nginx.conf ]]; }

runtime_check() {
  [[ -c /dev/net/tun ]] || { echo '宿主机缺少 /dev/net/tun；请先在 VPS 面板启用 TUN（可尝试 sudo modprobe tun）。' >&2; exit 1; }
  mkdir -p docker-data docker-certs/etc docker-certs/lib docker-backups
}

show_info() {
  "${compose[@]}" exec -T miku python3 -B /usr/local/lib/miku/container.py healthcheck
  "${compose[@]}" exec -T miku miku info
  if [[ "$scope" == all ]] && frontend_exists; then frontend info; fi
}

start_frontend() {
  if [[ "$scope" == all ]]; then
    if ! frontend start; then
      echo '后端已就绪，前端启动失败。查看 frontend logs，修正后运行 frontend start；可用 backend info 查看后端入口。' >&2
      return 1
    fi
  fi
}

case "$action" in
  up|update)
    runtime_check
    prepare_image
    "${compose[@]}" up -d --no-build --pull never --wait --wait-timeout 180
    start_frontend
    show_info ;;
  build) prepare_image 1 ;;
  start)
    runtime_check
    prepare_image
    "${compose[@]}" up -d --no-build --pull never --wait --wait-timeout 180
    start_frontend
    show_info ;;
  restart)
    runtime_check
    "${compose[@]}" restart miku
    "${compose[@]}" exec -T miku python3 -B -c 'import sys;sys.path.insert(0,"/usr/local/lib/miku");import common;common.wait_ready()'
    start_frontend
    show_info ;;
  info) show_info ;;
  status)
    "${compose[@]}" ps
    if [[ "$scope" == all ]] && frontend_exists; then frontend status; fi ;;
  logs) "${compose[@]}" logs --tail=100 -f miku ;;
  stop|down)
    if [[ "$scope" == all ]] && frontend_exists; then frontend "$action"; fi
    "${compose[@]}" "$action" ;;
  quick) "${compose[@]}" exec miku miku quick "$@" ;;
  sub) "${compose[@]}" exec -T miku miku sub ;;
  password) "${compose[@]}" exec miku miku password ;;
  backup)
    mkdir -p docker-backups
    was_running="$("${compose[@]}" ps --status running -q miku)"
    resume_miku() {
      if [[ -n "$was_running" ]]; then
        "${compose[@]}" start miku
        "${compose[@]}" exec -T miku python3 -B -c 'import sys;sys.path.insert(0,"/usr/local/lib/miku");import common;common.wait_ready()'
        if [[ "$scope" == all ]] && frontend_exists; then frontend start; fi
      fi
    }
    trap resume_miku EXIT
    "${compose[@]}" stop miku
    backup_name="miku-docker-$(date -u +%Y%m%dT%H%M%SZ)-$$.tar.gz"
    "${compose[@]}" run --rm --no-deps --entrypoint python3 miku \
      -B /usr/local/lib/miku/container.py snapshot "$backup_name"
    echo "备份完成：$MIKU_DOCKER_DIR/docker-backups/$backup_name"
    ;;
esac

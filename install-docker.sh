#!/usr/bin/env bash
# Run from an unpacked release bundle or a source checkout. Never installs systemd units.
set -euo pipefail
umask 077
MIKU_DOCKER_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
cd -- "$MIKU_DOCKER_DIR"
action="${1:-up}"
if (($#)); then shift; fi
case "$action" in
  -h|--help|help)
    echo '用法：bash install-docker.sh [up|build|info|quick [参数]|sub|password|status|logs|start|stop|restart|backup|update|down]'
    echo '默认 up：构建镜像并后台启动；要求 Linux、Docker Engine、Compose v2 和 TUN。'
    exit 0 ;;
  up|build|info|quick|sub|password|status|logs|start|stop|restart|backup|update|down) ;;
  *) echo "未知命令：$action" >&2; exit 2 ;;
esac
if [[ "$action" != quick && $# -ne 0 ]]; then echo '此命令不接受额外参数' >&2; exit 2; fi
[[ "$(uname -s)" == Linux ]] || { echo 'Docker 模式需要 Linux VPS。' >&2; exit 1; }
command -v docker >/dev/null 2>&1 || { echo '请先安装 Docker Engine 和 Compose v2，见 docs/DOCKER.md。' >&2; exit 1; }
docker compose version >/dev/null
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

runtime_check() {
  [[ -c /dev/net/tun ]] || { echo '宿主机缺少 /dev/net/tun；请先在 VPS 面板启用 TUN（可尝试 sudo modprobe tun）。' >&2; exit 1; }
  mkdir -p docker-data docker-certs/etc docker-certs/lib docker-backups
}

show_info() {
  "${compose[@]}" exec -T miku python3 -B /usr/local/lib/miku/container.py healthcheck
  "${compose[@]}" exec -T miku miku info
}

case "$action" in
  up|update)
    runtime_check
    "${compose[@]}" build --pull
    "${compose[@]}" up -d --wait --wait-timeout 180
    show_info ;;
  build) "${compose[@]}" build --pull ;;
  start)
    runtime_check
    "${compose[@]}" up -d --wait --wait-timeout 180
    show_info ;;
  restart)
    runtime_check
    "${compose[@]}" restart miku
    "${compose[@]}" exec -T miku python3 -B -c 'import sys;sys.path.insert(0,"/usr/local/lib/miku");import common;common.wait_ready()'
    show_info ;;
  info) show_info ;;
  status) "${compose[@]}" ps ;;
  logs) "${compose[@]}" logs --tail=100 -f miku ;;
  stop) "${compose[@]}" stop miku ;;
  down) "${compose[@]}" down ;;
  quick) "${compose[@]}" exec miku miku quick "$@" ;;
  sub) "${compose[@]}" exec -T miku miku sub ;;
  password) "${compose[@]}" exec miku miku password ;;
  backup)
    mkdir -p docker-backups
    was_running="$("${compose[@]}" ps --status running -q miku)"
    resume_miku() { if [[ -n "$was_running" ]]; then "${compose[@]}" start miku; fi; }
    trap resume_miku EXIT
    "${compose[@]}" stop miku
    backup_name="miku-docker-$(date -u +%Y%m%dT%H%M%SZ)-$$.tar.gz"
    "${compose[@]}" run --rm --no-deps --entrypoint python3 miku \
      -B /usr/local/lib/miku/container.py snapshot "$backup_name"
    echo "备份完成：$MIKU_DOCKER_DIR/docker-backups/$backup_name"
    ;;
esac

#!/usr/bin/env bash
# Runs under progress.py; stdout/stderr go to the private installer log.
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo '请以 root 安装 Docker 环境'; exit 1; }
if ! command -v docker >/dev/null || ! docker compose version >/dev/null 2>&1; then
  . /etc/os-release
  case "$ID" in debian|ubuntu) ;; *) echo '自动安装环境支持 Debian / Ubuntu；其他系统请先安装 Docker Engine 与 Compose v2。'; exit 1 ;; esac
  apt-get -qq update
  apt-get -y -qq install --no-install-recommends ca-certificates curl python3 kmod
  install -m 0755 -d /etc/apt/keyrings
  curl --connect-timeout 15 --retry 3 -fsSL "https://download.docker.com/linux/$ID/gpg" -o /etc/apt/keyrings/docker.asc
  chmod a+r /etc/apt/keyrings/docker.asc
  cat > /etc/apt/sources.list.d/docker.sources <<EOF
Types: deb
URIs: https://download.docker.com/linux/$ID
Suites: ${UBUNTU_CODENAME:-$VERSION_CODENAME}
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/docker.asc
EOF
  apt-get -qq update
  apt-get -y -qq install --no-install-recommends docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
fi
if ! docker info >/dev/null 2>&1; then systemctl enable --now docker; fi
docker info >/dev/null
docker compose version
python3 -c 'import sys; assert sys.version_info >= (3,10), "需要 Python 3.10+"'
if [[ "${MIKU_FRONTEND_ONLY:-0}" != 1 ]]; then
  if [[ ! -c /dev/net/tun ]]; then modprobe tun; fi
  [[ -c /dev/net/tun ]] || { echo 'VPS 尚未提供 TUN，请在服务商面板开启。'; exit 1; }
fi

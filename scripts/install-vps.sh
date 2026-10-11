#!/bin/sh
# Local verified distribution installer; never pipe an unreviewed remote script.
set -eu
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
if [ "$(id -u)" != 0 ]; then echo '请在已授权VPS用sudo运行；安装后控制服务用专用普通账号。' >&2; exit 1; fi
if [ "$(uname -s)" != Linux ]; then echo 'VPS安装仅支持Linux。' >&2; exit 1; fi
if ! command -v python3 >/dev/null 2>&1; then
  for argument in "$@"; do
    if [ "$argument" = --plan-only ]; then echo 'PLAN_ONLY缺少python3，未安装依赖；请先准备解释器。' >&2; exit 1; fi
  done
  echo '缺少安装协调器依赖python3。只安装python3，不升级全系统。'
  printf '是否安装python3？明确输入yes，否则无安装：'
  read -r answer
  if [ "$answer" != yes ]; then echo '用户拒绝依赖安装，停止。'; exit 1; fi
  if ! command -v apt-get >/dev/null 2>&1; then echo '当前bootstrap只支持apt系统，请自行安装python3后重试。' >&2; exit 1; fi
  apt-get update
  apt-get install --no-install-recommends -y python3
fi
base=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
exec python3 -B "$base/miku-install.py" "$@"

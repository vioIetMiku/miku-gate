#!/usr/bin/env bash
# Miku Gate short installer entry point.
set -euo pipefail
# Uninstall may have removed the invoking shell's working directory.
pwd -P >/dev/null 2>&1 || cd /
if [[ "${1:-}" != --help && "${1:-}" != -h ]]; then
# MikuGate installer wordmark; sourced by local installers.
if [[ "${MIKU_INSTALL_BANNER_SHOWN:-0}" != 1 ]]; then
  if [[ -t 1 && "${TERM:-dumb}" != dumb && -z "${NO_COLOR:-}" ]]; then printf '\033[38;5;43m'; fi
  cat <<'MIKU_WORDMARK'
 __  __ _ _                ____       _
|  \/  (_) | ___   _      / ___| __ _| |_ ___
| |\/| | | |/ / | | |____| |  _ / _` | __/ _ \\
| |  | | |   <| |_| |____| |_| | (_| | ||  __/
|_|  |_|_|_|\_\\__,_|     \____|\__,_|\__\___|
                    MikuGate / 你的连接，你的节奏
MIKU_WORDMARK
  if [[ -t 1 && "${TERM:-dumb}" != dumb && -z "${NO_COLOR:-}" ]]; then printf '\033[0m'; fi
  export MIKU_INSTALL_BANNER_SHOWN=1
fi
fi
url='https://github.com/vioIetMiku/miku-gate/releases/latest/download/install-online.sh'
tmp=$(mktemp -d "${TMPDIR:-/tmp}/mikugate.XXXXXXXX")
trap 'rm -rf -- "$tmp"' EXIT
curl --proto '=https' --tlsv1.2 --connect-timeout 15 --retry 3 -fsSL "$url" -o "$tmp/install-online.sh"
bash "$tmp/install-online.sh" "$@"

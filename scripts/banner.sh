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

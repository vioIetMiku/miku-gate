#!/usr/bin/env bash
set -euo pipefail
exec python3 -B /usr/local/lib/miku/container.py "${@}"

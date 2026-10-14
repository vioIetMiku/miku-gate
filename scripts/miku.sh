#!/bin/sh
set -eu
if [ "$(id -u)" -ne 0 ]; then
  exec sudo -- /usr/local/bin/miku "$@"
fi
exec /usr/bin/env python3 -B /usr/local/lib/miku/maintain.py "$@"

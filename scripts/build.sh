#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
command -v go >/dev/null
command -v npm >/dev/null
(cd web && npm ci && npm run build)
mkdir -p bin
go build -trimpath -o bin/miku-gate ./cmd/miku-gate
printf '\nBuilt control-plane development release. No VPN executor is included.\n'

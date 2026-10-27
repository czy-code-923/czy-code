#!/usr/bin/env bash
# Runs from a reviewed source checkout. Never curl-pipe this script as root.
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
case "${1:-compose}" in
  compose)
    command -v docker >/dev/null || { echo 'Install Docker Engine + Compose v2 from docs.docker.com first.'; exit 1; }
    docker compose version >/dev/null
    command -v python3 >/dev/null || { echo 'Python 3 is required to initialize credentials.'; exit 1; }
    python3 scripts/prepare.py
    docker compose config --quiet
    docker compose up --build --detach --wait --wait-timeout 180
    printf '%s\n' 'Harbor ready at http://127.0.0.1:5173 (SSH tunnel for remote access).' 'ADMIN_TOKEN is in .env. Never publish this file.'
    ;;
  build)
    command -v go >/dev/null
    command -v npm >/dev/null
    mkdir -p bin
    for program in control agent agent-cleanup firewall gateway; do CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o "bin/harbor-$program" "./cmd/$program"; done
    (cd web && npm ci && npm run build)
    echo 'Built bin/ and web/dist/. See docs/INSTALL.md for PostgreSQL and native service setup.'
    ;;
  *) echo 'Usage: bash scripts/install.sh [compose|build]'; exit 2 ;;
esac

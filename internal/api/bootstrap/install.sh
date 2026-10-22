#!/bin/sh
set -eu
if [ "$(id -u)" -ne 0 ]; then exec sudo sh "$0" "$@"; fi
[ "$#" -eq 2 ] || { echo 'Expected HTTPS control origin and enrollment ticket.' >&2; exit 1; }
command -v python3 >/dev/null || { echo 'Please install Python 3 (apt install python3 or dnf install python3), then rerun.' >&2; exit 1; }
command -v systemctl >/dev/null || { echo 'This installer requires a Linux systemd host.' >&2; exit 1; }
case "$1" in https://*) ;; *) echo 'Verified HTTPS is required.' >&2; exit 1;; esac
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
chmod 700 "$work"
curl --proto '=https' --tlsv1.2 -fsS "$1/api/v1/agent/install.py" -o "$work/install.py"
python3 "$work/install.py" "$1" "$2"

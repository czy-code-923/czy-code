#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
role="${1:-control}"
case "$role" in control) config=.env ;; agent) config=.agent.env ;; *) exit 2 ;; esac
# Parse key=value without executing configuration as shell code.
while IFS= read -r line || [[ -n "$line" ]]; do
 line="${line%$'\r'}"
 if [[ "$line" =~ ^([A-Z_][A-Z0-9_]*)=(.*)$ ]]; then export "${BASH_REMATCH[1]}=${BASH_REMATCH[2]}"; fi
done < "$config"
exec "./bin/harbor-$role"

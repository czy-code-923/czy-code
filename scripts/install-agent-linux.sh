#!/usr/bin/env bash
set -euo pipefail
[[ $(id -u) = 0 ]] || { echo 'Run as root after reviewing script.'; exit 1; }
command -v systemctl >/dev/null || { echo 'systemd required; on Alpine use the documented foreground/OpenRC method.'; exit 1; }
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
[[ -f bin/harbor-agent && -f .agent.env ]] || { echo 'Build agent and create .agent.env using docs/INSTALL.md first.'; exit 1; }
[[ ! -e /etc/systemd/system/harbor-agent.service ]] || { echo 'Existing service found; update deliberately with backup.'; exit 1; }
getent passwd harbor >/dev/null || useradd --system --home-dir /var/lib/harbor --shell /usr/sbin/nologin harbor
install -d -o root -g harbor -m 0750 /opt/harbor /etc/harbor
install -d -o harbor -g harbor -m 0700 /var/lib/harbor/live
install -o root -g root -m 0755 bin/harbor-agent /opt/harbor/harbor-agent
install -o root -g harbor -m 0640 .agent.env /etc/harbor/agent.env
if [[ -f engines/receipt.json ]]; then
 install -d -o root -g root -m 0755 /opt/harbor/engines
 for engine in sing-box frps frpc; do install -o root -g root -m 0755 "engines/$engine" "/opt/harbor/engines/$engine"; done
fi
python3 - <<'PY'
from pathlib import Path
p=Path('/etc/harbor/agent.env');s=p.read_text();lines=[]
for line in s.splitlines():
 if line.startswith('LIVE_STATE_DIR='): line='LIVE_STATE_DIR=/var/lib/harbor/live'
 for prefix,name in [('SING_BOX','sing-box'),('FRPS','frps'),('FRPC','frpc')]:
  if line.startswith(prefix+'_BINARY='): line=prefix+'_BINARY=/opt/harbor/engines/'+name
 lines.append(line)
p.write_text('\n'.join(lines)+'\n')
PY
cat > /etc/systemd/system/harbor-agent.service <<'UNIT'
[Unit]
Description=Harbor outbound Agent and constrained engine supervisor
After=network-online.target
Wants=network-online.target
[Service]
User=harbor
Group=harbor
EnvironmentFile=/etc/harbor/agent.env
ExecStart=/opt/harbor/harbor-agent
Restart=on-failure
RestartSec=10
KillMode=control-group
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/harbor
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
UMask=0077
MemoryMax=384M
TasksMax=96
[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable --now harbor-agent
systemctl --no-pager status harbor-agent

"""Create inventory-only sample assets and queued tasks through the authenticated API."""
from pathlib import Path
import json
import os
import urllib.request
import uuid

root = Path(__file__).resolve().parents[1]
env_file = Path(os.environ.get('HARBOR_ENV_FILE', str(root / '.env')))
env = dict(line.split('=', 1) for line in env_file.read_text().splitlines() if line and not line.startswith('#'))
base = os.environ.get('CONTROL_URL', 'http://127.0.0.1:8080')

def post(path, body):
    request = urllib.request.Request(base + '/api/v1/' + path, json.dumps(body).encode(), headers={
        'Authorization': 'Bearer ' + env['ADMIN_TOKEN'], 'Content-Type': 'application/json', 'Idempotency-Key': str(uuid.uuid4())})
    with urllib.request.urlopen(request, timeout=10) as r:
        return json.load(r)

hosts = [post('hosts', dict(name=name+' · 演示', provider=name, address='inventory.invalid')) for name in ['GreenCloud','Gomami','DMIT']]
proxy = post('nodes', dict(name='基础代理 · 演示', engine='sing-box'))
frp = post('nodes', dict(name='内网隧道 · 演示', engine='frp'))
instances = []
for host in hosts:
    instances.append(post('instances',dict(name='代理实例 · '+host['id'][:6], host_id=host['id'], node_id=proxy['id'], listen_port=2080)))
tunnel_instance = post('instances',dict(name='FRP 实例 · 演示',host_id=hosts[0]['id'],node_id=frp['id'],listen_port=7000))
post('tunnels',dict(name='本地 Web · 演示',instance_id=tunnel_instance['id'],local_host='127.0.0.1',local_port=8080,remote_port=18080))
post('routes',dict(name='三机中转规划 · 不执行',hops=[i['id'] for i in instances]))
for instance in [*instances,tunnel_instance]:
    config = post('instances/'+instance['id']+'/configs',dict(outbound='block'))
    post('instances/'+instance['id']+'/deploy',dict(config_id=config['id']))
# Only the first demo host gets an Agent. Other hosts intentionally remain queued.
env['HOST_ID'], env['AGENT_TOKEN'] = hosts[0]['id'], hosts[0]['agent_token']
with env_file.open('w',encoding='utf-8') as f:
    f.write(''.join(f'{key}={value}\n' for key,value in env.items()))
print('Created 3 inventory hosts, 2 nodes, 4 instances, 1 tunnel, 1 planned route, 4 configs and 4 tasks.')
print('First demo Agent credentials saved to .env. Start: docker compose --profile demo up -d agent')

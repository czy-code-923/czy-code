"""Local-only end-to-end native staging and rollback check. No engine service is started.

Requires a running local control API, ADMIN_TOKEN (or .env), compiled AGENT_BINARY,
and SING_BOX/FRPS/FRPC_BINARY + corresponding _SHA256 environment variables.
Adds clearly labelled test inventory; intentionally keeps private state for inspection.
"""
from pathlib import Path
import hashlib
import json
import os
import subprocess
import time
import urllib.request
import urllib.parse
import uuid

root = Path(__file__).resolve().parents[1]
values = {}
if (root / '.env').exists():
    values.update(dict(line.split('=', 1) for line in (root / '.env').read_text().splitlines() if line and not line.startswith('#')))
values.update(os.environ)
base = values.get('CONTROL_URL', 'http://127.0.0.1:8080').rstrip('/')
if urllib.parse.urlparse(base).hostname not in ['localhost', '127.0.0.1', '::1']:
    raise SystemExit('This smoke check only accepts a loopback control API')
for key in ['ADMIN_TOKEN','AGENT_BINARY','SING_BOX_BINARY','SING_BOX_SHA256','FRPS_BINARY','FRPS_SHA256','FRPC_BINARY','FRPC_SHA256']:
    if not values.get(key):
        raise SystemExit('Required setting missing: '+key)
for prefix in ['SING_BOX','FRPS','FRPC']:
    if hashlib.sha256(Path(values[prefix+'_BINARY']).read_bytes()).hexdigest() != values[prefix+'_SHA256'].lower():
        raise SystemExit('Pinned binary does not match: '+prefix)

def request(path, body=None):
    headers = {'Authorization':'Bearer '+values['ADMIN_TOKEN'], 'Content-Type':'application/json', 'Idempotency-Key':uuid.uuid4().hex}
    req = urllib.request.Request(base+'/api/v1/'+path, None if body is None else json.dumps(body).encode(), headers)
    with urllib.request.urlopen(req, timeout=10) as response:
        return json.load(response)

def wait(job):
    deadline=time.monotonic()+45
    while time.monotonic()<deadline:
        task=next((t for t in request('tasks') if t['id']==job['id']), None)
        if task and task['state']=='failed':
            raise RuntimeError('Staging job failed: '+task['result'])
        if task and task['state']=='succeeded':
            assert len(task['artifact_checksum'])==64 and task['mode']=='staging'
            return task
        time.sleep(.25)
    raise TimeoutError('Agent task did not finish')

host=request('hosts',dict(name='Native smoke test · inventory only',provider='Local QA',address='inventory.invalid'))
proxy=request('nodes',dict(name='Native smoke proxy',engine='sing-box'))
frp=request('nodes',dict(name='Native smoke FRP',engine='frp'))
instance=request('instances',dict(name='Native smoke proxy',host_id=host['id'],node_id=proxy['id'],listen_port=24080))
fi=request('instances',dict(name='Native smoke FRP',host_id=host['id'],node_id=frp['id'],listen_port=27000))
request('tunnels',dict(name='Native smoke web',instance_id=fi['id'],local_host='127.0.0.1',local_port=8080,remote_port=28080))
state_parent=Path(values.get('SMOKE_STATE_PARENT', root/'state'))
state=(state_parent/('smoke-'+uuid.uuid4().hex)).resolve()
env={**os.environ,**values,'CONTROL_URL':base,'HOST_ID':host['id'],'AGENT_TOKEN':host['agent_token'],'AGENT_MODE':'staging','STATE_DIR':str(state),'ENGINE_SECRET':uuid.uuid4().hex+uuid.uuid4().hex}
flags=subprocess.CREATE_NO_WINDOW if os.name=='nt' else 0
agent=subprocess.Popen([str(Path(values['AGENT_BINARY']).resolve())],env=env,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,creationflags=flags)
try:
    path='instances/'+instance['id']
    first=request(path+'/configs',dict(outbound='block'))
    wait(request(path+'/deploy',dict(config_id=first['id'],mode='staging')))
    second=request(path+'/configs',dict(outbound='direct'))
    wait(request(path+'/deploy',dict(config_id=second['id'],mode='staging')))
    rollback=wait(request(path+'/rollback',dict(config_id=first['id'],mode='staging')))
    assert rollback['operation']=='rollback'
    fc=request('instances/'+fi['id']+'/configs',dict(outbound='block'))
    wait(request('instances/'+fi['id']+'/deploy',dict(config_id=fc['id'],mode='staging')))
    for iid,cfg in [(instance['id'],first['id']),(fi['id'],fc['id'])]:
        pointer=json.loads((state/'instances'/iid/'current.json').read_text())
        assert pointer['config_id']==cfg
        assert (state/pointer['revision']).is_dir()
    print('PASS: API -> Agent -> official native validators -> private config staging -> historical rollback')
    print('4 successful native staging tasks; no services started; state retained at '+str(state))
finally:
    agent.terminate()
    try:
        agent.wait(timeout=10)
    except subprocess.TimeoutExpired:
        agent.kill()
        agent.wait()

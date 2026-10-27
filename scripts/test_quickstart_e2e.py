"""Destructive installation/uninstallation test ONLY on a fresh GitHub-hosted Linux VM.
Never run on a user's VPS. The independent CI job has no user data or credentials.
"""
import json,os,subprocess,sys,time,urllib.error,urllib.request
from pathlib import Path

def main():
    if os.name=='nt' or os.geteuid()!=0 or os.environ.get('GITHUB_ACTIONS')!='true' or os.environ.get('RUNNER_ENVIRONMENT')!='github-hosted':
        raise RuntimeError('Fresh GitHub-hosted Linux runner required')
    if Path('/opt/harbor').exists() or Path('/etc/harbor').exists() or Path('/opt/starhub-control').exists():
        raise RuntimeError('Existing installation refused; this test cannot run on a user VPS')
    distribution=Path(sys.argv[1]).resolve()
    subprocess.run([sys.executable,str(distribution/'scripts/quickstart.py'),'--origin','http://127.0.0.1:18080'],check=True)
    root=Path('/opt/starhub-control')
    env=dict(line.split('=',1) for line in (root/'control.env').read_text().splitlines() if '=' in line)
    def api(path,body=None,status=200,method=None,agent=None):
        headers={'Content-Type':'application/json','Authorization':'Bearer '+(agent['AGENT_TOKEN'] if agent else env['ADMIN_TOKEN'])}
        if agent:headers['X-Host-ID']=agent['HOST_ID']
        request=urllib.request.Request('http://127.0.0.1:18080/api/v1/'+path,data=None if body is None else json.dumps(body).encode(),headers=headers,method=method)
        try:
            with urllib.request.urlopen(request,timeout=12) as response:
                if response.status!=status:raise RuntimeError('Unexpected HTTP status for '+path)
                return json.load(response)
        except urllib.error.HTTPError as error:
            if error.code==status:return None
            raise RuntimeError('HTTP '+str(error.code)+' for '+path) from None
    def wait(check,description,seconds=120):
        for _ in range(seconds):
            if check():return
            time.sleep(1)
        raise RuntimeError('Timeout: '+description)
    assert api('session')['version']=='0.14.1'
    site=api('site');assert site['domain']=='' and site['ip_origin']=='http://127.0.0.1:18080'
    assert api('agent/releases/linux-amd64')['size']>1000000
    print('Fresh one-command control installation and no-domain IP access: PASS',flush=True)
    host=api('hosts',{'name':'CI disposable Agent'},201)
    path='hosts/'+host['id']+'/enrollment'
    api(path,{},400)
    command=api(path,{'allow_insecure_http':True},201)['command']
    # Only our own fixed enrollment command on a fresh CI VM. Never log its ticket.
    result=subprocess.run(['sh','-c',command],stdout=subprocess.DEVNULL)
    if result.returncode:raise RuntimeError('Agent bootstrap failed; enrollment ticket omitted')
    def connected():
        return any(h['id']==host['id'] and h['agent_version']=='0.14.1' and h['online'] and h['cleanup_capable'] for h in api('hosts'))
    wait(connected,'Agent registration and verified heartbeat')
    wait(lambda:any(k['engine']=='sing-box' and k['ready'] for k in api('hosts/'+host['id']+'/kernels')),'Native kernel capabilities')
    agent_env=Path('/etc/harbor-agent/agent.env')
    credentials=dict(line.split('=',1) for line in agent_env.read_text().splitlines() if '=' in line)
    row=api('live',{'name':'CI disposable SS','host_id':host['id'],'engine':'sing-box','kind':'shadowsocks','listen':'127.0.0.1','address':'127.0.0.1','port':24789},201)
    api('live/'+row['id']+'/toggle',{'enabled':True})
    wait(lambda:any(v['id']==row['id'] and v['state']=='running' and v['reported_version']==v['version'] for v in api('live')),'Native inbound deployment')
    print('One-command Agent onboarding and real native deployment: PASS',flush=True)
    api('hosts/'+host['id']+'/retirement',{},202)
    api('hosts/'+host['id']+'/rotate-agent-token',{},404)
    wait(lambda:all(h['id']!=host['id'] for h in api('hosts')),'Root cleanup and authenticated purge acknowledgement',150)
    paths=('/opt/harbor-agent','/etc/harbor-agent/agent.env','/var/lib/harbor-agent/live','/etc/systemd/system/harbor-agent.service','/etc/harbor-agent-cleanup.json')
    wait(lambda:all(not Path(path).exists() for path in paths),'Agent files and units fully removed',30)
    assert all(v['id']!=row['id'] for v in api('live'))
    api('agent/heartbeat',{},401,agent=credentials)
    assert api('session')['version']=='0.14.1' and root.joinpath('control.env').exists()
    subprocess.run(['docker','exec','starhub-control-db','pg_isready','-U','harbor','-d','harbor'],check=True,stdout=subprocess.DEVNULL)
    print('Remote Agent files and control-side deployment data removed; manager and database preserved: PASS',flush=True)
if __name__=='__main__':main()

"""Destructive installation/uninstallation test ONLY on a fresh GitHub-hosted Linux VM.
Never run on a user's VPS. The independent CI job has no user data or credentials.
"""
import datetime,http.client,http.server,json,os,subprocess,sys,tempfile,threading,time,urllib.error,urllib.request
from pathlib import Path

def main():
    if os.name=='nt' or os.geteuid()!=0 or os.environ.get('GITHUB_ACTIONS')!='true' or os.environ.get('RUNNER_ENVIRONMENT')!='github-hosted':
        raise RuntimeError('Fresh GitHub-hosted Linux runner required')
    if Path('/opt/harbor').exists() or Path('/etc/harbor').exists() or Path('/opt/starhub-control').exists():
        raise RuntimeError('Existing installation refused; this test cannot run on a user VPS')
    distribution=Path(sys.argv[1]).resolve()
    install=['sh',str(Path(sys.argv[2]).resolve())] if len(sys.argv)>2 else [sys.executable,str(distribution/'scripts/quickstart.py')]
    subprocess.run(install+['--origin','http://127.0.0.1:18080'],check=True)
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
    assert api('session')['version']=='0.23.9'
    site=api('site');assert site['domain']=='' and site['ip_origin']=='http://127.0.0.1:18080'
    assert api('agent/releases/linux-amd64')['size']>1000000
    print('Fresh one-command control installation and no-domain IP access: PASS',flush=True)
    wait(lambda:api('panel-updates')['helper_ready'],'Independent updater heartbeat',45)
    subprocess.run([sys.executable,str(Path(__file__).with_name('test_panel_update_e2e.py')),str(distribution)],check=True,timeout=600)
    access=root/'panel-access.py'
    status=json.loads(subprocess.check_output([sys.executable,str(access),'status'],text=True))
    assert status['panel_url']=='http://127.0.0.1:18080' and not status['recovery']['active']
    denied=subprocess.run([sys.executable,str(access),'enable-ip'],capture_output=True,text=True)
    assert denied.returncode!=0 and '--allow-http' in denied.stderr
    subprocess.run([sys.executable,str(access),'enable-ip','--allow-http','--bind','127.0.0.1','--ip','127.0.0.1','--port','18081','--minutes','1'],check=True)
    wait(lambda:subprocess.run(['systemctl','is-active','--quiet','starhub-panel-rescue.service']).returncode==0,'Recovery service ready',10)
    status=json.loads(subprocess.check_output([sys.executable,str(access),'status'],text=True))
    assert status['recovery']['active'] and status['recovery']['expires_at']
    recovery='http://127.0.0.1:18081'
    def recovery_status(origin,authorized=True):
        headers={'Origin':origin,'Host':'192.0.2.1:18081'}
        if authorized:headers['Authorization']='Bearer '+env['ADMIN_TOKEN']
        try:
            with urllib.request.urlopen(urllib.request.Request(recovery+'/api/v1/session',headers=headers),timeout=5) as response:return response.status
        except urllib.error.HTTPError as e:return e.code
    assert recovery_status('http://192.0.2.1:18081')==200
    assert recovery_status('http://192.0.2.1:18081',False)==401
    assert recovery_status('http://other.example')==403
    subprocess.run([sys.executable,str(access),'disable-ip'],check=True)
    assert not json.loads(subprocess.check_output([sys.executable,str(access),'status'],text=True))['recovery']['active']
    ttl_process=subprocess.Popen([str(root/'bin/harbor-control'),'panel-web','--addr','127.0.0.1:18082','--dir',str(root/'web'),'--ttl','1s'],env={**os.environ,**env})
    assert ttl_process.wait(timeout=8)==0
    print('Panel address query, authenticated same-origin IP recovery, cross-origin denial and automatic expiry: PASS',flush=True)
    host=api('hosts',{'name':'CI disposable Agent'},201)
    path='hosts/'+host['id']+'/enrollment'
    api(path,{},400)
    command=api(path,{'allow_insecure_http':True},201)['command']
    # Only our own fixed enrollment command on a fresh CI VM. Never log its ticket.
    result=subprocess.run(['sh','-c',command],stdout=subprocess.DEVNULL)
    if result.returncode:raise RuntimeError('Agent bootstrap failed; enrollment ticket omitted')
    def connected():
        return any(h['id']==host['id'] and h['agent_version']=='0.23.9' and h['online'] and h['cleanup_capable'] for h in api('hosts'))
    wait(connected,'Agent registration and verified heartbeat')
    wait(lambda:any(k['engine']=='sing-box' and k['ready'] for k in api('hosts/'+host['id']+'/kernels')),'Native kernel capabilities')
    agent_env=Path('/etc/harbor-agent/agent.env')
    credentials=dict(line.split('=',1) for line in agent_env.read_text().splitlines() if '=' in line)
    row=api('live',{'name':'CI disposable SS','host_id':host['id'],'engine':'sing-box','kind':'shadowsocks','listen':'127.0.0.1','address':'127.0.0.1','port':24789},201)
    api('live/'+row['id']+'/toggle',{'enabled':True})
    wait(lambda:any(v['id']==row['id'] and v['state']=='running' and v['reported_version']==v['version'] for v in api('live')),'Native inbound deployment')
    print('One-command Agent onboarding and real native deployment: PASS',flush=True)
    update=api('hosts/'+host['id']+'/engine-updates',{'engine':'sing-box','version':'latest'},202)
    def switched():
        rows=api('hosts/'+host['id']+'/engine-updates')
        current=next(v for v in rows if v['id']==update['id'])
        if current['state']=='failed':raise RuntimeError('Official update failed: '+current.get('error',''))
        return current['state']=='succeeded'
    wait(switched,'Official engine download, validation and switch',480)
    wait(lambda:any(v['id']==row['id'] and v['state']=='running' for v in api('live')),'Existing inbound after engine switch')
    kernels=api('hosts/'+host['id']+'/kernels')
    assert any(k['engine']=='sing-box' and k['ready'] and 'harbor' not in k['version'].lower() for k in kernels)
    print('Official latest engine switch with existing inbound preserved: PASS',flush=True)

    # Check real host counters after downloading a large official archive.
    traffic_path='hosts/'+host['id']+'/traffic'
    wait(lambda:api(traffic_path)['fresh'] and api(traffic_path)['period_rx']>0,'OS interface traffic report')
    traffic=api(traffic_path)
    api(traffic_path,{'version':traffic['version'],'quota_bytes':10**12,'mode':'both','reset_day':5,'auto_reset':True,'interfaces':[]},method='PUT')
    traffic=api(traffic_path)
    assert traffic['used_bytes']==traffic['period_rx']+traffic['period_tx']
    assert traffic['remaining_bytes']==traffic['quota_bytes']-traffic['used_bytes']
    assert datetime.datetime.fromisoformat(traffic['next_reset_at'].replace('Z','+00:00')).astimezone(datetime.timezone(datetime.timedelta(hours=8))).day==5
    print('Native host byte counters, quota balance and monthly reset day: PASS',flush=True)

    # A real per-device VMess identity transfers data through the installed Agent.
    managed=api('live',{'name':'CI device VMess','host_id':host['id'],'engine':'sing-box','kind':'vmess','listen':'127.0.0.1','address':'127.0.0.1','port':24790,'tls_mode':'none','managed_users':True},201)
    api('live/'+managed['id']+'/toggle',{'enabled':True})
    user=api('proxy-users',{'name':'CI device owner','enabled':True,'expires_at':(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(days=1)).isoformat(),'quota_bytes':10**9,'billing_mode':'both','multiplier_milli':1500,'max_devices':1,'device_limit_mode':'hwid'})
    api('user-groups',{'name':'CI device grant','user_ids':[user['id']],'service_ids':[managed['id']],'max_devices':2})
    device_path='proxy-users/'+user['id']+'/devices'
    subscription=api('subscriptions',{'name':'CI stable HWID subscription','days':1,'user_id':user['id']},201)
    device_url=subscription['url']+'?client=sing-box'
    request_headers={'User-Agent':'Starhub-CI/device-client','X-Hwid':'ci-client-installation-0123456789','X-Device-Os':'Linux','X-Ver-Os':'CI','X-Device-Model':'Disposable CI VM'}
    def download(headers=request_headers):
        try:
            with urllib.request.urlopen(urllib.request.Request(device_url,headers=headers),timeout=15) as response:
                return response.status,response.headers,json.load(response)
        except urllib.error.HTTPError as error:
            return error.code,error.headers,None
    assert download({'User-Agent':'ClashVerge/CI'})[0]==409
    status,headers,_=download();assert status==503 and headers['Retry-After']=='3'
    status,headers,_=download({**request_headers,'X-Hwid':'second-ci-installation-0123456789'});assert status==403 and headers['X-Hwid-Max-Devices-Reached']=='true'
    device=api(device_path)[0]
    assert device['registration_source']=='subscription_hwid' and device['device_os']=='Linux'
    device['url']=device_url
    def deployed():
        return any(v['id']==managed['id'] and v['state']=='running' and v['reported_version']==v['version'] for v in api('live'))
    wait(deployed,'Device-specific identity deployment')
    with urllib.request.urlopen(urllib.request.Request(device['url'],headers=request_headers),timeout=15) as response:
        exported=json.load(response)
    outbound=next(v for v in exported['outbounds'] if v['type']=='vmess')
    outbound['tag']='device-proxy'
    class Payload(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            data=b'x'*(2*1024*1024)
            self.send_response(200);self.send_header('Content-Length',str(len(data)));self.end_headers();self.wfile.write(data)
        def log_message(self,*args):pass
    target=http.server.ThreadingHTTPServer(('127.0.0.1',0),Payload)
    thread=threading.Thread(target=target.serve_forever,daemon=True);thread.start()
    def transfer():
        connection=http.client.HTTPConnection('127.0.0.1',24791,timeout=10)
        try:
            connection.request('GET','http://127.0.0.1:'+str(target.server_port)+'/metadata-only')
            response=connection.getresponse()
            return response.status==200 and len(response.read())==2*1024*1024
        except (OSError,http.client.HTTPException):return False
        finally:connection.close()
    try:
        with tempfile.TemporaryDirectory(prefix='starhub-device-ci-') as directory:
            config=Path(directory)/'client.json'
            config.write_text(json.dumps({'log':{'level':'error'},'inbounds':[{'type':'http','tag':'local','listen':'127.0.0.1','listen_port':24791}],'outbounds':[outbound],'route':{'final':'device-proxy'}}))
            with subprocess.Popen([credentials['SING_BOX_BINARY'],'run','-c',str(config)],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL) as client:
                try:
                    wait(transfer,'Actual per-device proxy transfer',30)
                    wait(lambda:api('proxy-users/'+user['id'])['used_bytes']>0,'Device counter mapped to parent billing')
                    bill=api('proxy-users/'+user['id'])
                    assert bill['used_bytes']==(bill['upload_bytes']+bill['download_bytes'])*1500//1000
                    assert bill['billed_upload_bytes']+bill['billed_download_bytes']==bill['used_bytes']
                    def recorded():
                        rows=api('access-records/page?device_id='+device['id'])['items']
                        return any(v['source_ip']=='127.0.0.1' and v['total_bytes'] is not None and v['total_bytes']>0 for v in rows)
                    wait(recorded,'Connection metadata with device and actual bytes')
                    with urllib.request.urlopen(urllib.request.Request(device['url'],headers=request_headers),timeout=15) as response:
                        fields=dict(part.strip().split('=',1) for part in response.headers['Subscription-Userinfo'].split(';'))
                    assert int(fields['upload'])+int(fields['download'])>=bill['used_bytes']
                    # Expire the link while the parent user remains active, then
                    # test the already imported native proxy credential itself.
                    subrow=next(v for v in api('subscriptions') if v['id']==subscription['id'])
                    subbody={'name':subrow['name'],'profile_id':subrow['profile_id'] or '', 'version':subrow['version'], 'expires_at':(datetime.datetime.now(datetime.timezone.utc)-datetime.timedelta(seconds=10)).isoformat()}
                    api('subscriptions/'+subscription['id'],subbody,method='PUT')
                    wait(lambda:deployed() and not transfer(),'Expired subscription credential denied by the native inbound')
                    assert api('proxy-users/'+user['id'])['active'], 'Parent user unexpectedly disabled'
                    subrow=next(v for v in api('subscriptions') if v['id']==subscription['id'])
                    subbody.update(version=subrow['version'],expires_at=(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(hours=1)).isoformat())
                    api('subscriptions/'+subscription['id'],subbody,method='PUT')
                    wait(lambda:deployed() and transfer(),'Renewed subscription credential restored')
                    api('hosts/'+host['id']+'/connection',{'paused':True})
                    wait(lambda:not transfer(),'Paused Agent stopped the native inbound')
                    assert any(h['id']==host['id'] and h['online'] and h['connection_paused'] for h in api('hosts')), 'Paused Agent did not retain heartbeat'
                    api('hosts/'+host['id']+'/connection',{'paused':False})
                    wait(transfer,'Resumed Agent restored the native inbound')
                    current=next(v for v in api(device_path) if v['id']==device['id'])
                    api(device_path+'/'+device['id'],{'version':current['version'],'name':current['name'],'client_id':'sing-box','enabled':False},method='PUT')
                    wait(deployed,'Disabled device identity removed from native inbound')
                    assert not transfer(), 'Disabled device still authenticated'
                finally:
                    client.terminate()
                    try:client.wait(timeout=10)
                    except subprocess.TimeoutExpired:client.kill();client.wait()
    finally:target.shutdown();target.server_close();thread.join(timeout=5)
    print('Real device transfer, multiplier billing, metadata, subscription usage and revocation: PASS',flush=True)
    assert download()[0]==403
    print('Automatic HWID registration, unsupported/over-limit feedback and disabled-client rejection: PASS',flush=True)


    # Ordinary Clash clients import immediately in compatible mode, no HWID needed.
    api('proxy-users/'+user['id'],{'name':'CI compatible owner','enabled':True,'expires_at':(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(days=1)).isoformat(),'max_devices':2,'device_limit_mode':'compatible'},method='PUT')
    wait(deployed,'Compatible parent identity applied')
    canonical=api('proxy-users/'+user['id']+'/subscription',{})
    assert canonical['id']==subscription['id'] and canonical['reused'] and canonical['url']==subscription['url']
    for ua in ('Clash-Verge/CI','FlClash/CI'):
        with urllib.request.urlopen(urllib.request.Request(canonical['url']+'?client=mihomo',headers={'User-Agent':ua}),timeout=15) as response:
            assert response.status==200 and response.headers['X-Starhub-Device-Recognition']=='source_and_client'
            assert b'proxies:' in response.read()
    observed=[v for v in api(device_path) if v['registration_source']=='observed_client']
    assert len(observed)==1 and observed[0]['user_agent']=='FlClash/CI' and observed[0]['source_ip']
    with urllib.request.urlopen(urllib.request.Request(canonical['url'],headers={'User-Agent':'Shadowrocket/CI'}),timeout=15) as response:auto=response.read()
    with urllib.request.urlopen(canonical['url']+'?client=shadowrocket',timeout=15) as response:manual=response.read()
    assert auto==manual and auto
    print('Clash Verge + FlClash compatible imports, canonical reuse, client metadata and Shadowrocket parity: PASS',flush=True)

    api('hosts/'+host['id']+'/retirement',{},202)
    api('hosts/'+host['id']+'/rotate-agent-token',{},404)
    wait(lambda:all(h['id']!=host['id'] for h in api('hosts')),'Root cleanup and authenticated purge acknowledgement',150)
    paths=('/opt/harbor-agent','/etc/harbor-agent/agent.env','/var/lib/harbor-agent/live','/etc/systemd/system/harbor-agent.service','/etc/harbor-agent-cleanup.json')
    wait(lambda:all(not Path(path).exists() for path in paths),'Agent files and units fully removed',30)
    assert all(v['id']!=row['id'] for v in api('live'))
    api('agent/heartbeat',{},401,agent=credentials)
    assert api('session')['version']=='0.23.9' and root.joinpath('control.env').exists()
    subprocess.run(['docker','exec','starhub-control-db','pg_isready','-U','harbor','-d','harbor'],check=True,stdout=subprocess.DEVNULL)
    print('Remote Agent files and control-side deployment data removed; manager and database preserved: PASS',flush=True)
    # This is an isolated CI VM. Verify both safe uninstall and explicit purge.
    tool=distribution/'scripts/uninstall.py'
    subprocess.run([sys.executable,str(tool),'--layout','quickstart','--apply'],check=True)
    assert root.joinpath('control.env').exists() and not root.joinpath('bin').exists()
    subprocess.run(['docker','volume','inspect','starhub-control-pgdata'],check=True,stdout=subprocess.DEVNULL)
    subprocess.run([sys.executable,str(tool),'--layout','quickstart','--apply','--purge-data'],check=True)
    assert not root.exists()
    assert subprocess.run(['docker','volume','inspect','starhub-control-pgdata'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode!=0
    print('One-command panel uninstall preserves data by default; explicit purge is bounded: PASS',flush=True)

if __name__=='__main__':main()

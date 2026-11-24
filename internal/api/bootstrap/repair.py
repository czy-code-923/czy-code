"""Repair an existing Linux Agent control origin without changing its identity."""
import datetime,hashlib,hmac,io,json,os,platform,re,secrets,shutil,stat,subprocess,sys,tempfile,time,urllib.parse,urllib.request,zipfile
from pathlib import Path

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,*args):raise ValueError('Redirect refused')

VERSION='__PANEL_VERSION__'
REPOSITORY='czy-code-923/czy-code'

class ReleaseRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,req,fp,code,msg,headers,url):
        u=urllib.parse.urlsplit(url)
        if u.scheme!='https' or u.username or u.port or u.hostname not in ('github.com','release-assets.githubusercontent.com','objects.githubusercontent.com'):raise ValueError('Unexpected release redirect')
        return super().redirect_request(req,fp,code,msg,headers,url)

def upgrade_agent(env_path=Path('/etc/harbor-agent/agent.env')):
    arch={'x86_64':'amd64','aarch64':'arm64'}.get(platform.machine())
    if not arch:raise ValueError('Unsupported Linux architecture')
    binary=Path('/opt/harbor-agent/harbor-agent') if env_path==Path('/etc/harbor-agent/agent.env') else Path('/opt/harbor/bin/harbor-agent')
    backup=binary.with_name('harbor-agent.previous')
    for path in (binary,binary.parent,backup if backup.exists() or backup.is_symlink() else binary.parent):
        st=path.lstat()
        if path.is_symlink() or st.st_uid!=0 or st.st_mode&0o022:raise ValueError('Root-owned standard Agent installation required')
    tag='v'+VERSION+'-distribution';name='starhub-'+VERSION+'-agent-releases.zip'
    request=urllib.request.Request('https://api.github.com/repos/'+REPOSITORY+'/releases/tags/'+tag,headers={'User-Agent':'Starhub-Agent-Updater'})
    with urllib.request.build_opener(NoRedirect()).open(request,timeout=30) as response:release=json.loads(response.read(2<<20))
    if release.get('tag_name')!=tag or release.get('draft') or release.get('prerelease'):raise ValueError('Verified stable release required')
    url='https://github.com/'+REPOSITORY+'/releases/download/'+tag+'/'+name
    assets=[a for a in release.get('assets',[]) if a.get('name')==name and a.get('browser_download_url')==url]
    if len(assets)!=1:raise ValueError('Official Agent archive not found')
    asset=assets[0]
    if not isinstance(asset.get('size'),int) or not 0<asset['size']<=200<<20 or not re.fullmatch('sha256:[a-f0-9]{64}',asset.get('digest','')):raise ValueError('Agent archive checksum missing')
    if shutil.disk_usage(binary.parent).free<asset['size']*2+(64<<20):raise ValueError('Not enough space to preserve the previous Agent')
    with tempfile.TemporaryDirectory(prefix='.upgrade-',dir=binary.parent) as folder:
        package=Path(folder)/'agents.zip';sha=hashlib.sha256();size=0
        with urllib.request.build_opener(ReleaseRedirect()).open(url,timeout=120) as response,package.open('xb') as out:
            while chunk:=response.read(1<<20):
                size+=len(chunk)
                if size>asset['size']:raise ValueError('Oversized Agent archive')
                sha.update(chunk);out.write(chunk)
        if size!=asset['size'] or 'sha256:'+sha.hexdigest()!=asset['digest']:raise ValueError('Agent archive checksum mismatch')
        member='linux-'+arch+'.zip'
        with zipfile.ZipFile(package) as z:
            if len(z.infolist())!=3 or set(z.namelist())!={'manifest.json','linux-amd64.zip','linux-arm64.zip'}:raise ValueError('Unexpected Agent archive')
            if z.getinfo('manifest.json').file_size>65536 or z.getinfo(member).file_size>160<<20:raise ValueError('Oversized Agent package')
            manifest=json.loads(z.read('manifest.json'))[member.removesuffix('.zip')];nested=z.read(member)
        if len(nested)!=manifest['size'] or hashlib.sha256(nested).hexdigest()!=manifest['sha256']:raise ValueError('Platform archive checksum mismatch')
        candidate=Path(folder)/'harbor-agent'
        with zipfile.ZipFile(io.BytesIO(nested)) as z:
            if '--enable-port-hopping' in sys.argv or Path('/usr/local/libexec/starhub-agent-network.py').exists():
                info=z.getinfo('agent-network.py')
                if info.file_size>65536:raise ValueError('Oversized network helper')
                helper=Path(folder)/'agent-network.py';helper.write_bytes(z.read(info))
                cleanup=Path('/usr/local/libexec/harbor-agent-cleanup')
                if cleanup.is_symlink() or cleanup.exists() and (cleanup.stat().st_uid!=0 or cleanup.stat().st_mode&0o022):raise ValueError('Untrusted cleanup helper')
                cleanup_info=z.getinfo('harbor-agent-cleanup')
                if cleanup_info.file_size>64<<20:raise ValueError('Oversized cleanup helper')
                staged_cleanup=Path(folder)/'harbor-agent-cleanup';staged_cleanup.write_bytes(z.read(cleanup_info));staged_cleanup.chmod(0o755)
                cleanup.parent.mkdir(parents=True,exist_ok=True)
                shutil.copy2(staged_cleanup,cleanup)
                subprocess.run([sys.executable,str(helper),'--install','--env',str(env_path)],check=True,timeout=60)
            items=[f for f in z.infolist() if f.filename=='harbor-agent']
            if len(items)!=1 or items[0].file_size>64<<20:raise ValueError('Invalid Agent executable')
            with z.open(items[0]) as source,candidate.open('xb') as out:shutil.copyfileobj(source,out)
        candidate.chmod(0o755)
        version=subprocess.run([str(candidate),'--version'],check=True,capture_output=True,text=True,timeout=15).stdout.strip()
        if version!=VERSION:raise ValueError('Downloaded Agent version mismatch')
        shutil.copy2(binary,backup);backup.chmod(0o755)
        subprocess.run(['systemctl','stop','harbor-agent.service'],check=True,timeout=40)
        try:
            os.replace(candidate,binary)
            subprocess.run(['systemctl','start','harbor-agent.service'],check=True,timeout=40)
            time.sleep(2)
            subprocess.run(['systemctl','is-active','--quiet','harbor-agent.service'],check=True,timeout=10)
        except BaseException:
            subprocess.run(['systemctl','stop','harbor-agent.service'],timeout=40)
            shutil.copy2(backup,binary)
            subprocess.run(['systemctl','start','harbor-agent.service'],check=True,timeout=40)
            raise
    print('Agent upgraded to '+VERSION+'. Existing cores, credentials and traffic ledger were preserved. Confirm the version and heartbeat in the panel.')

def verify(origin,values):
    u=urllib.parse.urlsplit(origin)
    if u.scheme!='https' or not u.hostname or u.username or u.password or u.path or u.query or u.fragment:raise ValueError('An HTTPS origin without path is required')
    nonce=secrets.token_hex(32)
    body=json.dumps({'host_id':values['HOST_ID'],'nonce':nonce,'origin':origin}).encode()
    req=urllib.request.Request(origin+'/api/v1/agent/verify-origin',data=body,headers={'Content-Type':'application/json','User-Agent':'Starhub-Agent-Repair/'+VERSION})
    with urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect()).open(req,timeout=15) as response:
        result=json.loads(response.read(2048));proof=result['proof']
    key=hashlib.sha256(values['AGENT_TOKEN'].encode()).hexdigest().encode()
    expected=hmac.new(key,('starhub-control-origin-v1:'+nonce+':'+origin).encode(),hashlib.sha256).hexdigest()
    if not hmac.compare_digest(proof,expected):raise ValueError('New endpoint does not prove this Agent identity; nothing changed')
    if result.get('server_time'):
        server_time=datetime.datetime.fromisoformat(result['server_time'].replace('Z','+00:00')).timestamp()
        if abs(time.time()-server_time)>30:raise ValueError('Clock skew exceeds 30 seconds. Enable NTP on both the control server and this VPS before repairing the address.')

def health(origin,values):
    req=urllib.request.Request(origin+'/api/v1/agent/status',data=b'null',headers={'Content-Type':'application/json','Authorization':'Bearer '+values['AGENT_TOKEN'],'X-Host-ID':values['HOST_ID'],'User-Agent':'Starhub-Agent-Repair/'+VERSION})
    with urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect()).open(req,timeout=10) as response:return json.loads(response.read(4096))

def healthy(status,since=None):
    if not (status.get('online') is True and status.get('traffic_fresh') is True and not status.get('paused') and status.get('failed_services')==0 and status.get('pending_services')==0):return False
    if since:
        def at(value):return datetime.datetime.fromisoformat(value.replace('Z','+00:00'))
        required=['last_seen','last_sample_at']+(['last_node_report_at'] if status.get('enabled_services',0)>0 else [])
        try:
            if any(not status.get(k) or at(status[k])<at(since) for k in required):return False
        except (TypeError,ValueError):return False
    return True

def confirm_recovery(origin,values,deadline=60,since=None):
    until=time.monotonic()+deadline;last={}
    while time.monotonic()<until:
        try:
            last=health(origin,values)
            if healthy(last,since):
                print('Verified: Agent heartbeat, OS traffic sampling and all enabled node versions are healthy. Existing identity and traffic ledger were preserved.')
                return
        except Exception:pass
        time.sleep(3)
    raise ValueError('Address updated but recovery is incomplete: online='+str(last.get('online'))+', traffic_fresh='+str(last.get('traffic_fresh'))+', paused='+str(last.get('paused'))+', failed_nodes='+str(last.get('failed_services'))+', pending_nodes='+str(last.get('pending_services'))+'. Check NTP and journalctl -u harbor-agent.service; for UDP hopping also check starhub-agent-network.service and port conflicts. Credentials were not replaced.')

def main():
    if os.geteuid()!=0:raise ValueError('Run as root on the existing Agent host')
    origin=sys.argv[1].rstrip('/')
    path=Path('/etc/harbor-agent/agent.env')
    if not path.exists() and Path('/etc/harbor/agent.env').exists():path=Path('/etc/harbor/agent.env')
    st=path.lstat()
    if not stat.S_ISREG(st.st_mode) or st.st_uid!=0 or st.st_mode&0o077:raise ValueError('Private root-owned Agent environment required')
    lines=path.read_text().splitlines();values=dict(line.split('=',1) for line in lines if '=' in line and not line.startswith('#'))
    if not re.fullmatch('[a-f0-9]{32}',values.get('HOST_ID','')) or len(values.get('AGENT_TOKEN',''))<32:raise ValueError('Existing Agent identity missing')
    if sum(line.startswith('CONTROL_URL=') for line in lines)!=1:raise ValueError('Expected exactly one CONTROL_URL')
    verify(origin,values) # Never sends the token to a different domain.
    updated='\n'.join('CONTROL_URL='+origin if line.startswith('CONTROL_URL=') else line for line in lines)+'\n'
    os.umask(0o077)
    temporary=path.with_name('agent.env.repair-'+secrets.token_hex(8))
    try:
        with temporary.open('x') as out:out.write(updated);out.flush();os.fsync(out.fileno())
        temporary.chmod(0o600);os.replace(temporary,path)
    finally:temporary.unlink(missing_ok=True)
    restarted=True
    if '--upgrade-agent' in sys.argv[2:]:upgrade_agent(path)
    else:
        current={}
        if values['CONTROL_URL'].rstrip('/')==origin:
            try:current=health(origin,values)
            except Exception:pass
        if current.get('paused'):raise ValueError('Management is paused in the panel; resume it there before recovery. Agent identity was preserved.')
        if not healthy(current):subprocess.run(['systemctl','restart','harbor-agent.service'],check=True,timeout=40)
        else:
            restarted=False
            print('Control address is unchanged and the Agent is healthy; unnecessary restart skipped.')
    since=None
    if restarted:
        since=datetime.datetime.now(datetime.timezone.utc).isoformat()
        try:since=health(origin,values).get('server_time')
        except Exception:pass
    confirm_recovery(origin,values,since=since)

if __name__=='__main__':
    try:main()
    except Exception as error:print('Repair stopped: '+str(error),file=sys.stderr);sys.exit(1)

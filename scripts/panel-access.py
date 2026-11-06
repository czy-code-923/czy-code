"""Query panel addresses, or explicitly open/close a recovery web entrance.

The recovery entrance is OFF by default. enable-ip requires --allow-http because
IP HTTP is not encrypted. Default lifetime is 30 minutes, including after reboot.
Use an SSH tunnel instead on untrusted networks. No API/DB ports are opened.
Linux quickstart: disable-public verifies the saved HTTPS domain before binding
the normal web listener to loopback. enable-autostart enables installed systemd
components and the verified project database's restart policy.
"""
import argparse, contextlib, datetime as dt, ipaddress, json, os, re, shutil, socket, subprocess, sys, tempfile, time, urllib.request
from pathlib import Path

UNIT = Path('/etc/systemd/system/starhub-panel-rescue.service')
MARKER = '# Starhub owned panel recovery gateway\n'
UNIT_DIR = Path('/etc/systemd/system')
UPDATER_STATE = Path('/var/lib/starhub-updater/quickstart')

def _run(argv,**kwargs):
    return subprocess.run(argv,check=True,timeout=kwargs.pop('timeout',60),**kwargs)

def _trusted(path,private=False):
    if not path.is_absolute() or path.resolve()!=path or path.is_symlink():raise ValueError('Unsafe installation path')
    for item in (path,*path.parents):
        info=item.stat()
        if info.st_uid!=0 or info.st_mode&0o022:raise ValueError('Installation paths must be root owned and not group/world writable')
    if private and path.stat().st_mode&0o077:raise ValueError('Private control.env permissions required')

def _installation(root,explicit=None):
    cfg=Path(explicit) if explicit else root/'control.env'
    if root==Path('/opt/harbor') or cfg!=root/'control.env':raise ValueError('This action requires a Linux quickstart installation; legacy deployments use their own service layout')
    _trusted(root);_trusted(cfg,True);_trusted(root/'bin/harbor-control')
    unit=UNIT_DIR/'starhub-control.service';_trusted(unit)
    lines=unit.read_text().splitlines()
    if 'ExecStart='+str(root/'bin/harbor-control') not in lines or 'EnvironmentFile='+str(cfg) not in lines:raise ValueError('Control service does not belong to this installation')
    return cfg

@contextlib.contextmanager
def _maintenance_guard():
    # Share the updater's lock so a manual start cannot interrupt a database
    # snapshot or rollback. Nothing is installed when the updater is absent.
    if not UPDATER_STATE.exists():yield;return
    import fcntl
    _trusted(UPDATER_STATE,True)
    path=UPDATER_STATE/'lock'
    if path.exists():_trusted(path,True)
    fd=os.open(path,os.O_CREAT|os.O_RDWR|getattr(os,'O_NOFOLLOW',0),0o600)
    try:
        for attempt in range(30):
            try:fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB);break
            except BlockingIOError:time.sleep(0.2)
        else:raise ValueError('Panel updater is busy; wait for the update to finish before maintenance')
        journal=UPDATER_STATE/'journal.json'
        if journal.exists():
            _trusted(journal,True);job=json.loads(journal.read_text())
            if job.get('state') not in ('succeeded','rolled_back','failed'):raise ValueError('An unfinished update needs recovery before manual service maintenance')
        yield
    finally:os.close(fd)

class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,*args):raise ValueError('Local control API redirects are refused')

def _local_json(env,path):
    address=listener(env.get('HTTP_ADDR','127.0.0.1:8080'))
    request=urllib.request.Request('http://'+address+'/api/v1/'+path,headers={'Authorization':'Bearer '+env['ADMIN_TOKEN']})
    with urllib.request.build_opener(urllib.request.ProxyHandler({}),_NoRedirect()).open(request,timeout=4) as response:data=response.read((1<<20)+1)
    if len(data)>1<<20:raise ValueError('Oversized local control response')
    result=json.loads(data)
    if not isinstance(result,dict):raise ValueError('Unexpected local control response')
    return result

def _web_port(value):
    from urllib.parse import urlsplit
    parsed=urlsplit('http://'+value)
    if parsed.username or parsed.password or parsed.path or parsed.query or parsed.fragment:raise ValueError('Invalid WEB_ADDR')
    ipaddress.ip_address(parsed.hostname or '')
    if not parsed.port or not 1024<=parsed.port<=65535 or parsed.port in (8080,8091,5432,15432):raise ValueError('Invalid web port')
    return parsed.port

def _probe_https(domain,version):
    if not isinstance(domain,str) or not domain or len(domain)>253 or '.' not in domain or not all(re.fullmatch(r'[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?',part) for part in domain.split('.')):raise ValueError('Save a valid HTTPS panel domain in settings first')
    try:ipaddress.ip_address(domain)
    except ValueError:pass
    else:raise ValueError('A panel domain is required, not an IP address')
    if not shutil.which('curl'):raise ValueError('curl is required for verified HTTPS origin checks')
    result=_run(['curl','--noproxy','*','--resolve',domain+':443:127.0.0.1','--connect-timeout','5','--max-time','15','--retry','3','--retry-delay','1','--retry-max-time','20','--retry-connrefused','-fsS','https://'+domain+'/api/v1/branding'],capture_output=True)
    if len(result.stdout)>1<<20:raise ValueError('Oversized HTTPS response')
    body=json.loads(result.stdout)
    if not isinstance(body,dict) or body.get('version')!=version:raise ValueError('HTTPS domain did not return this panel version')

def _atomic_env(cfg,data):
    mode=cfg.stat().st_mode&0o777
    fd,name=tempfile.mkstemp(prefix='.starhub-control-env-',dir=cfg.parent)
    try:
        with os.fdopen(fd,'wb') as out:out.write(data);out.flush();os.fsync(out.fileno())
        os.chmod(name,mode);os.replace(name,cfg)
    finally:
        if os.path.exists(name):os.unlink(name)

def _wait_control(env):
    for attempt in range(60):
        try:
            if _local_json(env,'session').get('version'):return
        except (OSError,ValueError):pass
        time.sleep(0.5)
    raise ValueError('Control service did not become healthy')

def disable_recovery(root):
    if UNIT.exists():
        verify_unit(root)
        _run(['systemctl','disable','--now',UNIT.name],capture_output=True)
        UNIT.unlink();_run(['systemctl','daemon-reload'],capture_output=True)

def disable_public(root,env,cfg):
    listener(env.get('HTTP_ADDR','127.0.0.1:8080'));listener(env.get('SHARE_ADDR','127.0.0.1:8091'))
    if UNIT.exists():verify_unit(root)
    site=_local_json(env,'site');version=_local_json(env,'session').get('version')
    if not site.get('domain') or site.get('https') is not True or not version:raise ValueError('Configure a working HTTPS panel domain before closing public web access')
    _probe_https(site['domain'],version)
    port=_web_port(env.get('WEB_ADDR','127.0.0.1:18080'));desired=('WEB_ADDR=127.0.0.1:'+str(port)).encode()
    original=cfg.read_bytes();candidate,count=re.subn(rb'(?m)^WEB_ADDR=[^\r\n]*',desired,original)
    if count!=1:raise ValueError('Exactly one WEB_ADDR entry is required; nothing changed')
    if candidate==original:
        disable_recovery(root)
        return {'changed':False,'web_listener':'127.0.0.1:'+str(port),'domain':site['domain'],'recovery_disabled':True}
    prefix=cfg.name+'.before-ip-close.'+dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%SZ')+'.'
    fd,name=tempfile.mkstemp(prefix=prefix,dir=cfg.parent);backup=Path(name)
    with os.fdopen(fd,'wb') as out:out.write(original);out.flush();os.fsync(out.fileno())
    backup.chmod(cfg.stat().st_mode&0o777)
    try:
        _atomic_env(cfg,candidate);_run(['systemctl','restart','starhub-control.service'],capture_output=True)
        _wait_control(env);_probe_https(site['domain'],version);disable_recovery(root)
    except Exception:
        try:_atomic_env(cfg,original);_run(['systemctl','restart','starhub-control.service'],capture_output=True)
        except Exception:raise ValueError('Public-access change failed; rollback needs attention. Private backup: '+str(backup)) from None
        raise ValueError('Public-access change failed; original configuration restored. Private backup: '+str(backup)) from None
    return {'changed':True,'web_listener':'127.0.0.1:'+str(port),'domain':site['domain'],'backup':str(backup),'recovery_disabled':True}

def _helper_timers(root):
    installed=[];missing=[]
    for name,marker,script in [('starhub-gateway','# starhub-owned-gateway-v1\n','gateway-worker.py'),('starhub-panel-updater','# starhub-owned-panel-updater-v1\n','panel-updater.py')]:
        service=UNIT_DIR/(name+'.service');timer=UNIT_DIR/(name+'.timer')
        if not service.exists() and not timer.exists():missing.append(timer.name);continue
        if not service.is_file() or not timer.is_file():raise ValueError('Incomplete helper unit pair: '+name)
        _trusted(service);_trusted(timer)
        if not service.read_text().startswith(marker) or str(root/'scripts'/script) not in service.read_text() or not timer.read_text().startswith(marker) or '[Timer]' not in timer.read_text() or 'Unit='+service.name not in timer.read_text().splitlines():raise ValueError('Unrecognized helper unit ownership: '+name)
        worker=root/'scripts'/script
        if not worker.is_file():raise ValueError('Installed helper worker is missing; repair '+name+' before enabling it')
        _trusted(worker)
        installed.append(timer.name)
    return installed,missing

def _database_owner(env,info):
    from urllib.parse import urlsplit,unquote
    db=urlsplit(env.get('DATABASE_URL',''))
    if db.scheme not in ('postgres','postgresql') or db.hostname not in ('127.0.0.1','localhost','::1') or db.username!='harbor' or db.path!='/harbor':raise ValueError('Expected the local project database')
    if not isinstance(info,list) or len(info)!=1:raise ValueError('Unexpected database container identity')
    row=info[0];config=row.get('Config',{});values=dict(v.split('=',1) for v in config.get('Env',[]) if '=' in v)
    ports=row.get('HostConfig',{}).get('PortBindings',{}).get('5432/tcp',[])
    mounts=row.get('Mounts',[])
    if not config.get('Image','').startswith('postgres:') or values.get('POSTGRES_USER')!='harbor' or values.get('POSTGRES_DB')!='harbor' or values.get('POSTGRES_PASSWORD')!=unquote(db.password or ''):raise ValueError('Database container ownership mismatch')
    if not ports or not all(p.get('HostIp') in ('127.0.0.1','::1') and int(p.get('HostPort',0))==(db.port or 5432) for p in ports):raise ValueError('Database must keep its verified loopback port')
    if not any(m.get('Type')=='volume' and m.get('Name')=='starhub-control-pgdata' and m.get('Destination')=='/var/lib/postgresql/data' for m in mounts):raise ValueError('Unexpected database data volume')
    return row

def _nginx_service(root):
    path=root/'gateway.json'
    if not path.exists():return None
    _trusted(path,True);config=json.loads(path.read_text())
    if config.get('owner')!='starhub-gateway-v1':raise ValueError('Unrecognized gateway configuration')
    if config.get('nginx')!='/usr/sbin/nginx':return None
    state=_run(['systemctl','show','nginx.service','--property=LoadState','--value'],capture_output=True,text=True).stdout.strip()
    command=_run(['systemctl','show','nginx.service','--property=ExecStart','--value'],capture_output=True,text=True).stdout
    if state!='loaded' or '/usr/sbin/nginx' not in command:raise ValueError('System Nginx service does not match the configured gateway; preserve the existing web server')
    return 'nginx.service'

def enable_autostart(root,env):
    listener(env.get('HTTP_ADDR','127.0.0.1:8080'));listener(env.get('SHARE_ADDR','127.0.0.1:8091'))
    timers,missing=_helper_timers(root)
    nginx=_nginx_service(root)
    _run(['systemctl','enable','--now','docker.service'],capture_output=True)
    result=_run(['docker','container','inspect','starhub-control-db'],capture_output=True)
    row=_database_owner(env,json.loads(result.stdout))
    policy=row.get('HostConfig',{}).get('RestartPolicy',{}).get('Name')
    if policy not in ('unless-stopped','always'):
        _run(['docker','update','--restart','unless-stopped','starhub-control-db'],capture_output=True);policy='unless-stopped'
    if not row.get('State',{}).get('Running'):_run(['docker','start','starhub-control-db'],capture_output=True)
    _run(['systemctl','enable','--now','starhub-control.service'],capture_output=True)
    _wait_control(env)
    if nginx:_run(['systemctl','enable','--now',nginx],capture_output=True)
    for timer in timers:_run(['systemctl','enable','--now',timer],capture_output=True)
    return {'enabled':['docker.service','starhub-control.service']+([nginx] if nginx else [])+timers,'nginx':nginx or 'external_or_unconfigured','database':'starhub-control-db','database_restart':policy,'missing_helpers':missing,'complete':not missing,'note':'Systemd keeps installed panel services running after SSH disconnects. External Nginx/BaoTa stays with its existing supervisor. Missing helpers must be installed separately; oneshot services are triggered by timers.'}

def env_file(explicit=None):
    candidates = [Path(explicit)] if explicit else ([Path(os.environ.get('ProgramData','C:/ProgramData'))/'StarhubControl/control.env'] if os.name=='nt' else [Path('/etc/harbor/control.env'),Path('/opt/starhub-control/control.env')])
    found=[p for p in candidates if p.is_file() and not p.is_symlink()]
    if len(found)!=1: raise ValueError('Choose one installed control.env with --env')
    p=found[0]
    values=dict(line.split('=',1) for line in p.read_text(encoding='utf-8-sig').splitlines() if '=' in line and not line.startswith('#'))
    if len(values.get('ADMIN_TOKEN',''))<32 or not values.get('WEB_DIR'): raise ValueError('Not a recognized panel installation')
    root=Path('/opt/harbor') if p==Path('/etc/harbor/control.env') else p.parent
    if root.is_symlink() or not root.is_absolute(): raise ValueError('Installation root must not be symlinked')
    return root,values

def listener(value):
    from urllib.parse import urlsplit
    u=urlsplit('http://'+value)
    if u.username or u.password or u.path or u.query or u.fragment: raise ValueError('Invalid internal listener')
    ip=ipaddress.ip_address(u.hostname or '')
    if not u.port or not ip.is_loopback: raise ValueError('Existing internal API must stay on loopback')
    return value

def local_ip():
    try:
        with socket.socket(socket.AF_INET,socket.SOCK_DGRAM) as s:
            s.connect(('1.1.1.1',53)); return s.getsockname()[0]
    except OSError: return '127.0.0.1'

def ip_url(ip,port):
    ip=ipaddress.ip_address(ip)
    return 'http://'+('['+str(ip)+']' if ip.version==6 else str(ip))+':'+str(port)

def query(root,env):
    # Never send an administrator token through a proxy or non-loopback target.
    addr=listener(env.get('HTTP_ADDR','127.0.0.1:8080'))
    site={}
    try:
        request=urllib.request.Request('http://'+addr+'/api/v1/site',headers={'Authorization':'Bearer '+env['ADMIN_TOKEN']})
        with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request,timeout=4) as response: site=json.load(response)
    except (OSError,ValueError): pass
    domain=site.get('domain','')
    scheme='https' if site.get('https') else 'http'
    state={}
    p=root/'panel-access.json'
    if p.is_file() and not p.is_symlink():
        state=json.loads(p.read_text(encoding='utf-8'))
    active=False
    if os.name!='nt' and UNIT.exists():
        verify_unit(root)
        active=subprocess.run(['systemctl','is-active','--quiet',UNIT.name]).returncode==0
    return {'panel_url':scheme+'://'+domain if domain else site.get('ip_origin') or env.get('PUBLIC_ORIGIN',''),
            'subscription_domain':site.get('share_domain',''), 'web_listener':env.get('WEB_ADDR','127.0.0.1:5173'),
            'recovery':{'active':active,'url':state.get('url') if active else None,'expires_at':state.get('expires_at') if active else None},
            'ssh_tunnel':'ssh -L 18080:'+env.get('WEB_ADDR','127.0.0.1:5173')+' USER@VPS_IP ; then open http://127.0.0.1:18080',
            'note':'Configured addresses are shown without claiming DNS, upstream firewall or Internet reachability. Recovery is independent of domain settings.'}

def verify_unit(root):
    if UNIT.is_symlink(): raise ValueError('Recovery service must not be a symlink')
    content=UNIT.read_text(encoding='utf-8')
    if not content.startswith(MARKER) or 'ExecStart='+str(root/'bin/harbor-control')+' panel-web ' not in content:
        raise ValueError('Recovery service belongs to a different installation; nothing changed')

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action',nargs='?',choices=['status','enable-ip','disable-ip','disable-public','enable-autostart'],default='status')
    parser.add_argument('--env');parser.add_argument('--port',type=int,default=18081)
    parser.add_argument('--bind',default='0.0.0.0');parser.add_argument('--ip',help='IP displayed in the recovery URL; NAT address may need explicit setting')
    parser.add_argument('--minutes',type=int,default=30);parser.add_argument('--persistent',action='store_true')
    parser.add_argument('--allow-http',action='store_true');parser.add_argument('--foreground',action='store_true',help='Run until timeout/Ctrl+C; supports Windows/manual supervisors')
    args=parser.parse_args()
    root,env=env_file(args.env)
    if args.action=='status': print(json.dumps(query(root,env),ensure_ascii=False,indent=2));return
    if args.action in ('disable-public','enable-autostart'):
        if sys.platform!='linux' or os.geteuid()!=0 or args.foreground:raise ValueError('This action requires root on systemd Linux, without --foreground')
        cfg=_installation(root,args.env)
        with _maintenance_guard():result=disable_public(root,env,cfg) if args.action=='disable-public' else enable_autostart(root,env)
        print(json.dumps(result,ensure_ascii=False,indent=2));return
    if not args.foreground and (os.name=='nt' or os.geteuid()!=0): raise ValueError('Use root on systemd Linux, or --foreground on Windows/manual installs')
    if args.action=='disable-ip':
        if args.foreground: raise ValueError('Stop a foreground gateway with Ctrl+C')
        disable_recovery(root)
        print('Recovery entrance disabled. Normal panel and Agents unchanged.');return
    if not args.allow_http: raise ValueError('IP HTTP exposes traffic to network observers. Use an SSH tunnel, or explicitly add --allow-http on a trusted network')
    if not 1<=args.minutes<=1440 or not 1024<=args.port<=65535 or args.port in (8080,8091,15432,5432): raise ValueError('Invalid recovery lifetime/port')
    bind=ipaddress.ip_address(args.bind)
    binary=root/'bin'/('harbor-control.exe' if os.name=='nt' else 'harbor-control')
    directory=Path(env['WEB_DIR'])
    if not binary.is_file() or not (directory/'index.html').is_file(): raise ValueError('Existing binary/web build missing')
    for p in (root,binary,directory):
        if not re.fullmatch(r'[A-Za-z0-9_./:\\ -]+',str(p)): raise ValueError('Unsupported installation path')
        if os.name!='nt' and ' ' in str(p): raise ValueError('systemd recovery requires a path without spaces')
    if (root/'panel-access.json').is_symlink(): raise ValueError('Refusing symlinked state')
    safe_env={key:listener(env.get(key,default)) for key,default in [('HTTP_ADDR','127.0.0.1:8080'),('SHARE_ADDR','127.0.0.1:8091')]}
    with socket.socket(socket.AF_INET6 if bind.version==6 else socket.AF_INET,socket.SOCK_STREAM) as s:s.bind((str(bind),args.port))
    duration='0s' if args.persistent else str(args.minutes)+'m'
    address=('['+str(bind)+']' if bind.version==6 else str(bind))+':'+str(args.port)
    command=[str(binary),'panel-web','--addr',address,'--dir',str(directory),'--ttl',duration]
    url=ip_url(args.ip or (local_ip() if bind.is_unspecified else str(bind)),args.port)
    expires=None if args.persistent else (dt.datetime.now(dt.timezone.utc)+dt.timedelta(minutes=args.minutes)).isoformat()
    if args.foreground:
        print('Recovery '+url+'; lifetime '+duration,flush=True)
        subprocess.run(command,env={**os.environ,**safe_env},check=True);return
    if UNIT.exists(): verify_unit(root)
    contents=MARKER+'[Unit]\nDescription=Starhub temporary panel recovery\nAfter=network.target\n[Service]\nType=simple\n'
    contents+=''.join('Environment='+k+'='+v+'\n' for k,v in safe_env.items())
    # Both the binary TTL and systemd deadline bound temporary recovery.
    contents+='ExecStart='+' '.join(command)+'\nRestart=no\nNoNewPrivileges=true\nPrivateTmp=true\nProtectSystem=strict\nProtectHome=true\nMemoryMax=96M\nTasksMax=32\n'
    if not args.persistent: contents+='RuntimeMaxSec='+str(args.minutes*60)+'\n'
    contents+='[Install]\nWantedBy=multi-user.target\n'
    UNIT.write_text(contents,encoding='utf-8');UNIT.chmod(0o644)
    subprocess.run(['systemctl','daemon-reload'],check=True)
    # Temporary gateways never restart at boot. Persistent is explicit.
    subprocess.run(['systemctl','enable' if args.persistent else 'disable',UNIT.name],check=True)
    subprocess.run(['systemctl','restart',UNIT.name],check=True)
    state=root/'panel-access.json'
    if state.is_symlink(): raise ValueError('Refusing symlinked state')
    state.write_text(json.dumps({'url':url,'expires_at':expires}),encoding='utf-8');state.chmod(0o600)
    print(json.dumps({'url':url,'expires_at':expires,'persistent':args.persistent,'note':'Only this web port is needed in the host/provider firewall. No firewall rules were broadened automatically.'},indent=2))

if __name__=='__main__':
    try:main()
    except (OSError,ValueError,subprocess.CalledProcessError,subprocess.TimeoutExpired) as error:print(str(error),file=sys.stderr);sys.exit(1)

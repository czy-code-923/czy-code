"""Fixed Harbor Agent bootstrap. No SSH credentials, admin token or arbitrary commands."""
import hashlib,io,json,os,platform,re,shutil,subprocess,sys,tempfile,time,urllib.error,urllib.parse,urllib.request,zipfile
from pathlib import Path
rollback={'dirs':[],'unit':False,'complete':False}

def run(*args):return subprocess.run(args,check=True,stdout=subprocess.DEVNULL)
def main():
    if os.geteuid()!=0:raise ValueError('Run this installer as root.')
    origin,ticket=sys.argv[1:];u=urllib.parse.urlsplit(origin)
    if u.scheme!='https' or not u.hostname or u.path or u.port or u.username or u.query or u.fragment:raise ValueError('HTTPS origin required')
    if not re.fullmatch('[a-f0-9]{64}',ticket):raise ValueError('Invalid enrollment ticket')
    arch={'x86_64':'amd64','amd64':'amd64','aarch64':'arm64','arm64':'arm64'}.get(platform.machine().lower())
    if not arch:raise ValueError('Supported CPU architectures: amd64 and arm64')
    unit=Path('/etc/systemd/system/harbor-agent.service');root=Path('/opt/harbor-agent');env=Path('/etc/harbor-agent/agent.env')
    if unit.exists() or root.exists() or env.exists():raise ValueError('Existing Agent found. Installation refused; preserve it and use deliberate upgrade/reconnect.')
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self,*args):raise ValueError('Unexpected redirect refused')
    opener=urllib.request.build_opener(NoRedirect())
    def fetch(path,body=None,limit=160<<20):
        req=urllib.request.Request(origin+path,data=None if body is None else json.dumps(body).encode(),headers={'Content-Type':'application/json','User-Agent':'Harbor-Agent-Installer'})
        try:
            with opener.open(req,timeout=120) as r:
                data=r.read(limit+1)
                if len(data)>limit:raise ValueError('Download exceeds limit')
                return data
        except urllib.error.HTTPError as e:
            if e.code==401:raise ValueError('Registration command expired, revoked or already used. Generate a new command in the panel.') from None
            raise ValueError('Control server returned HTTP '+str(e.code)) from None
    print('1/4 Checking platform and downloading verified Agent + engines...',flush=True)
    path='/api/v1/agent/releases/linux-'+arch
    manifest=json.loads(fetch(path,limit=65536));archive=fetch(path+'?download=1')
    if len(archive)!=manifest['size'] or hashlib.sha256(archive).hexdigest()!=manifest['sha256']:raise ValueError('Package integrity verification failed')
    names={'harbor-agent','sing-box','frps','frpc','xray'}
    with tempfile.TemporaryDirectory(prefix='harbor-install-') as temporary:
        stage=Path(temporary)
        with zipfile.ZipFile(io.BytesIO(archive)) as z:
            if set(z.namelist())!=names or len(z.infolist())!=5 or sum(m.file_size for m in z.infolist())>400<<20:raise ValueError('Unexpected package contents')
            for name in names:
                p=stage/name;p.write_bytes(z.read(name));p.chmod(0o755)
        for binary,args in [('harbor-agent',[]),('sing-box',['version']),('frps',['--version']),('frpc',['--version']),('xray',['version'])]:
            if args:run(str(stage/binary),*args)
        print('2/4 Exchanging the single-use registration ticket...',flush=True)
        registration=json.loads(fetch('/api/v1/agent/enroll',{'ticket':ticket},65536))
        if not re.fullmatch('[a-f0-9]{32}',registration['host_id']) or not re.fullmatch('[a-f0-9]{64}',registration['agent_token']):raise ValueError('Invalid registration response')
        # Credentials are never printed and only enter a root-readable systemd environment file.
        os.umask(0o077)
        if subprocess.run(['id','harbor-agent'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode:run('useradd','--system','--home-dir','/var/lib/harbor-agent','--shell','/usr/sbin/nologin','harbor-agent')
        root.mkdir(mode=0o755);rollback['dirs'].append(root);root.chmod(0o755)
        env.parent.mkdir(mode=0o700);rollback['dirs'].append(env.parent)
        for name in names:shutil.copyfile(stage/name,root/name);(root/name).chmod(0o755)
        state=Path('/var/lib/harbor-agent');state.mkdir(mode=0o700);rollback['dirs'].append(state);run('chown','harbor-agent:harbor-agent',str(state))
        values={'CONTROL_URL':origin,'HOST_ID':registration['host_id'],'AGENT_TOKEN':registration['agent_token'],'AGENT_MODE':'simulation','ENABLE_LIVE':'1','LIVE_STATE_DIR':str(state/'live'),'GOMEMLIMIT':'96MiB','GOMAXPROCS':'1'}
        for name,prefix in [('sing-box','SING_BOX'),('frps','FRPS'),('frpc','FRPC'),('xray','XRAY')]:values[prefix+'_BINARY']=str(root/name);values[prefix+'_SHA256']=hashlib.sha256((root/name).read_bytes()).hexdigest()
        env.write_text(''.join(k+'='+v+'\n' for k,v in values.items()));env.chmod(0o600)
        with unit.open('x') as unit_file:unit_file.write('''[Unit]
Description=Harbor outbound Agent
After=network-online.target
Wants=network-online.target
[Service]
User=harbor-agent
Group=harbor-agent
EnvironmentFile=/etc/harbor-agent/agent.env
ExecStart=/opt/harbor-agent/harbor-agent
WorkingDirectory=/var/lib/harbor-agent
Restart=on-failure
RestartSec=10
KillMode=control-group
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/harbor-agent
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX AF_NETLINK
CapabilityBoundingSet=
UMask=0077
MemoryMax=384M
TasksMax=96
[Install]
WantedBy=multi-user.target
''')
        rollback['unit']=True
        print('3/4 Starting the unprivileged Agent service...',flush=True)
        run('systemctl','daemon-reload');run('systemctl','enable','--now','harbor-agent.service')
        time.sleep(2);run('systemctl','is-active','--quiet','harbor-agent.service')
        rollback['complete']=True
        print('4/4 Agent started. Return to the panel to confirm its heartbeat. No inbound management port was opened.')

def rollback_install():
    if not rollback['complete']:
        if rollback['unit']:
            subprocess.run(['systemctl','disable','--now','harbor-agent.service'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
            Path('/etc/systemd/system/harbor-agent.service').unlink(missing_ok=True)
        for directory in reversed(rollback['dirs']):shutil.rmtree(directory)
        if rollback['unit']:subprocess.run(['systemctl','daemon-reload'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)

if __name__=='__main__':
    try:main()
    except Exception as error:
        rollback_install()
        print('Installation failed: '+str(error),file=sys.stderr)
        print('New installation files were rolled back. Generate a new command in the panel and retry. Pre-existing Agents are never overwritten.',file=sys.stderr)
        sys.exit(1)

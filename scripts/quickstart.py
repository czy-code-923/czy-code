"""Install a fresh control plane from a verified binary distribution.
Requires Python 3 and an already running Docker Engine (Linux containers).
Never overwrites an installation, rotates credentials, or opens internal API/DB ports.
"""
import argparse,hashlib,io,zipfile,ipaddress,json,os,platform,secrets,shutil,socket,subprocess,sys,time,urllib.request
from pathlib import Path

def run(*args,**kwargs):return subprocess.run(args,check=True,**kwargs)
def free(port):
    with socket.socket() as s:s.bind(('127.0.0.1',port))
def detected_ip():
    try:
        with socket.socket(socket.AF_INET,socket.SOCK_DGRAM) as s:s.connect(('1.1.1.1',53));return s.getsockname()[0]
    except OSError:
        try:
            with socket.socket(socket.AF_INET6,socket.SOCK_DGRAM) as s:s.connect(('2606:4700:4700::1111',53));return '['+s.getsockname()[0]+']'
        except OSError:return '127.0.0.1'
def public_bind():
    try:
        with socket.socket(socket.AF_INET6,socket.SOCK_STREAM) as s:s.bind(('::',0));return '[::]'
    except OSError:return '0.0.0.0'
def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--prefix',default=str(Path(os.environ.get('ProgramData','C:/ProgramData'))/'StarhubControl') if os.name=='nt' else '/opt/starhub-control')
    parser.add_argument('--port',type=int,default=18080);parser.add_argument('--db-port',type=int,default=15432)
    parser.add_argument('--public',action='store_true',help='Explicitly allow authenticated web UI on all interfaces; HTTP has no transport encryption')
    parser.add_argument('--origin',help='Configured http(s)://IP:port or domain; DNS/TLS reverse proxy is configured separately')
    parser.add_argument('--foreground',action='store_true',help='Do not install service/task; suitable for OpenRC or supervised manual operation')
    args=parser.parse_args()
    from urllib.parse import urlsplit
    origin=(args.origin or 'http://'+(detected_ip() if args.public else '127.0.0.1')+':'+str(args.port)).rstrip('/')
    u=urlsplit(origin)
    if u.scheme not in ('http','https') or not u.hostname or u.username or u.path or u.query or u.fragment:raise ValueError('Invalid public origin')
    if not 1024<=args.port<=65535 or args.port in (8080,8091,5173,args.db_port) or not 1024<=args.db_port<=65535:raise ValueError('Invalid external ports')
    if os.name!='nt' and os.geteuid()!=0 and not args.foreground:raise ValueError('Service installation requires root; use sudo or --foreground')
    if os.name!='nt' and not args.foreground and not shutil.which('systemctl'):raise ValueError('systemd unavailable; use --foreground with your supervisor')
    if run('docker','info','--format','{{.OSType}}',capture_output=True,text=True).stdout.strip()!='linux':raise ValueError('A Linux-container Docker daemon is required')
    engine_host=os.environ.get('DOCKER_HOST') or run('docker','context','inspect','--format','{{.Endpoints.docker.Host}}',capture_output=True,text=True).stdout.strip()
    if not engine_host.startswith('unix://') and not (os.name=='nt' and engine_host.startswith('npipe://')):raise ValueError('Use a local Docker daemon, not a remote Docker context')
    run('docker','version',stdout=subprocess.DEVNULL)
    source=Path(__file__).resolve().parents[1];extension='.exe' if os.name=='nt' else ''
    if not (source/'bin'/('harbor-control'+extension)).is_file() or not (source/'web/dist/index.html').is_file():raise ValueError('Use the complete binary distribution with web/dist')
    root=Path(args.prefix).absolute()
    if any(c in str(root) for c in ('\n','\r','"',"'")) or (os.name!='nt' and any(c.isspace() for c in str(root))):raise ValueError('Unsupported installation path')
    if os.name!='nt' and not args.foreground and Path('/etc/systemd/system/starhub-control.service').exists():raise ValueError('Existing systemd service refused')
    if os.name=='nt' and not args.foreground and subprocess.run(['schtasks','/Query','/TN','StarhubControl'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:raise ValueError('Existing Windows task refused')
    if root.exists() or root.is_symlink():raise ValueError('Existing installation path refused; back up and follow the upgrade guide')
    for port in (8080,8091,args.port,args.db_port):free(port)
    container='starhub-control-db'
    if subprocess.run(['docker','volume','inspect','starhub-control-pgdata'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:raise ValueError('Existing database volume refused; follow the backup/upgrade guide')
    existing=subprocess.run(['docker','container','inspect',container],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    if existing.returncode==0:raise ValueError('Existing starhub-control-db container refused')
    os.umask(0o077);root.mkdir(parents=True,mode=0o700)
    if os.name=='nt':
        identity=run('whoami',capture_output=True,text=True).stdout.strip()
        run('icacls',str(root),'/inheritance:r','/grant:r',identity+':(OI)(CI)F','*S-1-5-18:(OI)(CI)F','*S-1-5-32-544:(OI)(CI)F',stdout=subprocess.DEVNULL)
    shutil.copyfile(source/'scripts/panel-access.py',root/'panel-access.py');shutil.copyfile(source/'scripts/uninstall.py',root/'uninstall.py');shutil.copyfile(source/'scripts/admin-token.py',root/'admin-token.py');shutil.copytree(source/'bin',root/'bin');shutil.copytree(source/'web/dist',root/'web')
    if os.name!='nt':
        for binary in (root/'bin').iterdir():binary.chmod(0o755)
    distribution=json.loads((source/'scripts/quickstart-distribution.json').read_text(encoding='utf-8'))
    with urllib.request.urlopen(distribution['agent_url'],timeout=180) as response:archive=response.read(200<<20)
    if len(archive)!=distribution['agent_size'] or hashlib.sha256(archive).hexdigest()!=distribution['agent_sha256']:raise ValueError('Agent package verification failed; no services started')
    agent_dir=root/'agent-releases';agent_dir.mkdir(mode=0o755);agent_dir.chmod(0o755)
    with zipfile.ZipFile(io.BytesIO(archive)) as z:
        if set(z.namelist())!={'manifest.json','linux-amd64.zip','linux-arm64.zip'}:raise ValueError('Unexpected Agent release archive')
        for name in z.namelist():(agent_dir/name).write_bytes(z.read(name));(agent_dir/name).chmod(0o644)
    password=secrets.token_hex(24);admin=secrets.token_hex(32);master=secrets.token_hex(32)
    pg=root/'postgres.env';pg.write_text('POSTGRES_DB=harbor\nPOSTGRES_USER=harbor\nPOSTGRES_PASSWORD='+password+'\n',encoding='utf-8');pg.chmod(0o600)
    env={'ADMIN_TOKEN':admin,'MASTER_KEY':master,'DATABASE_URL':f'postgres://harbor:{password}@127.0.0.1:{args.db_port}/harbor?sslmode=disable','HTTP_ADDR':'127.0.0.1:8080','SHARE_ADDR':'127.0.0.1:8091','WEB_ADDR':(public_bind() if args.public else '127.0.0.1')+':'+str(args.port),'WEB_DIR':str(root/'web'),'PUBLIC_ORIGIN':origin,'AGENT_RELEASE_DIR':str(agent_dir),'GOMEMLIMIT':'128MiB','GOMAXPROCS':'1'}
    private=root/'control.env';private.write_text(''.join(k+'='+v+'\n' for k,v in env.items()),encoding='utf-8');private.chmod(0o600)
    run('docker','run','--detach','--name',container,'--restart','unless-stopped','--memory','384m','--pids-limit','100','--publish',f'127.0.0.1:{args.db_port}:5432','--env-file',str(pg),'--volume','starhub-control-pgdata:/var/lib/postgresql/data','postgres:17-alpine','postgres','-c','shared_buffers=64MB','-c','max_connections=30','-c','work_mem=2MB',stdout=subprocess.DEVNULL)
    for _ in range(60):
        if subprocess.run(['docker','exec',container,'pg_isready','-U','harbor','-d','harbor'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
        time.sleep(1)
    else:raise RuntimeError('PostgreSQL startup timed out; installation retained for diagnosis')
    print('Credentials saved privately in '+str(private)+'. Read ADMIN_TOKEN locally; never publish it.')
    if args.foreground:run(str(root/'bin'/('harbor-control'+extension)),env={**os.environ,**env});return
    if os.name=='nt':
        launcher=root/'run-control.ps1';launcher.write_text("$ErrorActionPreference='Stop'\nGet-Content -LiteralPath '"+str(private)+"' | ForEach-Object { if ($_ -match '^([A-Z_]+)=(.*)$') {[Environment]::SetEnvironmentVariable($Matches[1],$Matches[2],'Process')} }\n& '"+str(root/'bin/harbor-control.exe')+"'\n",encoding='utf-8')
        # Runs only as the current user at logon; no privileged Windows service is claimed.
        run('schtasks','/Create','/TN','StarhubControl','/SC','ONLOGON','/RL','LIMITED','/TR','powershell.exe -NoProfile -File "'+str(launcher)+'"')
        run('schtasks','/Run','/TN','StarhubControl')
    else:
        if subprocess.run(['id','starhub-control'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode:run('useradd','--system','--home-dir',str(root),'--shell','/usr/sbin/nologin','starhub-control')
        root.chmod(0o755);private.chmod(0o640);run('chgrp','starhub-control',str(private))
        for directory,dirs,files in os.walk(root/'web'):
            Path(directory).chmod(0o755)
            for file in files:(Path(directory)/file).chmod(0o644)
        for binary in (root/'bin').iterdir():binary.chmod(0o755)
        (root/'bin').chmod(0o755)
        unit=Path('/etc/systemd/system/starhub-control.service')
        if unit.exists():raise ValueError('Existing starhub-control unit refused')
        unit.write_text('[Unit]\nDescription=Starhub VPS control plane\nAfter=network-online.target docker.service\n[Service]\nUser=starhub-control\nGroup=starhub-control\nEnvironmentFile='+str(private)+'\nExecStart='+str(root/'bin/harbor-control')+'\nRestart=on-failure\nRestartSec=5\nNoNewPrivileges=true\nPrivateTmp=true\nProtectSystem=strict\nProtectHome=true\nUMask=0077\nMemoryMax=192M\n[Install]\nWantedBy=multi-user.target\n',encoding='utf-8');unit.chmod(0o644)
        run('systemctl','daemon-reload');run('systemctl','enable','--now','starhub-control')
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
    for _ in range(30):
        try:
            with opener.open('http://127.0.0.1:'+str(args.port)+'/healthz',timeout=2) as r:
                if r.status==200:print('Ready: '+origin+' (API and PostgreSQL remain loopback-only)');return
        except OSError:time.sleep(1)
    raise RuntimeError('Web health check timed out; inspect service/task. Data and credentials retained.')
if __name__=='__main__':
    try:main()
    except Exception as e:print('Installation stopped: '+str(e),file=sys.stderr);sys.exit(1)

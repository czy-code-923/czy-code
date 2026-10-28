"""Query panel addresses, or explicitly open/close a recovery web entrance.

The recovery entrance is OFF by default. enable-ip requires --allow-http because
IP HTTP is not encrypted. Default lifetime is 30 minutes, including after reboot.
Use an SSH tunnel instead on untrusted networks. No API/DB ports are opened.
"""
import argparse, datetime as dt, ipaddress, json, os, re, socket, subprocess, sys, urllib.request
from pathlib import Path

UNIT = Path('/etc/systemd/system/starhub-panel-rescue.service')
MARKER = '# Starhub owned panel recovery gateway\n'

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
    parser.add_argument('action',nargs='?',choices=['status','enable-ip','disable-ip'],default='status')
    parser.add_argument('--env');parser.add_argument('--port',type=int,default=18081)
    parser.add_argument('--bind',default='0.0.0.0');parser.add_argument('--ip',help='IP displayed in the recovery URL; NAT address may need explicit setting')
    parser.add_argument('--minutes',type=int,default=30);parser.add_argument('--persistent',action='store_true')
    parser.add_argument('--allow-http',action='store_true');parser.add_argument('--foreground',action='store_true',help='Run until timeout/Ctrl+C; supports Windows/manual supervisors')
    args=parser.parse_args()
    root,env=env_file(args.env)
    if args.action=='status': print(json.dumps(query(root,env),ensure_ascii=False,indent=2));return
    if not args.foreground and (os.name=='nt' or os.geteuid()!=0): raise ValueError('Use root on systemd Linux, or --foreground on Windows/manual installs')
    if args.action=='disable-ip':
        if args.foreground: raise ValueError('Stop a foreground gateway with Ctrl+C')
        if UNIT.exists():
            verify_unit(root)
            subprocess.run(['systemctl','disable','--now',UNIT.name],check=True)
            UNIT.unlink();subprocess.run(['systemctl','daemon-reload'],check=True)
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
    except (OSError,ValueError,subprocess.CalledProcessError) as error:print(str(error),file=sys.stderr);sys.exit(1)

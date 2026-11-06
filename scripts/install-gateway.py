"""Install bounded Linux gateway helpers for an existing Starhub control plane.

No credentials are generated, displayed or sent outside the loopback control API.
Existing Nginx/BaoTa configurations are detected and preserved. Standalone repair
downloads only reviewed helpers from an exact, checksum-verified official release.
"""
import argparse,hashlib,importlib.util,io,ipaddress,json,os,re,shutil,socket,stat,subprocess,sys,tempfile,urllib.parse,urllib.request,zipfile
from pathlib import Path,PurePosixPath

VERSION='0.25.5'
REPO='czy-code-923/czy-code'
FILES=('panel-updater.py','install-gateway.py','gateway-worker.py','domain-sync.py','managed-gateway.py','gateway-config.py')
MARKER='# starhub-owned-gateway-v1\n'

def run(*argv,**kw):return subprocess.run(list(argv),check=True,timeout=kw.pop('timeout',180),**kw)

def module(name,path):
    spec=importlib.util.spec_from_file_location(name,path);obj=importlib.util.module_from_spec(spec);spec.loader.exec_module(obj);return obj

class OfficialRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,req,fp,code,msg,headers,url):
        p=urllib.parse.urlsplit(url)
        if p.scheme!='https' or p.hostname not in ('github.com','api.github.com','release-assets.githubusercontent.com','objects.githubusercontent.com') or p.username or p.port:raise ValueError('Unexpected release redirect')
        return super().redirect_request(req,fp,code,msg,headers,url)

def source_release(directory):
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),OfficialRedirect())
    url='https://api.github.com/repos/'+REPO+'/releases/tags/v'+VERSION+'-distribution'
    with opener.open(urllib.request.Request(url,headers={'User-Agent':'Starhub-gateway-repair'}),timeout=30) as r:data=r.read((1<<20)+1)
    if len(data)>1<<20:raise ValueError('Oversized release metadata')
    release=json.loads(data);name='harbor-'+VERSION+'-source.zip'
    if release.get('tag_name')!='v'+VERSION+'-distribution' or release.get('draft') or release.get('prerelease'):raise ValueError('Stable gateway repair release required')
    assets=[a for a in release['assets'] if a['name']==name]
    if len(assets)!=1:raise ValueError('Official source package missing')
    asset=assets[0];url='https://github.com/'+REPO+'/releases/download/v'+VERSION+'-distribution/'+name
    if asset.get('browser_download_url')!=url or not 0<asset.get('size',0)<=20<<20 or not re.fullmatch(r'sha256:[a-f0-9]{64}',asset.get('digest','')):raise ValueError('Source identity/checksum missing')
    with opener.open(urllib.request.Request(url,headers={'User-Agent':'Starhub-gateway-repair'}),timeout=90) as r:data=r.read(asset['size']+1)
    if len(data)!=asset['size'] or 'sha256:'+hashlib.sha256(data).hexdigest()!=asset['digest']:raise ValueError('Source checksum mismatch')
    with zipfile.ZipFile(io.BytesIO(data)) as z:
        if len(z.infolist())>5000 or sum(m.file_size for m in z.infolist())>50<<20:raise ValueError('Source archive exceeds bounds')
        for name in FILES:
            member='harbor-'+VERSION+'/scripts/'+name
            info=z.getinfo(member)
            if info.file_size>256<<10 or stat.S_ISLNK(info.external_attr>>16):raise ValueError('Unsafe helper member')
            (directory/name).write_bytes(z.read(member))
    return directory

def nginx_path():
    # Prefer BaoTa's existing installation; never start a competing web server.
    return next((Path(p) for p in ('/www/server/nginx/sbin/nginx','/usr/sbin/nginx','/usr/local/nginx/sbin/nginx') if Path(p).is_file()),None)

def plugin_available(certbot):
    if certbot is None:return False
    r=run(str(certbot),'plugins','--text',capture_output=True,text=True,timeout=45)
    return 'dns-cloudflare' in r.stdout

def check_web_ports():
    for port in (80,443):
        for family,address in ((socket.AF_INET,'0.0.0.0'),(socket.AF_INET6,'::')):
            try:
                with socket.socket(family,socket.SOCK_STREAM) as s:
                    if family==socket.AF_INET6:s.setsockopt(socket.IPPROTO_IPV6,socket.IPV6_V6ONLY,1)
                    s.bind((address,port))
            except OSError as e:
                if family==socket.AF_INET6 and e.errno in (97,99):continue
                raise ValueError('Port '+str(port)+' is already used by another service; specify the existing Nginx instead of installing a competing server') from None

def ensure_dependencies(install):
    nginx=nginx_path();certbot=Path('/usr/bin/certbot') if Path('/usr/bin/certbot').is_file() else None
    cloudflare=plugin_available(certbot)
    if nginx and certbot and cloudflare:return nginx,certbot
    if not install:raise ValueError('Nginx, certbot and its DNS Cloudflare plugin are required; rerun with --install-deps or install them with the OS package manager')
    if not nginx:check_web_ports()
    packages=([] if nginx else ['nginx'])+([] if certbot else ['certbot'])+([] if cloudflare else ['python3-certbot-dns-cloudflare'])
    if shutil.which('apt-get'):
        env={**os.environ,'DEBIAN_FRONTEND':'noninteractive'}
        run('apt-get','update',env=env,timeout=600);run('apt-get','install','-y',*packages,env=env,timeout=900)
    elif shutil.which('dnf') or shutil.which('yum'):
        run(shutil.which('dnf') or shutil.which('yum'),'install','-y',*packages,timeout=900)
    else:raise ValueError('Install nginx, certbot and the DNS Cloudflare plugin with this distribution package manager, then retry without --install-deps')
    nginx=nginx_path();certbot=Path('/usr/bin/certbot') if Path('/usr/bin/certbot').is_file() else None
    if not nginx or not certbot or not plugin_available(certbot):raise ValueError('Gateway dependencies are incomplete; the control plane and data have been preserved')
    return nginx,certbot

def public_listener(values):
    host=values.get('WEB_ADDR','127.0.0.1:18080').rsplit(':',1)[0].strip('[]')
    try:return not ipaddress.ip_address(host).is_loopback
    except ValueError:raise ValueError('Unrecognized existing WEB_ADDR')

def unit_text(scripts,config):
    return MARKER+'[Unit]\nDescription=Starhub certificate and domain gateway\nAfter=network-online.target\n[Service]\nType=oneshot\nUser=root\nUMask=0077\nExecStart=/usr/bin/python3 '+str(scripts/'gateway-worker.py')+' --config '+str(config)+'\nTimeoutStartSec=1800\nMemoryMax=256M\nNice=10\nNoNewPrivileges=true\nPrivateTmp=true\n'

def install_gateway(env,source,install_deps=False,enable_acme=False):
    if sys.platform!='linux' or os.geteuid()!=0 or not shutil.which('systemctl'):raise ValueError('Linux root with systemd required')
    helper=module('gateway_install_validation',source/'panel-updater.py');installation=helper.load_install(Path(env))
    root=installation['root'];scripts=root/'scripts';config=root/'gateway.json'
    # A legacy helper may already own these same domains. Never add a second owner.
    for name in ('harbor-domain-sync.timer','harbor-managed-gateway.timer'):
        if subprocess.run(['systemctl','is-active','--quiet',name],timeout=10).returncode==0:raise ValueError('An existing '+name+' already manages this gateway; preserve it instead of adding another worker')
    for p in (root,scripts,Path(env)):
        if any(c.isspace() or c in "'\"%" for c in str(p)):raise ValueError('Unsafe systemd path')
    units=Path('/etc/systemd/system');targets=[units/'starhub-gateway.service',units/'starhub-gateway.timer']
    for p in targets:
        if p.exists() and (p.is_symlink() or not p.read_text().startswith(MARKER)):raise ValueError('Existing unowned gateway unit refused')
    if config.exists():
        helper.trusted(config)
        if config.stat().st_mode&0o077 or json.loads(config.read_text()).get('owner')!='starhub-gateway-v1':raise ValueError('Existing unowned gateway configuration refused')
    for p in targets:
        if p.exists():helper.trusted(p)
    if targets[0].exists():
        state=run('systemctl','show','starhub-gateway.service','--property=ActiveState','--value',capture_output=True,text=True).stdout.strip()
        if state in ('active','activating','deactivating'):raise ValueError('Gateway is processing a request; wait for it to finish before repairing')
    nginx,certbot=ensure_dependencies(install_deps)
    sync=module('gateway_install_sync',source/'domain-sync.py');sync.trusted_binary(str(nginx));sync.trusted_binary(str(certbot));sync.trusted_binary(str(root/'bin/harbor-gateway'))
    vhosts=Path('/www/server/panel/vhost/nginx') if str(nginx).startswith('/www/server/') else Path('/etc/nginx/conf.d')
    if not vhosts.is_dir():raise ValueError('Recognized Nginx include directory missing')
    sync.verify_output(vhosts/'starhub-management.conf')
    dumped=run(str(nginx),'-T',capture_output=True,text=True)
    if not re.search(r'include\s+'+re.escape(str(vhosts))+r'/\*\.conf\s*;',dumped.stdout+dumped.stderr):raise ValueError('Nginx does not include the recognized vhost directory; no configuration was modified')
    values=dict(line.split('=',1) for line in Path(env).read_text().splitlines() if '=' in line and not line.startswith('#'))
    helper.local_api(installation['token'],'site')
    if scripts.exists():helper.trusted(scripts,True)
    else:scripts.mkdir(mode=0o755)
    for directory in (Path('/etc/harbor'),Path('/var/lib/starhub-gateway'),Path('/var/lib/starhub-gateway/logs'),Path('/var/www/harbor-acme')):
        if directory.exists():helper.trusted(directory,True)
        else:directory.mkdir(parents=True,mode=0o755 if 'www' in directory.parts else 0o700)
        # quickstart deliberately uses umask 077 for credentials. ACME's public
        # webroot must still be traversable by the unprivileged Nginx worker.
        if directory==Path('/var/www/harbor-acme'):directory.chmod(0o755)
    touched=[scripts/name for name in FILES if name!='panel-updater.py']+[config]+targets
    before={}
    for path in touched:
        if path.exists():
            helper.trusted(path);before[path]=(path.read_bytes(),path.stat().st_mode&0o777)
        else:before[path]=None
    timer_active=subprocess.run(['systemctl','is-active','--quiet','starhub-gateway.timer'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=10).returncode==0
    if timer_active:run('systemctl','stop','starhub-gateway.timer')
    try:
        # Copy complete helper set before enabling its timer. Preserve the installed updater.
        for name in FILES:
            if name=='panel-updater.py':continue
            target=scripts/name
            if source/name!=target:helper.atomic_copy(source/name,target)
            target.chmod(0o755);os.chown(target,0,0)
        settings={'owner':'starhub-gateway-v1','env':str(env),'nginx':str(nginx),'vhost_dir':str(vhosts),'certbot':str(certbot),'gateway':str(root/'bin/harbor-gateway'),'web_port':int(values.get('WEB_ADDR','127.0.0.1:5173').rsplit(':',1)[1]),'enable_acme':bool(enable_acme),'public_management':public_listener(values),'allow_http':public_listener(values)}
        helper.write_json(config,settings)
        targets[0].write_text(unit_text(scripts,config))
        targets[1].write_text(MARKER+'[Unit]\nDescription=Starhub gateway reconciliation queue\n[Timer]\nOnBootSec=15s\nOnUnitInactiveSec=60s\nAccuracySec=5s\nUnit=starhub-gateway.service\n[Install]\nWantedBy=timers.target\n')
        for p in targets:p.chmod(0o644)
        run(str(nginx),'-t',capture_output=True)
        if not Path('/www/server/nginx/sbin/nginx').is_file():run('systemctl','enable','--now','nginx.service')
        run('systemctl','daemon-reload');run('systemctl','enable','--now','starhub-gateway.timer')
        run('systemctl','start','--no-block','starhub-gateway.service')
    except BaseException:
        subprocess.run(['systemctl','stop','starhub-gateway.timer','starhub-gateway.service'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=30)
        for path,old in before.items():
            if old is None:path.unlink(missing_ok=True)
            else:path.write_bytes(old[0]);path.chmod(old[1])
        subprocess.run(['systemctl','daemon-reload'],timeout=30)
        if timer_active:subprocess.run(['systemctl','enable','--now','starhub-gateway.timer'],timeout=30)
        raise
    print('Gateway helpers installed. Existing credentials, database, Agents and other Nginx sites were preserved. Check: systemctl status starhub-gateway.timer; journalctl -u starhub-gateway.service -n 60 --no-pager')

def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--env',default='/opt/starhub-control/control.env');p.add_argument('--install-deps',action='store_true');p.add_argument('--enable-acme',action='store_true',help='Allow configured domain certificate issuance and renewal')
    args=p.parse_args()
    if sys.platform!='linux' or os.geteuid()!=0:raise ValueError('Run as Linux root; root does not need sudo')
    with tempfile.TemporaryDirectory(prefix='starhub-gateway-repair-') as directory:
        source=source_release(Path(directory));install_gateway(Path(args.env),source,args.install_deps,args.enable_acme)

if __name__=='__main__':
    try:main()
    except Exception as e:print('Gateway setup stopped: '+str(e),file=sys.stderr);sys.exit(1)

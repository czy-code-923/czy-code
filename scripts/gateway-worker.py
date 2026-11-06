"""One serialized root worker for certificates, domain gateways and reverse proxies."""
import argparse,fcntl,importlib.util,json,os,re,subprocess,sys,urllib.error
from pathlib import Path

def module(name,path):
    spec=importlib.util.spec_from_file_location(name,path);m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m);return m

def report_runtime(helper,token,nginx,certbot):
    version=subprocess.run([nginx,'-v'],capture_output=True,check=True).stderr
    match=re.search(rb'nginx/([0-9]+\.[0-9]+\.[0-9]+)',version)
    if not match:raise ValueError('Unrecognized Nginx version')
    plugins=subprocess.run([certbot,'plugins','--text'],capture_output=True,text=True,timeout=45)
    report={'version':match.group(1).decode(),'source':'baota' if nginx.startswith('/www/server/') else 'system','certbot_available':plugins.returncode==0,'cloudflare_available':plugins.returncode==0 and 'dns-cloudflare' in plugins.stdout}
    try:helper.local_api(token,'gateway/nginx',report)
    except urllib.error.HTTPError as e:
        if e.code!=400:raise
        # Preserve compatibility when repairing an already installed 0.25.x control plane.
        helper.local_api(token,'gateway/nginx',{k:report[k] for k in ('version','source')})

def roles(site):
    result=[]
    if site.get('domain'):result.append(('management',site['domain'],site['https']))
    share=site.get('share_domain','')
    if share and share!=site.get('domain'):result.append(('subscription',share,site.get('share_https',True)))
    else:result.append(('subscription','',True))
    return result

def retire_management(config,site,sync):
    if site.get('domain'):return
    out=Path(config['vhost_dir'])/'starhub-management.conf'
    sync.verify_output(out)
    if not out.exists():return
    old=out.read_bytes()
    if not old.startswith(b'# harbor-managed-v1:'):raise ValueError('Unowned management vhost refused')
    out.unlink()
    try:sync.run([config['nginx'],'-t']);sync.run([config['nginx'],'-s','reload'])
    except Exception:out.write_bytes(old);raise
    state=out.with_suffix('.conf.state.json')
    if state.is_symlink():raise ValueError('Unsafe gateway state link')
    state.unlink(missing_ok=True)

def bootstrap_http(config,site,certs,sync):
    temporary=[]
    for role,domain,https in roles(site):
        cert=next((c for c in certs if c['domain']==domain and not c.get('deleted_at')),None)
        if not https or not cert or cert['provider']!='http-01' or cert['state'] not in ('pending','running'):continue
        sync.domain_name(domain)
        out=Path(config['vhost_dir'])/('starhub-management.conf' if role=='management' else 'starhub-management.subscription.conf')
        if out.exists():
            original=out.read_bytes()
            if not original.startswith(b'# harbor-managed-v1:'):raise ValueError('Unowned management vhost refused')
            if re.search(rb'server_name\s+'+re.escape(domain.encode())+rb'\s*;',original):continue
            # A renamed domain gets a temporary challenge vhost while the old
            # valid site stays available. Remove it before applying the new TLS site.
            out=out.parent/('starhub-acme-'+domain+'.conf');temporary.append(out)
            if out.exists() and not out.read_bytes().startswith(b'# harbor-managed-v1: ACME bootstrap'):raise ValueError('Unowned challenge vhost refused')
        # Do not shadow another application's hostname, including BaoTa sites.
        managed=module('gateway_worker_managed',Path(__file__).with_name('managed-gateway.py'))
        managed.ensure_domain_available(out.parent,domain,out)
        data=('# harbor-managed-v1: ACME bootstrap\nserver { listen 80; listen [::]:80; server_name '+domain+'; location ^~ /.well-known/acme-challenge/ { root /var/www/harbor-acme; } location / { return 503; } }\n').encode()
        sync.verify_output(out)
        out.write_bytes(data);out.chmod(0o644)
        try:sync.run([config['nginx'],'-t']);sync.run([config['nginx'],'-s','reload'])
        except Exception:
            out.unlink();raise
    return temporary

def reconcile(config,helper):
    scripts=Path(__file__).resolve().parent
    installation=helper.load_install(Path(config['env']));token=installation['token']
    sync=module('gateway_worker_sync',scripts/'domain-sync.py')
    for executable in (config['nginx'],config['certbot'],config['gateway']):sync.trusted_binary(executable)
    call=lambda path,body=None:helper.local_api(token,path,body)
    site=call('site');certs=call('certificates')
    report_runtime(helper,token,config['nginx'],config['certbot'])
    retire_management(config,site,sync)
    temporary=bootstrap_http(config,site,certs,sync) if config['enable_acme'] else []
    if config['enable_acme']:
        try:subprocess.run([sys.executable,str(scripts/'managed-gateway.py'),'--env',config['env'],'--nginx',config['nginx'],'--vhost-dir',config['vhost_dir'],'--certbot',config['certbot']],check=True,timeout=900)
        finally:
            for path in temporary:
                sync.verify_output(path)
                if path.exists() and path.read_bytes().startswith(b'# harbor-managed-v1: ACME bootstrap'):path.unlink()
            if temporary:sync.run([config['nginx'],'-t']);sync.run([config['nginx'],'-s','reload'])
    certs=call('certificates');failures=[]
    for role,domain,https in roles(site):
        cert=next((c for c in certs if c['domain']==domain and not c.get('deleted_at')),None)
        if https and cert and cert['state']!='ready':
            failures.append('certificate_not_ready')
            call('site/gateway-status',{'role':role,'domain':domain,'https':https,'state':'error'})
            continue
        argv=[sys.executable,str(scripts/'domain-sync.py'),'--env',config['env'],'--gateway',config['gateway'],'--nginx',config['nginx'],'--out',str(Path(config['vhost_dir'])/'starhub-management.conf'),'--admin-cidrs','127.0.0.1/32,::1/128','--certbot',config['certbot'],'--web-port',str(config.get('web_port',5173)),'--role',role,'--apply']
        # Explicit Cloudflare requests never fall back to competing HTTP issuance.
        if config['enable_acme'] and cert is None:argv.append('--issue')
        if config['public_management']:argv.append('--public-management')
        if config['allow_http']:argv.append('--allow-http')
        if subprocess.run(argv,timeout=450).returncode:failures.append('domain_reconciliation_failed')
    report_runtime(helper,token,config['nginx'],config['certbot'])
    if failures:raise ValueError(','.join(sorted(set(failures))))

def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--config',required=True);args=p.parse_args()
    if sys.platform!='linux' or os.geteuid()!=0:raise ValueError('Linux root required')
    helper=module('gateway_worker_validation',Path(__file__).with_name('panel-updater.py'));path=helper.trusted(Path(args.config))
    if path.stat().st_mode&0o077:raise ValueError('Private gateway configuration required')
    config=json.loads(path.read_text())
    if config.get('owner')!='starhub-gateway-v1':raise ValueError('Unrecognized gateway configuration')
    lock=os.open('/run/starhub-gateway.lock',os.O_CREAT|os.O_RDWR|getattr(os,'O_NOFOLLOW',0),0o600)
    try:
        fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB);reconcile(config,helper)
    finally:os.close(lock)

if __name__=='__main__':
    try:main()
    except Exception as e:print('Gateway worker needs attention: '+str(e),file=sys.stderr);sys.exit(1)

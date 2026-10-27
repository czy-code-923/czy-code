"""Linux gateway reconciler: typed site settings -> dedicated nginx vhost, with rollback.
Dry-run by default. Explicit --apply permits writing this one managed vhost and reload.
"""
import argparse
import fcntl
import ipaddress
import json
import os
from pathlib import Path
import re
import stat
import time
import tempfile
import subprocess
import urllib.request

def domain_name(value):
    if not isinstance(value,str) or len(value)>253 or not all(re.fullmatch(r'[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?',p) for p in value.split('.')):
        raise ValueError('Invalid DNS name')
    return value

def trusted_binary(path):
    p=Path(path)
    if not p.is_absolute(): raise ValueError('Executable paths must be absolute')
    for candidate in (p,p.resolve()):
        for item in (candidate,*candidate.parents):
            st=item.stat()
            if st.st_uid!=0 or st.st_mode & (stat.S_IWGRP|stat.S_IWOTH):
                raise ValueError('Executables and parents must be root owned and not group/world writable')
    return str(p)

def verify_output(output):
    if not output.is_absolute() or output.is_symlink(): raise ValueError('Safe absolute managed vhost output required')
    for parent in (output.parent,*output.parent.parents):
        st=parent.stat()
        if st.st_uid!=0 or st.st_mode & (stat.S_IWGRP|stat.S_IWOTH): raise ValueError('Unsafe vhost parent permissions')

def run(argv):
    # Fixed executables and argument arrays; no shell or remote URLs are accepted.
    result=subprocess.run(argv,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,timeout=180,check=True)
    if len(result.stdout)>1024*1024: raise ValueError('Output too large')
    return result.stdout

def main(subscription=False):
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--env',required=True)
    parser.add_argument('--gateway',required=True)
    parser.add_argument('--nginx',required=True)
    parser.add_argument('--out',required=True)
    parser.add_argument('--admin-cidrs',required=True)
    parser.add_argument('--certbot',default='/usr/bin/certbot')
    parser.add_argument('--issue',action='store_true',help='Explicitly allow ACME issuance/renewal through certbot')
    parser.add_argument('--apply',action='store_true')
    parser.add_argument('--allow-http',action='store_true',help='Explicitly allow plaintext subscription endpoint')
    parser.add_argument('--public-management',action='store_true',help='Allow any source to reach HTTPS management; admin token remains required')
    args=parser.parse_args()
    if os.geteuid()!=0: raise ValueError('Gateway reconciler requires Linux root')
    values={}
    for line in Path(args.env).read_text().splitlines():
        if '=' in line and not line.startswith('#'):
            k,v=line.split('=',1);values[k]=v
    token=values['ADMIN_TOKEN']
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self,req,fp,code,msg,headers,newurl):
            raise ValueError('Control redirects forbidden')
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect())
    def call(path,body=None):
        data=None if body is None else json.dumps(body).encode()
        request=urllib.request.Request('http://127.0.0.1:8080/api/v1/'+path,data=data,headers={'Authorization':'Bearer '+token,'Content-Type':'application/json'})
        with opener.open(request,timeout=10) as response:
            if response.geturl()!=request.full_url: raise ValueError('Control redirects forbidden')
            return json.loads(response.read(65536))
    site=call('site')
    ip_port=None
    if subscription:
        args.out=str(Path(args.out).with_name(Path(args.out).stem+'.subscription.conf'))
        domain=site.get('share_domain','')
        if not domain or domain==site.get('domain'):
            old=Path(args.out)
            verify_output(old)
            if old.exists():
                if not old.read_bytes().startswith(b'# harbor-managed-v1:'): raise ValueError('Unowned subscription vhost')
                previous=old.read_bytes();old.unlink()
                try:run([trusted_binary(args.nginx),'-t']);run([trusted_binary(args.nginx),'-s','reload'])
                except BaseException:old.write_bytes(previous);raise
            call('site/gateway-status',{'role':'subscription','domain':'','https':True,'state':'inherited'})
            return
        domain=domain_name(domain);https=site.get('share_https',True)
    elif not site['domain']:
        from urllib.parse import urlsplit
        parsed=urlsplit(site['ip_origin']);ipaddress.ip_address(parsed.hostname)
        if parsed.scheme!='http' or not parsed.port or parsed.port in (80,443,8080,8091,5173) or parsed.port<1024:raise ValueError('IP gateway needs explicit HTTP port >=1024, outside internal ports')
        domain=parsed.hostname;ip_port=parsed.port;https=False
    else:domain=domain_name(site['domain']);https=site['https']
    if not isinstance(https,bool): raise ValueError('Invalid HTTPS setting')
    if not https and not args.allow_http: raise ValueError('HTTP refused; review risk and explicitly use --allow-http')
    # Plain HTTP is an explicit site setting and additionally requires --allow-http.
    for cidr in args.admin_cidrs.split(','):
        network=ipaddress.ip_network(cidr,strict=False)
        if network.prefixlen==0: raise ValueError('Public management CIDR forbidden')
    output=Path(args.out)
    verify_output(output)
    gateway=trusted_binary(args.gateway);nginx=trusted_binary(args.nginx)
    cert=Path('/etc/letsencrypt/live')/domain/'fullchain.pem';key=cert.parent/'privkey.pem'
    argv=[gateway,'--domain',('127.0.0.1' if ip_port else domain),'--admin-cidrs',args.admin_cidrs,'--https='+str(https).lower()]
    if subscription: argv+=['--share-only']
    if ip_port:argv+=['--listen-port',str(ip_port)]
    if https: argv+=['--cert',str(cert),'--key',str(key)]
    if not args.apply:
        print('Dry run. Domain:',domain,'HTTPS:',https,'managed vhost:',output)
        if not https or cert.exists(): print(run(argv).decode())
        return
    if output.with_suffix(output.suffix+'.state.json').is_symlink(): raise ValueError('Unsafe state symlink')
    lock_path=output.with_suffix(output.suffix+'.lock')
    lock=os.open(lock_path,os.O_CREAT|os.O_RDWR|getattr(os,"O_NOFOLLOW",0),0o600)
    fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
    original=output.read_bytes() if output.exists() else None
    state_path=output.with_suffix(output.suffix+'.state.json')
    previous={}
    if state_path.exists() and not state_path.is_symlink():
        try: previous=json.loads(state_path.read_text())
        except (ValueError,OSError): pass
    signature={'domain':domain,'https':https,'ip_port':ip_port,'subscription':subscription}
    if previous.get('signature')==signature and previous.get('failed') and time.time()-previous.get('time',0)<1800:
        raise ValueError('Previous gateway update failed; 30-minute retry backoff. Remove the managed state JSON to retry after repair.')
    if original is not None and not original.startswith(b'# harbor-managed-v1:'):
        raise ValueError('Existing vhost is not owned by Harbor; refusing overwrite')
    def write(data):
        fd,name=tempfile.mkstemp(prefix=output.name+'.',suffix='.tmp',dir=output.parent)
        temporary=Path(name)
        try:
            with os.fdopen(fd,'wb') as file:
                file.write(data);file.flush();os.fsync(file.fileno())
            os.replace(temporary,output)
        finally:
            if temporary.exists(): temporary.unlink()
    def reload_nginx():
        run([nginx,'-t']);run([nginx,'-s','reload'])
    try:
        renewal_check=previous.get('renewal_check',0)
        if https and args.issue and (not cert.exists() or time.time()-previous.get('renewal_check',0)>43200):
            certbot=trusted_binary(args.certbot)
            webroot=Path('/var/www/harbor-acme');webroot.mkdir(mode=0o755,parents=True,exist_ok=True)
            if not cert.exists():
                # The temporary challenge vhost never serves plaintext credentials.
                challenge=('# harbor-managed-v1: ACME bootstrap\nserver { listen 80; listen [::]:80; server_name '+domain+'; location ^~ /.well-known/acme-challenge/ { root /var/www/harbor-acme; } location / { return 503; } }\n').encode()
                write(challenge);reload_nginx()
            renewal_check=time.time()
            run([certbot,'certonly','--non-interactive','--agree-tos','--register-unsafely-without-email','--webroot','-w',str(webroot),'-d',domain,'--keep-until-expiring'])
        candidate=run(argv)
        if args.public_management:
            import re
            candidate=re.sub(rb'(?m)^\s*allow [^;\n]+;\s*$',b'',candidate)
            candidate=re.sub(rb'(?m)^\s*deny all;\s*$',b'',candidate)
        cert_mtime=cert.stat().st_mtime_ns if https else 0
        key_mtime=key.stat().st_mtime_ns if https else 0
        if candidate!=original: write(candidate)
        # Even an unchanged path can contain a renewed certificate and need reload.
        if candidate!=original or previous.get('cert_mtime')!=cert_mtime or previous.get('key_mtime')!=key_mtime:
            reload_nginx()
        state_path.write_text(json.dumps({'signature':signature,'failed':False,'time':time.time(),'renewal_check':renewal_check,'cert_mtime':cert_mtime,'key_mtime':key_mtime}))
        state_path.chmod(0o600)
        call('site/gateway-status',{'role':'subscription' if subscription else 'management','domain':domain if subscription else site['domain'],'https':https,'state':'active'})
        print('Managed gateway applied and nginx reloaded:',domain,'HTTPS:',https)
    except BaseException:
        if not state_path.is_symlink():
            state_path.write_text(json.dumps({'signature':signature,'failed':True,'time':time.time()}));state_path.chmod(0o600)
        if original is None:
            if output.exists(): output.unlink()
        else: write(original)
        try: reload_nginx()
        except Exception: pass
        try:
            call('site/gateway-status',{'role':'subscription' if subscription else 'management','domain':domain if subscription else site['domain'],'https':https,'state':'error'})
        except Exception: pass
        raise
    finally:
        os.close(lock)

if __name__=='__main__':
    main()
    main(subscription=True)

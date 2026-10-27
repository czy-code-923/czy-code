"""Root-only, bounded ACME and reverse-proxy reconciler. No shell/config text from API."""
import argparse,fcntl,importlib.util,ipaddress,json,os,re,subprocess,tempfile,urllib.parse,urllib.request
from pathlib import Path
spec=importlib.util.spec_from_file_location('domain_sync',Path(__file__).with_name('domain-sync.py'));sync=importlib.util.module_from_spec(spec);spec.loader.exec_module(sync)
HEADER='# harbor-managed-routing-v1\n'
def upstream(value):
 u=urllib.parse.urlsplit(value)
 if u.scheme!='http' or u.hostname not in ('127.0.0.1','::1') or u.username or u.password or u.path or u.query or u.fragment:raise ValueError('Invalid upstream')
 if not u.port or not 1024<=u.port<=65535 or u.port in (8080,8091,5173,15432,5432,2375):raise ValueError('Reserved port')
 return value

def render(domain,target=None,certificate=False):
 domain=sync.domain_name(domain)
 if '.' not in domain:raise ValueError('Fully qualified domain required')
 challenge='location ^~ /.well-known/acme-challenge/ { root /var/www/harbor-acme; }'
 text=HEADER+'server { listen 80; listen [::]:80; server_name '+domain+'; '+challenge+' location / { '+('return 301 https://$host$request_uri;' if certificate and target else 'return 503;')+' } }\n'
 if certificate and target:
  target=upstream(target);base='/etc/harbor/gateway-certificates/'+domain
  text+='server { listen 443 ssl; listen [::]:443 ssl; server_name '+domain+'; ssl_certificate '+base+'/fullchain.pem; ssl_certificate_key '+base+'/privkey.pem; ssl_protocols TLSv1.2 TLSv1.3; client_max_body_size 16m; location / { proxy_pass '+target+'; proxy_http_version 1.1; proxy_set_header Host $host; proxy_set_header X-Real-IP $remote_addr; proxy_set_header X-Forwarded-For $remote_addr; proxy_set_header X-Forwarded-Proto https; proxy_set_header Upgrade $http_upgrade; proxy_set_header Connection "upgrade"; proxy_connect_timeout 10s; proxy_read_timeout 300s; } }\n'
 return text.encode()

def ensure_domain_available(directory,domain,own):
 for path in directory.glob('*.conf'):
  if path==own:continue
  content=re.sub(r'(?m)#.*$','',path.read_text(errors='replace'))
  for names in re.findall(r'\bserver_name\s+([^;]+);',content):
   if domain in names.split():raise ValueError('Domain already owned by another vhost')

def atomic_vhost(path,content,nginx):
 sync.verify_output(path)
 previous=path.read_bytes() if path.exists() else None
 if previous==content:return False
 if previous is not None and not previous.startswith(HEADER.encode()):raise ValueError('Refusing unmanaged vhost overwrite')
 def write(data):
  fd,name=tempfile.mkstemp(prefix=path.name+'.',suffix='.tmp',dir=path.parent)
  with os.fdopen(fd,'wb') as f:f.write(data);f.flush();os.fsync(f.fileno())
  os.chmod(name,0o644);os.replace(name,path)
 try:
  write(content);sync.run([nginx,'-t']);sync.run([nginx,'-s','reload']);return True
 except BaseException:
  if previous is not None:write(previous)
  else:path.unlink(missing_ok=True)
  try:sync.run([nginx,'-t']);sync.run([nginx,'-s','reload'])
  except Exception:pass
  raise

def main():
 parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--env',required=True);parser.add_argument('--nginx',required=True);parser.add_argument('--vhost-dir',required=True);parser.add_argument('--certbot',default='/usr/bin/certbot');args=parser.parse_args()
 if os.geteuid()!=0:raise ValueError('Root required')
 envfile=Path(args.env);st=envfile.stat()
 if st.st_uid!=0 or st.st_mode&0o077:raise ValueError('Private root-owned environment required')
 values=dict(line.split('=',1) for line in envfile.read_text().splitlines() if '=' in line and not line.startswith('#'))
 nginx=sync.trusted_binary(args.nginx);certbot=sync.trusted_binary(args.certbot);out=Path(args.vhost_dir);sync.verify_output(out/'harbor-check.conf')
 lock=os.open('/run/harbor-managed-gateway.lock',os.O_CREAT|os.O_RDWR|getattr(os,'O_NOFOLLOW',0),0o600);fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
 class NoRedirect(urllib.request.HTTPRedirectHandler):
  def redirect_request(self,*a):raise ValueError('Redirect forbidden')
 opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect())
 def call(path,body=None):
  req=urllib.request.Request('http://127.0.0.1:8080/api/v1/'+path,data=None if body is None else json.dumps(body).encode(),headers={'Authorization':'Bearer '+values['ADMIN_TOKEN'],'Content-Type':'application/json'})
  with opener.open(req,timeout=15) as response:return json.loads(response.read(1024*1024))
 site=call('site');work=call('gateway/work',{});main_domain=sync.domain_name(site['domain'])
 for job in work['certificates']:
  try:
   domain=sync.domain_name(job['domain']);path=out/('harbor-route-'+domain+'.conf')
   webroot=Path('/var/www/harbor-acme');webroot.mkdir(parents=True,exist_ok=True,mode=0o755)
   if job['provider']=='http-01' and domain!=main_domain and not path.exists():
    ensure_domain_available(out,domain,path);atomic_vhost(path,render(domain),nginx)
   argv=[certbot,'certonly','--non-interactive','--agree-tos','--email',job['email'],'--cert-name',domain,'-d',domain,'--keep-until-expiring']
   if job['provider']=='http-01':sync.run(argv+['--webroot','-w',str(webroot)])
   elif job['provider']=='cloudflare':
    token=job['dns_token']
    if not re.fullmatch(r'[A-Za-z0-9_-]{20,256}',token):raise ValueError('Invalid DNS token')
    with tempfile.TemporaryDirectory(prefix='harbor-dns-') as directory:
     credential=Path(directory)/'cloudflare.ini';credential.write_text('dns_cloudflare_api_token = '+token+'\n');credential.chmod(0o600)
     sync.run(argv+['--dns-cloudflare','--dns-cloudflare-credentials',str(credential),'--dns-cloudflare-propagation-seconds','30'])
   else:raise ValueError('Provider unsupported')
   base=Path('/etc/letsencrypt/live')/domain
   call('gateway/report',dict(id=job['id'],kind='certificate',state='ready',certificate=(base/'fullchain.pem').read_text(),private_key=(base/'privkey.pem').read_text()))
   sync.run([nginx,'-t']);sync.run([nginx,'-s','reload']);print('Certificate reconciled:',domain)
  except Exception:
   call('gateway/report',dict(id=job['id'],kind='certificate',state='failed'));print('Certificate request failed; inspect certbot private logs:',job['domain'])
 for proxy in work['reverse_proxies']:
  try:
   domain=sync.domain_name(proxy['domain'])
   if domain==main_domain:raise ValueError('Management vhost reserved')
   enabled=proxy['enabled']
   if enabled and (proxy['certificate_domain']!=domain or proxy['certificate_state']!='ready'):continue
   changed=False
   if enabled:
    directory=Path('/etc/harbor/gateway-certificates')/domain;directory.mkdir(parents=True,exist_ok=True,mode=0o700);directory.chmod(0o700)
    for field,name in [('certificate','fullchain.pem'),('private_key','privkey.pem')]:
     content=proxy.get(field,'')
     if not content:raise ValueError('Certificate payload missing')
     path=directory/name;sync.verify_output(path)
     if path.exists() and path.is_symlink():raise ValueError('Unsafe certificate link')
     if not path.exists() or path.read_text()!=content:
      fd,temporary=tempfile.mkstemp(dir=directory)
      with os.fdopen(fd,'w') as file:file.write(content);file.flush();os.fsync(file.fileno())
      os.chmod(temporary,0o600);os.replace(temporary,path);changed=True
   path=out/('harbor-route-'+domain+'.conf');ensure_domain_available(out,domain,path);reloaded=atomic_vhost(path,render(domain,proxy['upstream'] if enabled else None,enabled),nginx)
   if changed and not reloaded:sync.run([nginx,'-t']);sync.run([nginx,'-s','reload'])
   if proxy['reported_version']!=proxy['version'] or proxy['state'] not in ('active','disabled'):call('gateway/report',dict(id=proxy['id'],kind='reverse-proxy',state='active' if enabled else 'disabled',version=proxy['version']))
  except Exception:call('gateway/report',dict(id=proxy['id'],kind='reverse-proxy',state='failed',version=proxy['version']));print('Reverse proxy validation failed:',proxy['domain'])
 os.close(lock)
if __name__=='__main__':main()

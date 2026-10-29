"""Root-only, bounded ACME and reverse-proxy reconciler. No shell/config text from API."""
import argparse,fcntl,importlib.util,ipaddress,json,os,re,subprocess,tempfile,urllib.parse,urllib.request
from pathlib import Path
spec=importlib.util.spec_from_file_location('domain_sync',Path(__file__).with_name('domain-sync.py'));sync=importlib.util.module_from_spec(spec);spec.loader.exec_module(sync)
spec=importlib.util.spec_from_file_location('gateway_config',Path(__file__).with_name('gateway-config.py'));config=importlib.util.module_from_spec(spec);spec.loader.exec_module(config)
HEADER=config.HEADER
upstream=config.upstream
render=config.render

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

def remove_vhost(path,nginx):
 sync.verify_output(path)
 if not path.exists():return
 previous=path.read_bytes()
 if not previous.startswith(HEADER.encode()):raise ValueError('Refusing unmanaged vhost removal')
 path.unlink()
 try:sync.run([nginx,'-t']);sync.run([nginx,'-s','reload'])
 except BaseException:
  path.write_bytes(previous);path.chmod(0o644)
  try:sync.run([nginx,'-t']);sync.run([nginx,'-s','reload'])
  except Exception:pass
  raise

def proxy_usage(directory,identity,state,baseline,nginx):
 if not config.ID.fullmatch(identity):raise ValueError('Invalid proxy id')
 fresh=state is None;state=dict(state or {'up':baseline[0],'down':baseline[1],'files':{}})
 active=directory/('reverse-'+identity+'.jsonl');old=active.with_suffix('.old')
 for path in (old,active):
  sync.verify_output(path)
  if not path.exists():continue
  inode=str(path.stat().st_ino);previous=state['files'].get(inode)
  if fresh and any(baseline) and previous is None:previous={'up':0,'down':0,'offset':path.stat().st_size,'inode':path.stat().st_ino}
  value=config.collect(path,previous)
  state['up']+=value['up']-(previous or {}).get('up',0);state['down']+=value['down']-(previous or {}).get('down',0)
  value['stable']=(previous or {}).get('stable',0)+1 if previous and value['offset']==previous['offset'] else 0
  state['files'][inode]=value
  if path==old and value['stable']>=2 and value['offset']==path.stat().st_size:
   path.unlink();state['files'].pop(inode,None)
 if active.exists() and active.stat().st_size>=4*1024*1024 and not old.exists():
  active.rename(old);active.touch(mode=0o600);sync.run([nginx,'-s','reopen'])
 known={str(p.stat().st_ino) for p in (old,active) if p.exists()}
 state['files']={k:v for k,v in state['files'].items() if k in known}
 return state

def main():
 parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--env',required=True);parser.add_argument('--nginx',required=True);parser.add_argument('--vhost-dir',required=True);parser.add_argument('--certbot',default='/usr/bin/certbot');args=parser.parse_args()
 if os.geteuid()!=0:raise ValueError('Root required')
 envfile=Path(args.env);st=envfile.stat()
 if st.st_uid!=0 or st.st_mode&0o077:raise ValueError('Private root-owned environment required')
 values=dict(line.split('=',1) for line in envfile.read_text().splitlines() if '=' in line and not line.startswith('#'))
 nginx=sync.trusted_binary(args.nginx);out=Path(args.vhost_dir);sync.verify_output(out/'harbor-check.conf')
 lock=os.open('/run/harbor-managed-gateway.lock',os.O_CREAT|os.O_RDWR|getattr(os,'O_NOFOLLOW',0),0o600);fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
 class NoRedirect(urllib.request.HTTPRedirectHandler):
  def redirect_request(self,*a):raise ValueError('Redirect forbidden')
 opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect())
 def call(path,body=None):
  req=urllib.request.Request('http://127.0.0.1:8080/api/v1/'+path,data=None if body is None else json.dumps(body).encode(),headers={'Authorization':'Bearer '+values['ADMIN_TOKEN'],'Content-Type':'application/json'})
  with opener.open(req,timeout=15) as response:return json.loads(response.read(1024*1024))
 try:
  output=subprocess.run([nginx,'-v'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5,check=True)
  version=re.search(rb'nginx/([0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3})',output.stdout+output.stderr)
  if not version:raise ValueError('Unrecognized nginx version')
  call('gateway/nginx',{'version':version.group(1).decode('ascii'),'source':'baota' if nginx.startswith('/www/server/nginx/') else 'system'})
 except Exception:print('Nginx version reporting unavailable')
 site=call('site');work=call('gateway/work',{});main_domain=sync.domain_name(site['domain']) if site.get('domain') else ''
 reserved={v for v in (main_domain,site.get('share_domain','')) if v}
 certbot=sync.trusted_binary(args.certbot) if work['certificates'] else None
 logs=Path('/var/log/harbor');sync.verify_output(logs);logs.mkdir(exist_ok=True,mode=0o700);sync.verify_output(logs/'ownership-check')
 state_path=Path('/etc/harbor/gateway-usage.json');sync.verify_output(state_path)
 usage=json.loads(state_path.read_text()) if state_path.exists() else {}
 if not isinstance(usage,dict) or len(usage)>100:raise ValueError('Invalid usage state')
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
   call('gateway/report',dict(id=job['id'],version=job['version'],kind='certificate',state='ready',certificate=(base/'fullchain.pem').read_text(),private_key=(base/'privkey.pem').read_text()))
   sync.run([nginx,'-t']);sync.run([nginx,'-s','reload']);print('Certificate reconciled:',domain)
  except Exception:
   call('gateway/report',dict(id=job['id'],version=job['version'],kind='certificate',state='failed'));print('Certificate request failed; inspect certbot private logs:',job['domain'])
 for proxy in work['reverse_proxies']:
  try:
   domain=sync.domain_name(proxy['domain'])
   if domain in reserved:raise ValueError('Management/subscription vhost reserved')
   if not config.ID.fullmatch(proxy['id']):raise ValueError('Invalid proxy id')
   old_domain=sync.domain_name(proxy['applied_domain']) if proxy.get('applied_domain') else ''
   if old_domain in reserved:raise ValueError('Reserved applied domain')
   if proxy.get('deleted_at'):
    for value in ({domain,old_domain}|set(proxy.get('cleanup_domains') or []))-{''}:
     if value in reserved:raise ValueError('Reserved domain cleanup')
     remove_vhost(out/('harbor-route-'+sync.domain_name(value)+'.conf'),nginx)
    for suffix in ('.jsonl','.old'):
     path=logs/('reverse-'+proxy['id']+suffix);sync.verify_output(path);path.unlink(missing_ok=True)
    usage.pop(proxy['id'],None);config.save_state(state_path,usage)
    call('gateway/report',dict(id=proxy['id'],kind='reverse-proxy',state='disabled',version=proxy['version']));continue
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
   pinned=config.pinned_upstream(proxy['upstream']) if enabled else None
   content=render(domain,proxy['upstream'] if enabled else None,enabled,proxy.get('options'),proxy['id'],pinned)
   path=out/('harbor-route-'+domain+'.conf');ensure_domain_available(out,domain,path);reloaded=atomic_vhost(path,content,nginx)
   if changed and not reloaded:sync.run([nginx,'-t']);sync.run([nginx,'-s','reload'])
   for value in ({old_domain}|set(proxy.get('cleanup_domains') or []))-{domain,''}:
    if value in reserved:raise ValueError('Reserved domain cleanup')
    remove_vhost(out/('harbor-route-'+sync.domain_name(value)+'.conf'),nginx)
   if proxy['reported_version']!=proxy['version'] or proxy['state'] not in ('active','disabled') or content.decode()!=proxy.get('rendered_config'):call('gateway/report',dict(id=proxy['id'],kind='reverse-proxy',state='active' if enabled else 'disabled',version=proxy['version'],rendered_config=content.decode()))
   usage[proxy['id']]=proxy_usage(logs,proxy['id'],usage.get(proxy['id']),(proxy.get('upload_bytes',0),proxy.get('download_bytes',0)),nginx)
   config.save_state(state_path,usage)
   call('gateway/report',dict(id=proxy['id'],kind='reverse-proxy-usage',upload_bytes=usage[proxy['id']]['up'],download_bytes=usage[proxy['id']]['down']))
  except Exception:call('gateway/report',dict(id=proxy['id'],kind='reverse-proxy',state='failed',version=proxy['version']));print('Reverse proxy validation failed:',proxy['domain'])
 used_domains=reserved|{p['domain'] for p in work['reverse_proxies']}|{p.get('applied_domain','') for p in work['reverse_proxies']}
 for cert in work.get('certificate_deletions',[]):
  try:
   domains=set(cert.get('cleanup_domains') or [])
   if cert.get('deleted_at'):domains.add(cert['domain'])
   for value in domains:
    domain=sync.domain_name(value)
    if domain in reserved:continue
    if domain in used_domains:raise ValueError('Certificate domain still referenced')
    remove_vhost(out/('harbor-route-'+domain+'.conf'),nginx)
    directory=Path('/etc/harbor/gateway-certificates')/domain
    if directory.exists():
     for name in ('fullchain.pem','privkey.pem'):
      path=directory/name;sync.verify_output(path);path.unlink(missing_ok=True)
     directory.rmdir()
   call('gateway/report',dict(id=cert['id'],kind='certificate-delete' if cert.get('deleted_at') else 'certificate-cleanup',version=cert['version']))
  except Exception:print('Certificate cleanup pending:',cert['domain'])
 os.close(lock)
if __name__=='__main__':main()

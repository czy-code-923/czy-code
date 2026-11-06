"""Disposable GitHub Linux VM only. Exercises real Nginx with a local test CA.

Fake certbot replaces external ACME/DNS, so no request goes to Let's Encrypt or
Cloudflare. The application, root helpers, HTTP challenge, TLS, and database are real.
"""
import json,os,shutil,subprocess,sys,time,urllib.request
from pathlib import Path

DOMAIN='starhub-gateway-ci.example.invalid'

def require_ci():
    if sys.platform!='linux' or os.geteuid()!=0 or os.environ.get('GITHUB_ACTIONS')!='true' or os.environ.get('RUNNER_ENVIRONMENT')!='github-hosted':raise ValueError('Disposable GitHub-hosted root VM required')

def verify():
    require_ci();root=Path('/opt/starhub-control');private=root/'control.env';original_env=private.read_bytes()
    env=dict(line.split('=',1) for line in private.read_text().splitlines() if '=' in line)
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
    def api(path,body=None,method=None):
        req=urllib.request.Request('http://127.0.0.1:8080/api/v1/'+path,data=None if body is None else json.dumps(body).encode(),headers={'Authorization':'Bearer '+env['ADMIN_TOKEN'],'Content-Type':'application/json'},method=method)
        with opener.open(req,timeout=20) as r:return json.load(r)
    subprocess.run(['systemctl','stop','starhub-gateway.timer','starhub-gateway.service'],check=True)
    config_file=root/'gateway.json';original_config=config_file.read_bytes();config=json.loads(original_config)
    out=Path(config['vhost_dir'])/'starhub-management.conf';state=out.with_suffix('.conf.state.json');lock=out.with_suffix('.conf.lock')
    if out.exists():raise ValueError('Unexpected existing management vhost in disposable test')
    unrelated=out.parent/'starhub-ci-unrelated.conf';unrelated_body=b'# unrelated test app\nserver { listen 127.0.0.1:18082; server_name unrelated.example.invalid; return 200 "preserved"; }\n';unrelated.write_bytes(unrelated_body)
    fake=root/'gateway-ci-certbot'
    ca=root/'gateway-ci-ca.pem';ca_key=root/'gateway-ci-ca.key';trust=Path('/usr/local/share/ca-certificates/starhub-gateway-ci.crt')
    if trust.exists():raise ValueError('Unexpected existing test trust anchor')
    subprocess.run(['/usr/bin/openssl','req','-x509','-newkey','rsa:2048','-nodes','-days','2','-subj','/CN=Starhub disposable CI CA','-addext','basicConstraints=critical,CA:TRUE','-addext','keyUsage=critical,keyCertSign,cRLSign','-keyout',str(ca_key),'-out',str(ca)],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    ca_key.chmod(0o600);shutil.copyfile(ca,trust);trust.chmod(0o644)
    subprocess.run(['update-ca-certificates'],check=True,stdout=subprocess.DEVNULL)
    # Exercise the real chain validation, never weaken production verification.
    subprocess.run(['systemctl','restart','starhub-control.service'],check=True)
    for attempt in range(30):
        try:api('session');break
        except OSError:time.sleep(1)
    else:raise ValueError('Control restart timed out')
    fake.write_text('''#!/usr/bin/python3
import http.client,os,pathlib,subprocess,sys,time
def failure(kind,value,tb):
 pathlib.Path('/opt/starhub-control/gateway-ci-failure.txt').write_text(kind.__name__+': '+str(value));sys.__excepthook__(kind,value,tb)
sys.excepthook=failure
a=sys.argv[1:]
if a[:1]==['plugins']:
 print('dns-cloudflare');sys.exit(0)
assert a[0]=='certonly'
domain=a[a.index('-d')+1];assert domain=='starhub-gateway-ci.example.invalid'
if '--dns-cloudflare' in a:
 assert '--webroot' not in a
 private=pathlib.Path(a[a.index('--dns-cloudflare-credentials')+1]);assert private.stat().st_mode&0o077==0
else:
 assert '--webroot' in a
 web=pathlib.Path(a[a.index('-w')+1])/'.well-known/acme-challenge/starhub-ci';web.parent.mkdir(parents=True,exist_ok=True);web.parent.chmod(0o755);web.parent.parent.chmod(0o755);web.write_text('local-challenge');web.chmod(0o644)
 for attempt in range(30):
  c=http.client.HTTPConnection('127.0.0.1',80,timeout=10);c.request('GET','/.well-known/acme-challenge/starhub-ci',headers={'Host':domain});r=c.getresponse();status=r.status;content=r.read();c.close()
  if status==200 and content==b'local-challenge':break
  time.sleep(0.2)
 else:raise ValueError('HTTP challenge did not become reachable after Nginx reload; status '+str(status))
 web.unlink()
directory=pathlib.Path('/etc/letsencrypt/live')/domain;directory.mkdir(parents=True,exist_ok=True)
root=pathlib.Path('/opt/starhub-control');csr=root/'gateway-ci-leaf.csr';ext=root/'gateway-ci-leaf.ext'
ext.write_text('subjectAltName=DNS:'+domain+'\\nbasicConstraints=critical,CA:FALSE\\nkeyUsage=critical,digitalSignature,keyEncipherment\\nextendedKeyUsage=serverAuth\\n')
subprocess.run(['/usr/bin/openssl','req','-new','-newkey','rsa:2048','-nodes','-subj','/CN='+domain,'-keyout',str(directory/'privkey.pem'),'-out',str(csr)],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
subprocess.run(['/usr/bin/openssl','x509','-req','-days','2','-in',str(csr),'-CA',str(root/'gateway-ci-ca.pem'),'-CAkey',str(root/'gateway-ci-ca.key'),'-CAcreateserial','-extfile',str(ext),'-out',str(directory/'fullchain.pem')],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
with (directory/'fullchain.pem').open('ab') as f:f.write((root/'gateway-ci-ca.pem').read_bytes())
(directory/'privkey.pem').chmod(0o600)
''');fake.chmod(0o755)
    original_site=api('site');fields=('domain','https','title','ip_origin','share_domain','share_https','api_origins','agent_origins')
    site_body={k:original_site[k] for k in fields}
    try:
        for provider in ('cloudflare','http-01'):
            body={**site_body,'domain':DOMAIN,'https':True};api('site',body,'PUT')
            cert=api('certificates',{'domain':DOMAIN,'email':'ci@example.invalid','provider':provider,**({'dns_token':'ci-test-token-not-a-real-key'} if provider=='cloudflare' else {})})
            config['certbot']=str(fake);config_file.write_text(json.dumps(config));config_file.chmod(0o600)
            subprocess.run([sys.executable,str(root/'scripts/gateway-worker.py'),'--config',str(config_file)],check=True,timeout=120)
            current=next(c for c in api('certificates') if c['id']==cert['id']);assert current['state']=='ready'
            assert api('site')['gateway_state']=='active'
            pem=Path('/etc/letsencrypt/live')/DOMAIN/'fullchain.pem'
            result=subprocess.run(['curl','--noproxy','*','--silent','--show-error','--fail','--cacert',str(pem),'--resolve',DOMAIN+':443:127.0.0.1','https://'+DOMAIN+'/api/v1/branding'],capture_output=True,check=True,text=True)
            assert json.loads(result.stdout)['version']==api('session')['version']
            large=root/'gateway-ci-body.json';large.write_text(json.dumps({'fixture':'x'*(80<<10)}))
            large_result=subprocess.run(['curl','--noproxy','*','--silent','--show-error','--output','/dev/null','--write-out','%{http_code}','--cacert',str(pem),'--resolve',DOMAIN+':443:127.0.0.1','--header','Content-Type: application/json','--data-binary','@'+str(large),'https://'+DOMAIN+'/api/v1/client-policies/import'],capture_output=True,check=True,text=True)
            assert large_result.stdout=='401';large.unlink()
            assert private.read_bytes()==original_env and unrelated.read_bytes()==unrelated_body
            if provider=='cloudflare':
                # Exercise the real SSH maintenance CLI under systemd, rather
                # than mocking listener changes, container state, or startup.
                public=original_env.replace(b'WEB_ADDR=127.0.0.1:18080',b'WEB_ADDR=0.0.0.0:18080')
                assert public!=original_env;private.write_bytes(public)
                subprocess.run(['systemctl','restart','starhub-control.service'],check=True)
                for attempt in range(30):
                    try:api('session');break
                    except OSError:time.sleep(1)
                closed=subprocess.run([sys.executable,str(root/'panel-access.py'),'disable-public','--env',str(private)],capture_output=True,text=True,check=True,timeout=90)
                change=json.loads(closed.stdout);assert change['changed'] and change['web_listener']=='127.0.0.1:18080'
                assert private.read_bytes()==original_env
                sockets=subprocess.check_output(['ss','-H','-lnt','sport = :18080'],text=True)
                assert '127.0.0.1:18080' in sockets and all(v not in sockets for v in ('0.0.0.0:18080','[::]:18080','*:18080'))
                started=subprocess.run([sys.executable,str(root/'panel-access.py'),'enable-autostart','--env',str(private)],capture_output=True,text=True,check=True,timeout=90)
                services=json.loads(started.stdout);assert services['complete'] and 'nginx.service' in services['enabled']
                subprocess.run(['systemctl','is-enabled','--quiet','docker.service','nginx.service','starhub-control.service','starhub-gateway.timer','starhub-panel-updater.timer'],check=True)
                subprocess.run(['systemctl','stop','starhub-gateway.timer','starhub-gateway.service'],check=True)
                saved=Path(change['backup']);assert saved.parent==root and saved.name.startswith('control.env.before-ip-close.');saved.unlink()
                assert private.read_bytes()==original_env
                print('Real public listener closure, HTTPS preservation, identity preservation and autostart: PASS',flush=True)
            # No user data: remove only this fixture's marked vhost and test record.
            api('site',site_body,'PUT');api('certificates/'+cert['id'],method='DELETE')
            subprocess.run([sys.executable,str(root/'scripts/gateway-worker.py'),'--config',str(config_file)],check=True,timeout=120)
            if out.exists():
                assert out.read_bytes().startswith(b'# harbor-managed-v1:');out.unlink()
            state.unlink(missing_ok=True);lock.unlink(missing_ok=True)
            subprocess.run([config['nginx'],'-t'],check=True);subprocess.run([config['nginx'],'-s','reload'],check=True)
            print('Actual gateway '+provider+' issuance, challenge/TLS, credential preservation and existing-site preservation: PASS',flush=True)
    except Exception as error:
        if isinstance(error,subprocess.CalledProcessError) and error.stderr:print('Maintenance fixture stderr: '+str(error.stderr)[-4000:],flush=True)
        diagnostic=root/'gateway-ci-failure.txt'
        if diagnostic.exists():print('Disposable Certbot fixture: '+diagnostic.read_text(),flush=True)
        subprocess.run(['journalctl','-u','starhub-control.service','-n','20','--no-pager'],check=False)
        raise
    finally:
        api('site',site_body,'PUT');config_file.write_bytes(original_config)
        unrelated.unlink(missing_ok=True);fake.unlink(missing_ok=True)
        trust.unlink(missing_ok=True);subprocess.run(['update-ca-certificates'],check=True,stdout=subprocess.DEVNULL)
        if out.exists() and out.read_bytes().startswith(b'# harbor-managed-v1:'):out.unlink()
        state.unlink(missing_ok=True);lock.unlink(missing_ok=True)
        subprocess.run([config['nginx'],'-t'],check=True);subprocess.run([config['nginx'],'-s','reload'],check=True)
        subprocess.run(['systemctl','enable','--now','starhub-gateway.timer'],check=True)

if __name__=='__main__':verify()

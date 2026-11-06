"""Gateway bootstrap tests with mocked package managers and control APIs."""
import hashlib,importlib.util,io,json,os,subprocess,sys,tempfile,types,unittest,zipfile
from pathlib import Path
from unittest.mock import patch
if os.name=='nt':sys.modules.setdefault('fcntl',types.SimpleNamespace(LOCK_EX=1,LOCK_NB=2,flock=lambda *a:None))

def load(name,file):
    s=importlib.util.spec_from_file_location(name,Path(__file__).with_name(file));m=importlib.util.module_from_spec(s);s.loader.exec_module(m);return m
i=load('install_gateway','install-gateway.py');w=load('gateway_worker','gateway-worker.py')

class BootstrapTests(unittest.TestCase):
    def test_detects_baota_before_system_nginx(self):
        with patch.object(i.Path,'is_file',return_value=True):self.assertEqual(i.nginx_path().as_posix(),'/www/server/nginx/sbin/nginx')

    def test_existing_baota_is_not_replaced_when_certbot_is_missing(self):
        calls=[]
        with patch.object(i,'nginx_path',return_value=Path('/www/server/nginx/sbin/nginx')),patch.object(i.Path,'is_file',side_effect=[False,True]),patch.object(i,'plugin_available',side_effect=[False,True]),patch.object(i.shutil,'which',return_value='/usr/bin/apt-get'),patch.object(i,'run',side_effect=lambda *a,**k:calls.append(a)):
            nginx,certbot=i.ensure_dependencies(True)
        self.assertEqual(nginx.as_posix(),'/www/server/nginx/sbin/nginx');self.assertIn('certbot',calls[-1]);self.assertNotIn('nginx',calls[-1]);self.assertIn('python3-certbot-dns-cloudflare',calls[-1])

    def test_dependency_install_is_explicit_and_busy_ports_block_it(self):
        with patch.object(i,'nginx_path',return_value=None),patch.object(i.Path,'is_file',return_value=False),patch.object(i,'run') as run:
            with self.assertRaisesRegex(ValueError,'--install-deps'):i.ensure_dependencies(False)
            with patch.object(i,'check_web_ports',side_effect=ValueError('port occupied')):
                with self.assertRaisesRegex(ValueError,'port occupied'):i.ensure_dependencies(True)
            run.assert_not_called()

    def test_existing_public_access_policy_is_preserved(self):
        for address,expected in [('127.0.0.1:18080',False),('[::1]:18080',False),('0.0.0.0:18080',True),('[::]:18080',True)]:self.assertEqual(i.public_listener({'WEB_ADDR':address}),expected)

    def test_source_download_verifies_checksum_and_exact_members(self):
        data=io.BytesIO()
        with zipfile.ZipFile(data,'w') as z:
            for name in i.FILES:z.writestr('harbor-'+i.VERSION+'/scripts/'+name,'# verified '+name)
        archive=data.getvalue();url='https://github.com/'+i.REPO+'/releases/download/v'+i.VERSION+'-distribution/harbor-'+i.VERSION+'-source.zip'
        asset={'name':'harbor-'+i.VERSION+'-source.zip','size':len(archive),'digest':'sha256:'+hashlib.sha256(archive).hexdigest(),'browser_download_url':url}
        metadata={'tag_name':'v'+i.VERSION+'-distribution','draft':False,'prerelease':False,'assets':[asset]}
        class Opener:
            def open(self,req,timeout=30):return io.BytesIO(json.dumps(metadata).encode() if 'api.github.com' in req.full_url else archive)
        with tempfile.TemporaryDirectory() as d,patch.object(i.urllib.request,'build_opener',return_value=Opener()):
            i.source_release(Path(d));self.assertEqual(set(p.name for p in Path(d).iterdir()),set(i.FILES))
            asset['digest']='sha256:'+'0'*64
            with self.assertRaisesRegex(ValueError,'checksum'):i.source_release(Path(d))

    def test_redirects_stay_on_official_https_hosts(self):
        for url in ('http://github.com/a','https://github.com:444/a','https://example.com/a','https://x:y@github.com/a'):
            with self.assertRaises(ValueError):i.OfficialRedirect().redirect_request(None,None,302,'',{},url)

    def test_nginx_runtime_capabilities_fall_back_for_old_control(self):
        calls=[]
        def api(token,path,data):
            calls.append(data)
            if len(calls)==1:raise w.urllib.error.HTTPError('',400,'old control',{},None)
        result=types.SimpleNamespace(stderr=b'nginx version: nginx/1.26.3',stdout='dns-cloudflare',returncode=0)
        with patch.object(w.subprocess,'run',return_value=result):w.report_runtime(types.SimpleNamespace(local_api=api),'PRIVATE_TEST_TOKEN','/usr/sbin/nginx','/usr/bin/certbot')
        self.assertTrue(calls[0]['cloudflare_available']);self.assertEqual(set(calls[1]),{'version','source'});self.assertNotIn('PRIVATE_TEST_TOKEN',json.dumps(calls))

    def test_empty_domain_never_competes_with_native_ip_listener(self):
        self.assertEqual(w.roles({'domain':'','ip_origin':'http://127.0.0.1:18080'}),[('subscription','',True)])
        self.assertEqual(w.roles({'domain':'','share_domain':'sub.example.com','share_https':True}),[('subscription','sub.example.com',True)])

    def test_cloudflare_domain_does_not_fall_back_to_http_issuance(self):
        site={'domain':'vps.example.com','https':True,'share_domain':''}
        cert={'domain':'vps.example.com','provider':'cloudflare','state':'ready'}
        calls=[]
        helper=types.SimpleNamespace(load_install=lambda p:{'token':'unit-test-only'},local_api=lambda token,path,body=None:site if path=='site' else [cert])
        sync=types.SimpleNamespace(trusted_binary=lambda x:x)
        config={'env':'/opt/starhub-control/control.env','nginx':'/usr/sbin/nginx','certbot':'/usr/bin/certbot','gateway':'/opt/starhub-control/bin/harbor-gateway','enable_acme':True,'vhost_dir':'/etc/nginx/conf.d','public_management':True,'allow_http':True}
        def run(args,**kw):calls.append(args);return types.SimpleNamespace(returncode=0)
        with patch.object(w,'module',return_value=sync),patch.object(w,'report_runtime'),patch.object(w,'bootstrap_http',return_value=[]) as bootstrap,patch.object(w.subprocess,'run',side_effect=run):w.reconcile(config,helper)
        self.assertIn('managed-gateway.py',calls[0][1]);self.assertNotIn('--issue',calls[1]);self.assertIn('--role',calls[1]);self.assertEqual(calls[1][calls[1].index('--role')+1],'management')

    def test_worker_unit_is_bounded_and_never_contains_credentials(self):
        text=i.unit_text(Path('/opt/starhub-control/scripts'),Path('/opt/starhub-control/gateway.json'))
        self.assertTrue(text.startswith(i.MARKER));self.assertIn('NoNewPrivileges=true',text);self.assertIn('MemoryMax=256M',text);self.assertNotIn('ADMIN_TOKEN',text)

    @unittest.skipIf(os.name=='nt','Unix permission fixture; exercised on Linux CI')
    def test_failed_repair_restores_helpers_and_keeps_control_credentials(self):
        real_path=Path
        with tempfile.TemporaryDirectory() as d:
            base=real_path(d);root=base/'starhub-control';scripts=root/'scripts';scripts.mkdir(parents=True);source=base/'source';source.mkdir();env=root/'control.env';env.write_text('ADMIN_TOKEN=private-test-only\nWEB_ADDR=127.0.0.1:18080\n')
            original_env=env.read_bytes()
            for name in i.FILES:
                (source/name).write_text('# candidate helper');(scripts/name).write_text('# original helper')
            def path(value):
                value=str(value)
                return base/value.lstrip('/') if value.startswith(('/etc/','/var/')) else real_path(value)
            units=path('/etc/systemd/system');units.mkdir(parents=True)
            for suffix in ('service','timer'):(units/('starhub-gateway.'+suffix)).write_text(i.MARKER+'# original unit')
            (root/'gateway.json').write_text(json.dumps({'owner':'starhub-gateway-v1'}));(root/'gateway.json').chmod(0o600)
            vhosts=path('/etc/nginx/conf.d');vhosts.mkdir(parents=True)
            helper=types.SimpleNamespace(load_install=lambda p:{'root':root,'token':'private-test-only'},trusted=lambda p,*a:p,local_api=lambda *a:{},atomic_copy=lambda s,t:__import__('shutil').copyfile(s,t),write_json=lambda p,v:p.write_text(json.dumps(v)))
            sync=types.SimpleNamespace(trusted_binary=lambda p:p,verify_output=lambda p:None)
            def run(*args,**kw):
                if args[-1:] == ('-t',):raise ValueError('Injected native validation failure')
                return types.SimpleNamespace(stdout='include '+str(vhosts)+'/*.conf;' if '-T' in args else 'inactive',stderr='',returncode=0)
            with patch.object(i,'Path',side_effect=path),patch.object(i,'module',side_effect=lambda name,p:helper if 'validation' in name else sync),patch.object(i.sys,'platform','linux'),patch.object(i.os,'geteuid',return_value=0,create=True),patch.object(i.os,'chown',create=True),patch.object(i.shutil,'which',return_value='/usr/bin/systemctl'),patch.object(i,'ensure_dependencies',return_value=(real_path('/usr/sbin/nginx'),real_path('/usr/bin/certbot'))),patch.object(i.subprocess,'run',return_value=types.SimpleNamespace(returncode=3)),patch.object(i,'run',side_effect=run):
                with self.assertRaisesRegex(ValueError,'Injected native'):i.install_gateway(env,source,False,True)
            self.assertEqual(env.read_bytes(),original_env)
            for name in i.FILES:self.assertEqual((scripts/name).read_text(),'# original helper')
            self.assertEqual(json.loads((root/'gateway.json').read_text()),{'owner':'starhub-gateway-v1'})
            self.assertTrue((units/'starhub-gateway.service').read_text().endswith('# original unit'))

if __name__=='__main__':unittest.main()

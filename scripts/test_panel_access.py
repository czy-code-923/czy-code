"""Recovery tests: configuration discovery, no token disclosure, bounded targets."""
import importlib.util, json, os, subprocess, sys, tempfile, types, unittest
from pathlib import Path
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('panel_access',Path(__file__).with_name('panel-access.py'))
p=importlib.util.module_from_spec(spec);spec.loader.exec_module(p)

class RecoveryTests(unittest.TestCase):
    def test_internal_targets_cannot_exfiltrate_token(self):
        for value in ['example.org:8080','0.0.0.0:8080','192.0.2.1:8080','user@127.0.0.1:8080','127.0.0.1:8080/path']:
            with self.subTest(value=value):
                with self.assertRaises(ValueError):p.listener(value)
        self.assertEqual(p.listener('[::1]:8080'),'[::1]:8080')

    def test_address_query_does_not_print_credentials(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp)
            env={'ADMIN_TOKEN':'secret-token-not-for-output-'*2,'WEB_DIR':str(root),'DATABASE_URL':'postgres://private','HTTP_ADDR':'127.0.0.1:8080','WEB_ADDR':'127.0.0.1:5173','PUBLIC_ORIGIN':'https://panel.example'}
            with patch.object(p.urllib.request.OpenerDirector,'open',side_effect=OSError('offline')),patch.object(p,'UNIT',root/'absent'):
                result=p.query(root,env)
            self.assertEqual(result['panel_url'],'https://panel.example')
            self.assertFalse(result['recovery']['active'])
            self.assertNotIn('secret-token',json.dumps(result))
            self.assertNotIn('postgres://',json.dumps(result))

    def test_recovery_unit_ownership_is_enforced(self):
        with tempfile.TemporaryDirectory() as tmp:
            unit=Path(tmp)/'service'
            with patch.object(p,'UNIT',unit):
                unit.write_text('ExecStart=/other/program\n')
                with self.assertRaises(ValueError):p.verify_unit(Path('/opt/starhub-control'))
                unit.write_text(p.MARKER+'ExecStart='+str(Path('/opt/starhub-control')/'bin/harbor-control')+' panel-web --ttl 30m\n')
                p.verify_unit(Path('/opt/starhub-control'))

    def test_ipv6_url(self):
        self.assertEqual(p.ip_url('2001:db8::1',18081),'http://[2001:db8::1]:18081')

class MaintenanceTests(unittest.TestCase):
    def setUp(self):
        directory=tempfile.TemporaryDirectory();self.addCleanup(directory.cleanup)
        self.root=Path(directory.name)/'starhub-control';self.root.mkdir();(self.root/'bin').mkdir();(self.root/'bin/harbor-control').write_bytes(b'fixture')
        self.cfg=self.root/'control.env';self.original=b'ADMIN_TOKEN=private-token-sentinel-01234567890123456789\nMASTER_KEY=private-key-sentinel\nWEB_ADDR=[::]:18080\nDATABASE_URL=postgres://harbor:private-db-sentinel@127.0.0.1:15432/harbor\n'
        self.cfg.write_bytes(self.original);self.cfg.chmod(0o600)
        self.env=dict(line.split('=',1) for line in self.original.decode().splitlines())
        self.units=self.root/'units';self.units.mkdir();self.rescue=self.units/'starhub-panel-rescue.service'
        self.site={'domain':'panel.example.com','https':True};self.calls=[]
        self.info=[{'Config':{'Image':'postgres:17-alpine','Env':['POSTGRES_USER=harbor','POSTGRES_DB=harbor','POSTGRES_PASSWORD=private-db-sentinel']},'HostConfig':{'PortBindings':{'5432/tcp':[{'HostIp':'127.0.0.1','HostPort':'15432'}]},'RestartPolicy':{'Name':'no'}},'Mounts':[{'Type':'volume','Name':'starhub-control-pgdata','Destination':'/var/lib/postgresql/data'}],'State':{'Running':False}}]
        for patcher in (patch.object(p,'UNIT',self.rescue),patch.object(p,'UNIT_DIR',self.units),patch.object(p,'_trusted'),patch.object(p,'_local_json',side_effect=self.local),patch.object(p,'_probe_https'),patch.object(p,'_run',side_effect=self.run_command)):
            patcher.start();self.addCleanup(patcher.stop)

    def local(self,env,path):return self.site if path=='site' else {'version':'0.25.5'}
    def run_command(self,argv,**kwargs):
        self.calls.append(argv)
        return types.SimpleNamespace(returncode=0,stdout=json.dumps(self.info).encode() if argv[:3]==['docker','container','inspect'] else b'')

    def test_disable_public_preserves_identities_and_closes_recovery(self):
        self.rescue.write_text(p.MARKER+'ExecStart='+str(self.root/'bin/harbor-control')+' panel-web --ttl 30m\n')
        result=p.disable_public(self.root,self.env,self.cfg)
        self.assertEqual(self.cfg.read_bytes(),self.original.replace(b'WEB_ADDR=[::]:18080',b'WEB_ADDR=127.0.0.1:18080'))
        self.assertEqual(Path(result['backup']).read_bytes(),self.original)
        self.assertFalse(self.rescue.exists());self.assertIn(['systemctl','restart','starhub-control.service'],self.calls)
        self.assertNotIn('private-token',json.dumps(result));self.assertNotIn('private-key',json.dumps(result));self.assertNotIn('private-db',json.dumps(result))

    def test_https_failure_never_changes_configuration(self):
        with patch.object(p,'_probe_https',side_effect=ValueError('bad HTTPS origin')):
            with self.assertRaisesRegex(ValueError,'bad HTTPS'):p.disable_public(self.root,self.env,self.cfg)
        self.assertEqual(self.cfg.read_bytes(),self.original);self.assertFalse(self.calls);self.assertFalse(list(self.root.glob('control.env.before-ip-close.*')))

    def test_missing_domain_stops_before_mutation(self):
        self.site={'domain':'','https':True}
        with self.assertRaisesRegex(ValueError,'working HTTPS'):p.disable_public(self.root,self.env,self.cfg)
        self.assertEqual(self.cfg.read_bytes(),self.original);self.assertFalse(self.calls)

    def test_unhealthy_restart_restores_exact_original_configuration(self):
        with patch.object(p,'_wait_control',side_effect=ValueError('health failed')):
            with self.assertRaisesRegex(ValueError,'original configuration restored'):p.disable_public(self.root,self.env,self.cfg)
        self.assertEqual(self.cfg.read_bytes(),self.original)
        self.assertEqual(self.calls.count(['systemctl','restart','starhub-control.service']),2)

    def test_duplicate_listener_entries_are_refused(self):
        self.cfg.write_bytes(self.original+b'WEB_ADDR=0.0.0.0:19000\n')
        with self.assertRaisesRegex(ValueError,'Exactly one'):p.disable_public(self.root,self.env,self.cfg)
        self.assertFalse(self.calls);self.assertFalse(list(self.root.glob('control.env.before-ip-close.*')))

    def test_already_private_keeps_original_backup_and_does_not_restart(self):
        private=self.original.replace(b'WEB_ADDR=[::]:18080',b'WEB_ADDR=127.0.0.1:18080');self.cfg.write_bytes(private);self.env['WEB_ADDR']='127.0.0.1:18080'
        result=p.disable_public(self.root,self.env,self.cfg)
        self.assertFalse(result['changed']);self.assertFalse(self.calls);self.assertEqual(self.cfg.read_bytes(),private)

    def test_control_service_must_belong_to_same_installation(self):
        unit=self.units/'starhub-control.service';unit.write_text('ExecStart=/other/harbor-control\nEnvironmentFile='+str(self.cfg)+'\n')
        with self.assertRaisesRegex(ValueError,'does not belong'):p._installation(self.root,self.cfg)
        unit.write_text('ExecStart='+str(self.root/'bin/harbor-control')+'\nEnvironmentFile='+str(self.cfg)+'\n')
        self.assertEqual(p._installation(self.root,self.cfg),self.cfg)

    def test_public_or_foreign_database_is_refused_before_container_mutation(self):
        self.info[0]['HostConfig']['PortBindings']['5432/tcp'][0]['HostIp']='0.0.0.0'
        with self.assertRaisesRegex(ValueError,'loopback'):p.enable_autostart(self.root,self.env)
        self.assertFalse(any(a[:2] in (['docker','update'],['docker','start']) for a in self.calls))

    def test_autostart_checks_stopped_database_and_enables_only_owned_timers(self):
        (self.root/'scripts').mkdir()
        for name,marker,script in [('starhub-gateway','# starhub-owned-gateway-v1\n','gateway-worker.py'),('starhub-panel-updater','# starhub-owned-panel-updater-v1\n','panel-updater.py')]:
            (self.root/'scripts'/script).write_text('# fixture worker')
            (self.units/(name+'.service')).write_text(marker+'ExecStart=/usr/bin/python3 '+str(self.root/'scripts'/script)+'\n')
            (self.units/(name+'.timer')).write_text(marker+'[Timer]\nUnit='+name+'.service\n')
        result=p.enable_autostart(self.root,self.env)
        self.assertTrue(result['complete']);self.assertFalse(result['missing_helpers'])
        self.assertIn(['docker','update','--restart','unless-stopped','starhub-control-db'],self.calls)
        self.assertLess(self.calls.index(['docker','start','starhub-control-db']),self.calls.index(['systemctl','enable','--now','starhub-control.service']))
        enabled=[a[-1] for a in self.calls if a[:3]==['systemctl','enable','--now']]
        self.assertIn('starhub-gateway.timer',enabled);self.assertNotIn('starhub-gateway.service',enabled);self.assertNotIn('starhub-panel-rescue.service',enabled)

    def test_missing_helpers_are_reported_without_false_complete_status(self):
        result=p.enable_autostart(self.root,self.env)
        self.assertFalse(result['complete']);self.assertEqual(set(result['missing_helpers']),{'starhub-gateway.timer','starhub-panel-updater.timer'})

    def test_unowned_helper_is_refused_before_starting_services(self):
        (self.units/'starhub-gateway.service').write_text('ExecStart=/other/helper\n');(self.units/'starhub-gateway.timer').write_text('[Timer]\nUnit=other.service\n')
        with self.assertRaisesRegex(ValueError,'ownership'):p.enable_autostart(self.root,self.env)
        self.assertFalse(self.calls)

    def test_database_password_mismatch_is_refused(self):
        self.info[0]['Config']['Env'][-1]='POSTGRES_PASSWORD=other-installation'
        with self.assertRaisesRegex(ValueError,'ownership'):p._database_owner(self.env,self.info)

    def test_system_nginx_is_enabled_only_when_its_unit_matches(self):
        (self.root/'gateway.json').write_text(json.dumps({'owner':'starhub-gateway-v1','nginx':'/usr/sbin/nginx'}))
        def run(argv,**kwargs):
            if '--property=LoadState' in argv:return types.SimpleNamespace(stdout='loaded\n')
            if '--property=ExecStart' in argv:return types.SimpleNamespace(stdout='{ path=/usr/sbin/nginx ; argv[]=/usr/sbin/nginx ; }')
            return self.run_command(argv,**kwargs)
        with patch.object(p,'_run',side_effect=run):p.enable_autostart(self.root,self.env)
        self.assertIn(['systemctl','enable','--now','nginx.service'],self.calls)

    def test_baota_nginx_is_left_to_its_existing_supervisor(self):
        (self.root/'gateway.json').write_text(json.dumps({'owner':'starhub-gateway-v1','nginx':'/www/server/nginx/sbin/nginx'}))
        result=p.enable_autostart(self.root,self.env)
        self.assertEqual(result['nginx'],'external_or_unconfigured')
        self.assertFalse(any('nginx.service' in a for a in self.calls))

    def test_existing_always_database_policy_is_preserved(self):
        self.info[0]['HostConfig']['RestartPolicy']['Name']='always'
        result=p.enable_autostart(self.root,self.env)
        self.assertEqual(result['database_restart'],'always');self.assertFalse(any(a[:2]==['docker','update'] for a in self.calls))

    def test_public_internal_api_or_share_listener_blocks_startup(self):
        for key in ('HTTP_ADDR','SHARE_ADDR'):
            env={**self.env,key:'0.0.0.0:8080'}
            with self.assertRaisesRegex(ValueError,'loopback'):p.enable_autostart(self.root,env)
        self.assertFalse(self.calls)

    def test_unfinished_update_blocks_manual_service_changes(self):
        state=self.root/'updater';state.mkdir();(state/'journal.json').write_text('{"state":"applying"}')
        fake=types.SimpleNamespace(LOCK_EX=1,LOCK_NB=2,flock=lambda *a:None)
        with patch.object(p,'UPDATER_STATE',state),patch.dict(sys.modules,{'fcntl':fake}):
            with self.assertRaisesRegex(ValueError,'unfinished update'):
                with p._maintenance_guard():self.fail('Update recovery was bypassed')
        self.assertFalse(self.calls)

    def test_busy_updater_lock_blocks_manual_service_changes(self):
        state=self.root/'updater';state.mkdir()
        def busy(*a):raise BlockingIOError()
        fake=types.SimpleNamespace(LOCK_EX=1,LOCK_NB=2,flock=busy)
        with patch.object(p,'UPDATER_STATE',state),patch.dict(sys.modules,{'fcntl':fake}),patch.object(p.time,'sleep'):
            with self.assertRaisesRegex(ValueError,'updater is busy'):
                with p._maintenance_guard():self.fail('Update lock was bypassed')
        self.assertFalse(self.calls)

class MaintenanceProbeTests(unittest.TestCase):
    def test_placeholder_and_ip_domains_are_rejected_before_curl(self):
        for domain in ('YOUR_PANEL_DOMAIN','192.0.2.1','a; include /secret','https://panel.example.com'):
            with patch.object(p,'_run') as run:
                with self.assertRaises(ValueError):p._probe_https(domain,'0.25.5')
                run.assert_not_called()

    def test_https_probe_requires_matching_panel_and_never_sends_admin_token(self):
        with patch.object(p.shutil,'which',return_value='/usr/bin/curl'),patch.object(p,'_run',return_value=types.SimpleNamespace(stdout=b'{"version":"other-panel"}')) as run:
            with self.assertRaisesRegex(ValueError,'panel version'):p._probe_https('panel.example.com','0.25.5')
            argv=run.call_args.args[0];self.assertNotIn('-k',argv);self.assertNotIn('-L',argv);self.assertNotIn('Authorization',' '.join(argv))

    def test_local_api_redirects_are_refused(self):
        with self.assertRaisesRegex(ValueError,'redirects'):p._NoRedirect().redirect_request(None,None,302,'',{},'https://external.example')

if __name__=='__main__':unittest.main()

import hashlib,importlib.util,io,json,os,sys,tempfile,types,unittest,zipfile
from pathlib import Path
from unittest.mock import patch

if os.name=='nt':sys.modules.setdefault('fcntl',types.SimpleNamespace())
spec=importlib.util.spec_from_file_location('panel_updater',Path(__file__).with_name('panel-updater.py'));u=importlib.util.module_from_spec(spec);spec.loader.exec_module(u)

class UpdateTests(unittest.TestCase):
    def test_archive_rejects_traversal_and_symlinks(self):
        for name,mode in [('../escape',0),('/absolute',0),('bin/link',0o120777<<16),('bin\\escape',0)]:
            with self.subTest(name=name),tempfile.TemporaryDirectory() as d:
                root=Path(d);archive=root/'a.zip'
                with zipfile.ZipFile(archive,'w') as z:
                    item=zipfile.ZipInfo(name);item.external_attr=mode;z.writestr(item,'bad')
                with self.assertRaises(ValueError):u.extract(archive,root/'stage')
    def test_extract_only_bounded_installation_files(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d);archive=root/'a.zip';stage=root/'stage'
            with zipfile.ZipFile(archive,'w') as z:
                z.writestr('bin/harbor-control',b'\x7fELFtest');z.writestr('web/dist/index.html','ok');z.writestr('scripts/evil.sh','bad');z.writestr('scripts/panel-updater.py','verified')
            u.extract(archive,stage)
            self.assertFalse((stage/'scripts/evil.sh').exists());self.assertTrue((stage/'scripts/panel-updater.py').exists())
    def test_download_checksum_and_redirect_bounds(self):
        class Response(io.BytesIO):
            def __enter__(self):return self
            def __exit__(self,*a):self.close()
        with tempfile.TemporaryDirectory() as d,patch.object(u.urllib.request,'build_opener') as opener:
            opener.return_value.open.return_value=Response(b'corrupted')
            asset={'browser_download_url':'https://github.com/czy-code-923/czy-code/releases/download/v0.24.0-distribution/test.zip','size':9,'digest':'sha256:'+hashlib.sha256(b'expected!').hexdigest()}
            with self.assertRaises(ValueError):u.download(asset,Path(d)/'a.zip')
        for url in ('http://github.com/a','https://evil.test/a','https://github.com:8443/a','https://user:pass@github.com/a'):
            with self.assertRaises(ValueError):u.OfficialRedirect().redirect_request(None,None,302,'',{},url)
    def test_crash_before_backup_restarts_stopped_units(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d);reports=[];worker=u.Updater({'token':'fixture'},root,api=lambda p,b=None:reports.append((p,b)))
            worker.job={'id':'a'*32,'from_version':'0.23.7','state':'backing_up','services_stopped':True,'units':['harbor-control.service'],'applying':False};worker.save()
            with patch.object(u,'run') as run,patch.object(worker,'health') as health:worker.recover()
            run.assert_called_once_with('systemctl','start','harbor-control.service',timeout=40);health.assert_called_once_with('0.23.7')
            self.assertEqual(reports[-1][1]['state'],'failed')
    def test_crash_during_apply_uses_rollback_and_preserves_terminal_state(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d);reports=[];worker=u.Updater({'token':'fixture'},root,api=lambda p,b=None:reports.append((p,b)))
            worker.job={'id':'a'*32,'state':'applying','applying':True};worker.save()
            with patch.object(worker,'restore') as restore:worker.recover()
            restore.assert_called_once();self.assertEqual(json.loads(worker.journal.read_text())['state'],'rolled_back')
    def test_report_loss_after_success_never_reapplies_update(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d);worker=u.Updater({'token':'fixture'},root,api=lambda p,b=None:{})
            worker.job={'id':'a'*32,'state':'succeeded','applying':True};worker.save()
            with patch.object(worker,'restore') as restore:worker.recover()
            restore.assert_not_called()
    def test_invalid_claim_has_durable_terminal_report(self):
        with tempfile.TemporaryDirectory() as d:
            reports=[]
            job={'id':'b'*32,'from_version':'0.24.0','target_version':'0.23.8','state':'downloading'}
            def api(path,body=None):
                reports.append((path,body));return {'job':job} if path.endswith('/claim') else {}
            worker=u.Updater({'token':'fixture','layout':'legacy','platform':'linux-amd64'},Path(d),api=api)
            with self.assertRaises(ValueError):worker.once()
            self.assertEqual(json.loads(worker.journal.read_text())['state'],'failed')
            self.assertEqual(reports[-1][1]['state'],'failed')

if __name__=='__main__':unittest.main()

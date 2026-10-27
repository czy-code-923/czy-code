"""Verify bundled patched-core integrity and fail before creating an engine directory."""
import hashlib,importlib.util,io,json,os,sys,tempfile,unittest,zipfile
from pathlib import Path
from unittest.mock import patch

class EngineInstallerTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup);self.root=Path(self.temp.name)
        (self.root/'scripts').mkdir();bundle=self.root/'patched-engines';bundle.mkdir()
        self.binary=b'patched fixture';self.source=b'source fixture'
        (bundle/'sing-box').write_bytes(self.binary);(bundle/'sing-box-source.zip').write_bytes(self.source);(bundle/'LICENSE.sing-box').write_bytes(b'license')
        self.receipt={'platform':'linux-amd64','version':'1.14.4-harbor.1','binary_sha256':hashlib.sha256(self.binary).hexdigest(),'corresponding_source_sha256':hashlib.sha256(self.source).hexdigest()}
        self.save()
        original=Path(__file__).with_name('fetch-engines.py');spec=importlib.util.spec_from_file_location('engine_fetch',original);self.m=importlib.util.module_from_spec(spec);spec.loader.exec_module(self.m)
        self.m.__file__=str(self.root/'scripts/fetch-engines.py')
        # platform.machine() may invoke Windows ver through subprocess.run;
        # keep OS discovery separate from the compiler prohibition below.
        p=patch.object(self.m.platform,'machine',return_value='amd64');p.start();self.addCleanup(p.stop)
        self.argv=patch.object(sys,'argv',['fetch-engines.py','--os','linux','--arch','amd64']);self.argv.start();self.addCleanup(self.argv.stop)
        self.archives={}
        for repo,names,archive_name in [('frp',['frps','frpc'],'frp_0.71.1_linux_amd64.zip'),('Xray-core',['xray'],'Xray-linux-64.zip')]:
            b=io.BytesIO()
            with zipfile.ZipFile(b,'w') as z:
                for name in names:z.writestr(name,b'fixture-'+name.encode())
            self.archives[repo]=b.getvalue()
        def fetch(url):
            repo='frp' if '/frp/' in url else 'Xray-core'
            if '/releases/tags/' in url:
                version='v0.71.1' if repo=='frp' else 'v26.10.21'
                owner='fatedier/frp' if repo=='frp' else 'XTLS/Xray-core'
                # Both archive parsers are exercised by existing native fetch runs;
                # here use a small real tar archive for FRP's selected Linux path.
                name='frp_0.71.1_linux_amd64.tar.gz' if repo=='frp' else 'Xray-linux-64.zip'
                if repo=='frp':
                    import tarfile
                    b=io.BytesIO()
                    with tarfile.open(fileobj=b,mode='w:gz') as z:
                        for binary in ('frps','frpc'):
                            data=b'fixture-'+binary.encode();info=tarfile.TarInfo(binary);info.size=len(data);z.addfile(info,io.BytesIO(data))
                    self.archives[repo]=b.getvalue()
                return json.dumps({'assets':[{'name':name,'digest':'sha256:'+hashlib.sha256(self.archives[repo]).hexdigest(),'browser_download_url':'https://github.com/'+owner+'/releases/download/'+version+'/'+name}]}).encode()
            return self.archives[repo]
        p=patch.object(self.m,'fetch',side_effect=fetch);p.start();self.addCleanup(p.stop)
        p=patch.object(self.m.subprocess,'run',side_effect=AssertionError('A bundled core must not require Go'));p.start();self.addCleanup(p.stop)
    def save(self):(self.root/'patched-engines/receipt.json').write_text(json.dumps(self.receipt))
    def test_verified_bundle_needs_no_compiler(self):
        self.m.main();self.assertEqual((self.root/'engines/sing-box').read_bytes(),self.binary)
        self.assertTrue((self.root/'engines/xray').is_file())
        if os.name!='nt':self.assertEqual((self.root/'engines/sing-box-source.zip').stat().st_mode&0o777,0o644)
    def test_modified_core_refused_before_writing(self):
        (self.root/'patched-engines/sing-box').write_bytes(b'altered')
        with self.assertRaisesRegex(ValueError,'digest mismatch'):self.m.main()
        self.assertFalse((self.root/'engines').exists())
    def test_wrong_platform_refused(self):
        self.receipt['platform']='windows-arm64';self.save()
        with self.assertRaisesRegex(ValueError,'platform/version mismatch'):self.m.main()
    def test_existing_engines_are_never_overwritten(self):
        (self.root/'engines').mkdir();(self.root/'engines/keep').write_text('keep')
        with self.assertRaises(SystemExit):self.m.main()
        self.assertEqual((self.root/'engines/keep').read_text(),'keep')

if __name__=='__main__':unittest.main()

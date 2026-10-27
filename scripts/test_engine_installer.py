"""Test official release integrity, version selection and non-overwrite behavior."""
import hashlib,importlib.util,io,json,sys,tarfile,tempfile,unittest,zipfile
from pathlib import Path
from unittest.mock import patch

class EngineInstallerTests(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup);self.root=Path(self.tmp.name);(self.root/'scripts').mkdir()
        path=Path(__file__).with_name('fetch-engines.py');spec=importlib.util.spec_from_file_location('engine_fetch',path);self.m=importlib.util.module_from_spec(spec);spec.loader.exec_module(self.m);self.m.__file__=str(self.root/'scripts/fetch-engines.py')
        p=patch.object(self.m.platform,'machine',return_value='amd64');p.start();self.addCleanup(p.stop)
        p=patch.object(sys,'argv',['fetch-engines.py','--os','linux','--arch','amd64']);p.start();self.addCleanup(p.stop)
        self.urls=[];self.corrupt=False;self.missing=False
        self.archives={}
        for repo,names in [('SagerNet/sing-box',['sing-box']),('fatedier/frp',['frps','frpc']),('XTLS/Xray-core',['xray'])]:
            stream=io.BytesIO()
            if 'Xray' in repo:
                with zipfile.ZipFile(stream,'w') as z:
                    for name in names:z.writestr(name,b'official-'+name.encode())
            else:
                with tarfile.open(fileobj=stream,mode='w:gz') as z:
                    for name in names:
                        data=b'official-'+name.encode();member=tarfile.TarInfo('package/'+name);member.size=len(data);z.addfile(member,io.BytesIO(data))
            self.archives[repo]=stream.getvalue()
        source=io.BytesIO()
        with zipfile.ZipFile(source,'w') as z:z.writestr('sing-box-1.14.4/LICENSE','official license')
        def fetch(url):
            self.urls.append(url)
            if 'codeload' in url:return source.getvalue()
            repo=next(v for v in self.archives if '/'+v+'/' in url)
            if '/releases/latest' in url or '/releases/tags/' in url:
                version={'SagerNet/sing-box':'v1.14.4','fatedier/frp':'v0.71.1','XTLS/Xray-core':'v26.10.21'}[repo]
                name={'SagerNet/sing-box':'sing-box-1.14.4-linux-amd64.tar.gz','fatedier/frp':'frp_0.71.1_linux_amd64.tar.gz','XTLS/Xray-core':'Xray-linux-64.zip'}[repo]
                raw=self.archives[repo];asset={'name':name,'size':len(raw),'digest':'sha256:'+hashlib.sha256(raw).hexdigest(),'browser_download_url':'https://github.com/'+repo+'/releases/download/'+version+'/'+name}
                return json.dumps({'tag_name':version,'prerelease':False,'assets':[] if self.missing else [asset]}).encode()
            return self.archives[repo]+(b'tampered' if self.corrupt else b'')
        p=patch.object(self.m,'fetch',side_effect=fetch);p.start();self.addCleanup(p.stop)
    def test_latest_is_official_and_compiler_is_not_required(self):
        self.m.main();receipt=json.loads((self.root/'engines/receipt.json').read_text());self.assertTrue(receipt['SagerNet/sing-box']['official']);self.assertEqual(receipt['SagerNet/sing-box']['version'],'v1.14.4');self.assertEqual((self.root/'engines/sing-box').read_bytes(),b'official-sing-box');self.assertTrue(any('/releases/latest' in u for u in self.urls));self.assertTrue((self.root/'engines/sing-box-source.zip').is_file())
    def test_modified_archive_is_refused_before_writing(self):
        self.corrupt=True
        with self.assertRaisesRegex(ValueError,'digest mismatch'):self.m.main()
        self.assertFalse((self.root/'engines').exists())
    def test_missing_platform_asset_is_refused(self):
        self.missing=True
        with self.assertRaisesRegex(ValueError,'platform asset'):self.m.main()
    def test_existing_engine_directory_is_never_overwritten(self):
        (self.root/'engines').mkdir();(self.root/'engines/keep').write_text('keep')
        with self.assertRaises(SystemExit):self.m.main()
        self.assertEqual((self.root/'engines/keep').read_text(),'keep')
    def test_specific_version_uses_an_exact_tag(self):
        with patch.object(sys,'argv',['fetch-engines.py','--os','linux','--arch','amd64','--sing-box-version','1.14.4']):self.m.main()
        self.assertTrue(any('/SagerNet/sing-box/releases/tags/v1.14.4' in u for u in self.urls))

if __name__=='__main__':unittest.main()

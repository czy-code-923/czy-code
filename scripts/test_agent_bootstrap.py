"""Exercise installer file permissions and failure cleanup in an isolated filesystem.

Downloads and system commands are fakes; no service/account/host is modified.
Use Linux for the Unix permission assertions.
"""
import hashlib, importlib.util, io, json, os, subprocess, sys, tempfile, unittest, zipfile
from pathlib import Path
from unittest.mock import patch

class BootstrapTests(unittest.TestCase):
    def setUp(self):
        self.temporary=tempfile.TemporaryDirectory(); self.addCleanup(self.temporary.cleanup)
        self.root=Path(self.temporary.name)
        source=Path(__file__).resolve().parents[1]/'internal/api/bootstrap/install.py'
        spec=importlib.util.spec_from_file_location('bootstrap_under_test',source)
        self.module=importlib.util.module_from_spec(spec)
        # Execute without creating a bytecode cache inside the Go embed directory.
        exec(compile(source.read_text(encoding='utf-8'),str(source),'exec'),self.module.__dict__)
        self.old_umask=os.umask(0o022); self.addCleanup(os.umask,self.old_umask)
        self.failure=False
        for relative in ('opt','etc/systemd/system','var/lib','usr/local/libexec'):(self.root/relative).mkdir(parents=True,exist_ok=True)
        package=io.BytesIO()
        with zipfile.ZipFile(package,'w') as archive:
            for name in ('harbor-agent','harbor-agent-cleanup','sing-box','frps','frpc','xray'):archive.writestr(name,b'fake-'+name.encode())
        self.archive=package.getvalue()
        def mapped(value):
            value=str(value)
            if value.startswith(('/opt/harbor-agent','/etc/harbor-agent','/etc/systemd/system/harbor-agent','/var/lib/harbor-agent','/usr/local/libexec/harbor-agent-cleanup')):return self.root/value.lstrip('/')
            return Path(value)
        def run(arguments,**kwargs):
            if arguments[0]=='id':return subprocess.CompletedProcess(arguments,1)
            if self.failure and arguments[:2]==('systemctl','is-active'):raise subprocess.CalledProcessError(1,arguments)
            return subprocess.CompletedProcess(arguments,0)
        def fetch(request,**kwargs):
            url=request.full_url
            if 'download=1' in url:data=self.archive
            elif url.endswith('/enroll'):data=json.dumps({'host_id':'a'*32,'agent_token':'b'*64}).encode()
            else:data=json.dumps({'size':len(self.archive),'sha256':hashlib.sha256(self.archive).hexdigest()}).encode()
            return io.BytesIO(data)
        for context in (patch.object(self.module,'Path',side_effect=mapped),patch.object(self.module.os,'geteuid',return_value=0,create=True),patch.object(self.module.platform,'machine',return_value='x86_64'),patch.object(self.module.subprocess,'run',side_effect=run),patch.object(self.module.time,'sleep'),patch.object(self.module.urllib.request,'build_opener')):
            result=context.start();self.addCleanup(context.stop)
            if context.attribute=='build_opener':result.return_value.open.side_effect=fetch
        self.argv=patch.object(sys,'argv',['install.py','https://control.example','c'*64]);self.argv.start();self.addCleanup(self.argv.stop)

    def test_success_keeps_binary_directory_traversable_and_credentials_private(self):
        self.module.main()
        binary=self.root/'opt/harbor-agent/harbor-agent'
        self.assertTrue(binary.is_file())
        if os.name!='nt':
            self.assertEqual((self.root/'opt/harbor-agent').stat().st_mode&0o777,0o755)
            self.assertEqual(binary.stat().st_mode&0o777,0o755)
            self.assertEqual((self.root/'etc/harbor-agent/agent.env').stat().st_mode&0o777,0o600)
        self.assertIn('AF_NETLINK',(self.root/'etc/systemd/system/harbor-agent.service').read_text())

    def test_failed_service_start_rolls_back_only_new_installation(self):
        self.failure=True
        with self.assertRaises(subprocess.CalledProcessError):self.module.main()
        self.module.rollback_install()
        for name in ('opt/harbor-agent','etc/harbor-agent','var/lib/harbor-agent','etc/systemd/system/harbor-agent.service'):self.assertFalse((self.root/name).exists())

    def test_existing_agent_is_preserved(self):
        folder=self.root/'opt/harbor-agent';folder.mkdir();(folder/'existing').write_text('keep')
        with self.assertRaisesRegex(ValueError,'Existing Agent'):self.module.main()
        self.module.rollback_install()
        self.assertEqual((folder/'existing').read_text(),'keep')

    def test_patched_core_source_and_license_are_installed_as_data(self):
        data=io.BytesIO()
        with zipfile.ZipFile(data,'w') as archive:
            for name in ('harbor-agent','harbor-agent-cleanup','sing-box','frps','frpc','xray','LICENSE.sing-box','sing-box-source.zip'):archive.writestr(name,b'fixture-'+name.encode())
        self.archive=data.getvalue();self.module.main()
        source=self.root/'opt/harbor-agent/sing-box-source.zip'
        self.assertEqual(source.read_bytes(),b'fixture-sing-box-source.zip')
        if os.name!='nt':self.assertEqual(source.stat().st_mode&0o777,0o644)

if __name__=='__main__':unittest.main()

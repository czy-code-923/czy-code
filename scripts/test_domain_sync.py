"""Tests the gateway file transaction with fake nginx/HTTP; never changes host nginx."""
import importlib.util
import io
import json
from pathlib import Path
import sys
import tempfile
import types
import unittest
from unittest.mock import patch

try:
    import fcntl
except ImportError:
    sys.modules['fcntl']=types.SimpleNamespace(LOCK_EX=1,LOCK_NB=2,flock=lambda *args:None)
spec=importlib.util.spec_from_file_location('harbor_domain_sync',Path(__file__).with_name('domain-sync.py'))
module=importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

class DomainSyncTests(unittest.TestCase):
    def test_rejects_nginx_injection(self):
        for value in ('a; include /secret;', 'a\nexample.com', '../example.com','https://example.com','a@b.com'):
            with self.assertRaises(ValueError): module.domain_name(value)
        self.assertEqual(module.domain_name('vps.example.com'),'vps.example.com')

    def scenario(self,fail=False):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);output=root/'harbor.conf';old=b'# harbor-managed-v1: old\n';new=b'# harbor-managed-v1: new\n';output.write_bytes(old);env=root/'private.env';env.write_text('ADMIN_TOKEN=unit-test-only\n')
            events=[];statuses=[];failed=False
            class Response(io.BytesIO):
                def __init__(self,request,data): super().__init__(json.dumps(data).encode());self.url=request.full_url
                def geturl(self):return self.url
            class Opener:
                def open(self,request,timeout=10):
                    if request.data: statuses.append(json.loads(request.data));return Response(request,{})
                    return Response(request,{'domain':'vps.example.com','https':False})
            def run(argv):
                nonlocal failed
                events.append(argv)
                if argv[0]=='/gateway':return new
                if fail and argv==['/nginx','-t'] and not failed:
                    failed=True;raise RuntimeError('fake nginx validation failed')
                return b''
            args=['domain-sync.py','--env',str(env),'--gateway','/gateway','--nginx','/nginx','--out',str(output),'--admin-cidrs','192.0.2.10/32','--allow-http','--apply']
            with patch.object(sys,'argv',args),patch.object(module.os,'geteuid',return_value=0,create=True),patch.object(module,'verify_output'),patch.object(module,'trusted_binary',side_effect=lambda p:p),patch.object(module,'run',side_effect=run),patch.object(module.urllib.request,'build_opener',return_value=Opener()):
                if fail:
                    with self.assertRaises(RuntimeError):module.main()
                    self.assertEqual(output.read_bytes(),old)
                    self.assertEqual(statuses[-1]['state'],'error')
                else:
                    module.main();self.assertEqual(output.read_bytes(),new);self.assertEqual(statuses[-1]['state'],'active')
                    events.clear();module.main();self.assertFalse(any(e[0]=='/nginx' for e in events),'unchanged gateway reloaded')
    def test_success_and_idle_no_reload(self):self.scenario()
    def test_validation_failure_restores_original(self):self.scenario(fail=True)

if __name__=='__main__':unittest.main()

"""No real credentials, network requests or service changes."""
import hashlib,hmac,io,json,types,unittest
from pathlib import Path
from unittest.mock import patch

module=types.ModuleType('agent_repair')
source=Path(__file__).resolve().parents[1]/'internal/api/bootstrap/repair.py'
exec(compile(source.read_text(encoding='utf-8'),str(source),'exec'),module.__dict__)

class AgentRepairTests(unittest.TestCase):
    def test_proves_origin_without_sending_token(self):
        values={'HOST_ID':'a'*32,'AGENT_TOKEN':'test-identity-not-a-real-token'}
        def response(request,**kwargs):
            self.assertNotIn(values['AGENT_TOKEN'],str(request.headers)+request.data.decode())
            self.assertNotIn('Authorization',request.headers)
            self.assertTrue(request.get_header('User-agent').startswith('Starhub-Agent-Repair/'))
            body=json.loads(request.data)
            key=hashlib.sha256(values['AGENT_TOKEN'].encode()).hexdigest().encode()
            proof=hmac.new(key,('starhub-control-origin-v1:'+body['nonce']+':'+body['origin']).encode(),hashlib.sha256).hexdigest()
            return io.BytesIO(json.dumps({'proof':proof}).encode())
        with patch.object(module.urllib.request,'build_opener') as opener:
            opener.return_value.open.side_effect=response
            module.verify('https://control.example',values)
    def test_wrong_proof_and_plaintext_are_rejected(self):
        values={'HOST_ID':'a'*32,'AGENT_TOKEN':'fixture'}
        with patch.object(module.urllib.request,'build_opener') as opener:
            opener.return_value.open.return_value=io.BytesIO(b'{"proof":"fake"}')
            with self.assertRaisesRegex(ValueError,'does not prove'):module.verify('https://control.example',values)
        for origin in ('http://control.example','https://u:p@control.example','https://control.example/path'):
            with self.assertRaises(ValueError):module.verify(origin,values)
    def test_release_redirects_stay_on_official_download_hosts(self):
        for url in ('http://github.com/a','https://evil.example/a','https://user:pass@github.com/a','https://github.com:8443/a'):
            with self.assertRaises(ValueError):module.ReleaseRedirect().redirect_request(None,None,302,'',{},url)

if __name__=='__main__':unittest.main()

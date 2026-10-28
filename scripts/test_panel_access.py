"""Recovery tests: configuration discovery, no token disclosure, bounded targets."""
import importlib.util, json, os, tempfile, unittest
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

if __name__=='__main__':unittest.main()

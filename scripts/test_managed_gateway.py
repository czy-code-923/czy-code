import importlib.util,sys,tempfile,types,unittest
from pathlib import Path
from unittest.mock import patch
try:import fcntl
except ImportError:sys.modules['fcntl']=types.SimpleNamespace(LOCK_EX=1,LOCK_NB=2,flock=lambda *args:None)
spec=importlib.util.spec_from_file_location('managed',Path(__file__).with_name('managed-gateway.py'));m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class ManagedGatewayTests(unittest.TestCase):
 def test_typed_reverse_and_no_injection(self):
  data=m.render('app.example.com','http://127.0.0.1:3000',True).decode();self.assertIn('proxy_pass http://127.0.0.1:3000;',data);self.assertIn('listen [::]:443',data);self.assertNotIn('proxy_pass',m.render('app.example.com').decode())
  for domain in ['evil; include /secret;','../bad','bad\nserver {}']:
   with self.assertRaises(ValueError):m.render(domain)
  for value in ['http://127.0.0.1:8080','http://evil.example.com:3000','http://127.0.0.1:3000/path','http://u:p@127.0.0.1:3000','https://127.0.0.1:3000']:
   with self.assertRaises(ValueError):m.upstream(value)
 def test_domain_ownership_protected(self):
  with tempfile.TemporaryDirectory() as directory:
   root=Path(directory);(root/'other.conf').write_text('server { server_name app.example.com; }')
   with self.assertRaises(ValueError):m.ensure_domain_available(root,'app.example.com',root/'ours.conf')
   m.ensure_domain_available(root,'new.example.com',root/'ours.conf')
 def test_atomic_rolls_back_invalid_candidate(self):
  with tempfile.TemporaryDirectory() as directory:
   path=Path(directory)/'test.conf';old=(m.HEADER+'old').encode();path.write_bytes(old);failed=False
   def run(argv):
    nonlocal failed
    if argv==['/nginx','-t'] and not failed:failed=True;raise RuntimeError('invalid nginx')
   with patch.object(m.sync,'verify_output'),patch.object(m.sync,'run',side_effect=run):
    with self.assertRaises(RuntimeError):m.atomic_vhost(path,m.render('test.example.com'),'/nginx')
    self.assertEqual(path.read_bytes(),old)
 def test_unmanaged_file_untouched(self):
  with tempfile.TemporaryDirectory() as directory:
   path=Path(directory)/'test.conf';path.write_bytes(b'not ours')
   with patch.object(m.sync,'verify_output'):
    with self.assertRaises(ValueError):m.atomic_vhost(path,m.render('test.example.com'),'/nginx')
   self.assertEqual(path.read_bytes(),b'not ours')
if __name__=='__main__':unittest.main()

import importlib.util,json,tempfile,unittest
from pathlib import Path
spec=importlib.util.spec_from_file_location('gateway_config',Path(__file__).with_name('gateway-config.py'));m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class GatewayConfigTests(unittest.TestCase):
 def test_public_https_pinning_verification_and_injection(self):
  resolver=lambda *a,**k:[(2,1,6,'',('93.184.215.14',443))]
  pinned=m.pinned_upstream('https://www.example.com/path',resolver)
  text=m.render('app.example.com','https://www.example.com/path',True,{'read_timeout':45},'a'*32,pinned).decode()
  for part in ['proxy_pass https://93.184.215.14:443/path;','proxy_ssl_verify on;','proxy_ssl_name www.example.com;','proxy_read_timeout 45s;','escape=json','reverse-'+('a'*32)+'.jsonl'] :self.assertIn(part,text)
  self.assertNotIn('$request_uri',text.split('log_format')[1].split(';')[0])
  for ip in ['127.0.0.1','10.0.0.1','169.254.169.254','100.64.0.1','::1','fc00::1']:
   with self.assertRaises(ValueError):m.pinned_upstream('https://www.example.com',lambda *a,**k:[(2,1,6,'',(ip,443))])
  with self.assertRaises(ValueError):m.render('app.example.com','https://example.com',True,{'host_header':'example.com; include /etc/passwd'})
  with self.assertRaises(ValueError):m.options({'read_timeout':True})
 def test_bytes_incremental_restart_rotation_and_partial_line(self):
  with tempfile.TemporaryDirectory() as directory:
   path=Path(directory)/'counter';path.write_bytes(b'{"up":10,"down":100}\n{"up":2')
   value=m.collect(path,None);self.assertEqual((value['up'],value['down']),(10,100))
   with path.open('ab') as f:f.write(b',"down":20}\n')
   value=m.collect(path,value);self.assertEqual((value['up'],value['down']),(12,120));self.assertEqual(m.collect(path,value),value)
   saved=Path(directory)/'state';m.save_state(saved,value);self.assertEqual(json.loads(saved.read_text()),value)
   restored=m.collect(path,None,(12,120));self.assertEqual((restored['up'],restored['down']),(12,120))
   path.rename(path.with_suffix('.old'));path.write_text('{"up":3,"down":30}\n');value=m.collect(path,value)
   self.assertEqual((value['up'],value['down']),(15,150))
if __name__=='__main__':unittest.main()

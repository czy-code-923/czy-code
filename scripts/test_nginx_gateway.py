"""Native syntax validation on CI; no production configuration or service is touched."""
import importlib.util,os,shutil,subprocess,tempfile,unittest
from pathlib import Path
spec=importlib.util.spec_from_file_location('gateway_config',Path(__file__).with_name('gateway-config.py'));m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class NativeGatewayTests(unittest.TestCase):
 def test_rendered_public_https_and_disabled_configs(self):
  nginx=shutil.which('nginx');openssl=shutil.which('openssl')
  if not nginx or not openssl or os.name=='nt':self.skipTest('native Linux Nginx and OpenSSL required')
  with tempfile.TemporaryDirectory() as directory:
   root=Path(directory);(root/'cert').mkdir();(root/'logs').mkdir()
   subprocess.run([openssl,'req','-x509','-newkey','rsa:2048','-nodes','-subj','/CN=app.example.com','-days','1','-keyout',str(root/'cert/privkey.pem'),'-out',str(root/'cert/fullchain.pem')],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
   for enabled in (True,False):
    text=m.render('app.example.com','https://www.example.com/api' if enabled else None,enabled,{'websocket':False,'host_header':'www.example.com'},'a'*32,'https://93.184.215.14:443/api').decode()
    text=text.replace('/etc/harbor/gateway-certificates/app.example.com',str(root/'cert')).replace('/var/log/harbor',str(root/'logs')).replace('listen 80;','listen 127.0.0.1:18080;').replace('listen [::]:80;','').replace('listen 443 ssl;','listen 127.0.0.1:18443 ssl;').replace('listen [::]:443 ssl;','')
    config=root/'nginx.conf';config.write_text('pid '+str(root/'nginx.pid')+'; error_log '+str(root/'error.log')+'; events {} http { '+text+' }')
    result=subprocess.run([nginx,'-t','-p',str(root)+'/', '-c',str(config)],capture_output=True,text=True)
    self.assertEqual(result.returncode,0,result.stderr)
if __name__=='__main__':unittest.main()

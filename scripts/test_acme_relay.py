import http.client
import importlib.util
import socket
import threading
import unittest
from pathlib import Path

spec=importlib.util.spec_from_file_location('relay',Path(__file__).with_name('acme-relay.py'))
relay=importlib.util.module_from_spec(spec);spec.loader.exec_module(relay)

class RelayTest(unittest.TestCase):
 def test_limits_redirects_to_challenge_and_exact_host(self):
  server=relay.Server(('127.0.0.1',0),relay.Handler);server.domain='sub.example.com';server.target='https://panel.example.com'
  thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
  def call(path,host,method='GET'):
   c=http.client.HTTPConnection('127.0.0.1',server.server_port,timeout=2);c.request(method,path,headers={'Host':host});r=c.getresponse();body=r.read();result=(r.status,r.getheader('Location'));c.close();return result
  try:
   self.assertEqual(call('/.well-known/acme-challenge/ABC_def-1','sub.example.com'),(302,'https://panel.example.com/.well-known/acme-challenge/ABC_def-1'))
   for path,host in [('/','sub.example.com'),('/.well-known/acme-challenge/../../secret','sub.example.com'),('/.well-known/acme-challenge/a?x=y','sub.example.com'),('/.well-known/acme-challenge/a','other.example.com')]:
    self.assertEqual(call(path,host)[0],404)
   self.assertNotEqual(call('/.well-known/acme-challenge/a','sub.example.com','POST')[0],302)
  finally:server.shutdown();server.server_close();thread.join()
if __name__=='__main__':unittest.main()


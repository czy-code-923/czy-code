import pathlib,subprocess,sys,tempfile,unittest

class SecretTests(unittest.TestCase):
 def test_installations_are_independent_and_repeated_prepare_preserves_secrets(self):
  source=pathlib.Path(__file__).with_name('prepare.py').read_bytes()
  with tempfile.TemporaryDirectory() as directory:
   tokens=[]
   for name in ['first','second']:
    root=pathlib.Path(directory)/name;(root/'scripts').mkdir(parents=True)
    script=root/'scripts/prepare.py';script.write_bytes(source)
    env=root/'.env';env.write_text('ADMIN_TOKEN=generate-at-least-32-random-bytes\nPOSTGRES_PASSWORD=generate-a-random-password\n')
    subprocess.run([sys.executable,str(script)],check=True,capture_output=True)
    before=env.read_bytes();subprocess.run([sys.executable,str(script)],check=True,capture_output=True)
    self.assertEqual(before,env.read_bytes())
    values=dict(line.split('=',1) for line in before.decode().splitlines() if '=' in line)
    self.assertEqual(len(values['ADMIN_TOKEN']),64);self.assertNotIn('generate-',values['ADMIN_TOKEN']);tokens.append(values['ADMIN_TOKEN'])
   self.assertNotEqual(*tokens)

if __name__=='__main__':unittest.main()

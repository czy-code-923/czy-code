import importlib.util,json,tempfile,unittest
from pathlib import Path
from unittest.mock import patch
spec=importlib.util.spec_from_file_location('uninstall',Path(__file__).with_name('uninstall.py'));u=importlib.util.module_from_spec(spec);spec.loader.exec_module(u)

class UninstallTests(unittest.TestCase):
    def fixture(self,root):
        root.mkdir();(root/'control.env').write_text('ADMIN_TOKEN='+'x'*40+'\nDATABASE_URL=postgres://local/test\n')
        (root/'bin').mkdir();(root/'bin/harbor-control').write_text('test-binary')
        (root/'web').mkdir();(root/'web/index.html').write_text('test-web')
    def test_default_removes_runtime_but_preserves_private_state(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory).resolve()/'starhub-control';self.fixture(root)
            paths,roots=u.plan(root,'quickstart',False)
            for path in paths:u.remove(path,roots)
            self.assertTrue((root/'control.env').is_file());self.assertFalse((root/'bin').exists())
    def test_purge_is_explicit_and_bounded(self):
        with tempfile.TemporaryDirectory() as directory:
            parent=Path(directory).resolve();root=parent/'starhub-control';self.fixture(root);other=parent/'unrelated';other.mkdir()
            paths,roots=u.plan(root,'quickstart',True)
            with self.assertRaises(ValueError):u.remove(other,roots)
            for path in paths:u.remove(path,roots)
            self.assertFalse(root.exists());self.assertTrue(other.exists())
    def test_unsafe_prefix_is_rejected_before_mutation(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory).resolve()/'unrelated';self.fixture(root)
            with self.assertRaises(ValueError):u.plan(root,'quickstart',False)
            self.assertTrue((root/'bin/harbor-control').exists())
    def test_database_ownership_is_required(self):
        row={'Config':{'Image':'postgres:17-alpine','Env':['POSTGRES_USER=other','POSTGRES_DB=other']},'Mounts':[{'Destination':'/var/lib/postgresql/data','Type':'volume','Name':'unrelated'}]}
        fake=type('Result',(),{'returncode':0,'stdout':json.dumps([row]),'stderr':''})()
        with patch.object(u.shutil,'which',return_value='docker'),patch.object(u.subprocess,'run',return_value=fake):
            with self.assertRaises(ValueError):u.database_container('starhub-control-db')
    def test_windows_task_must_target_selected_root(self):
        xml='<Task><Actions><Exec><Arguments>-File C:\\Other\\run.ps1</Arguments></Exec></Actions></Task>'
        fake=type('Result',(),{'returncode':0,'stdout':xml,'stderr':''})()
        with patch.object(u.subprocess,'run',return_value=fake):
            with self.assertRaises(ValueError):u.owned_windows_task(Path('C:/ProgramData/StarhubControl'))

if __name__=='__main__':unittest.main()

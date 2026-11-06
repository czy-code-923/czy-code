"""Pre-publish fresh-install check. Never runs on a user's system."""
import importlib.util,os,shutil,subprocess,sys,tempfile
from pathlib import Path

def main():
    if sys.platform!='linux' or os.geteuid()!=0 or os.environ.get('GITHUB_ACTIONS')!='true' or os.environ.get('RUNNER_ENVIRONMENT')!='github-hosted':raise ValueError('Disposable GitHub-hosted root VM required')
    if any(Path(p).exists() for p in ('/opt/starhub-control','/opt/harbor','/etc/harbor')):raise ValueError('Existing installation refused')
    source=Path(__file__).resolve().parents[1]
    with tempfile.TemporaryDirectory(prefix='starhub-gateway-preflight-',dir='/var/tmp') as d:
        dist=Path(d);(dist/'bin').mkdir();(dist/'web').mkdir()
        shutil.copytree(source/'scripts',dist/'scripts');shutil.copytree(source/'web/dist',dist/'web/dist')
        for name in ('control','gateway'):
            subprocess.run([os.environ.get('STARHUB_TEST_GO','go'),'build','-trimpath','-o',str(dist/'bin'/('harbor-'+name)),'./cmd/'+name],cwd=source,check=True,timeout=300)
        try:
            subprocess.run([sys.executable,str(dist/'scripts/quickstart.py'),'--origin','http://127.0.0.1:18080'],check=True,timeout=900)
            s=importlib.util.spec_from_file_location('gateway_native_preflight',source/'scripts/test_gateway_native.py');m=importlib.util.module_from_spec(s);s.loader.exec_module(m);m.verify()
        finally:
            root=Path('/opt/starhub-control')
            if (root/'control.env').is_file():subprocess.run([sys.executable,str(source/'scripts/uninstall.py'),'--prefix',str(root),'--apply','--purge-data'],check=True,timeout=120)
            # Only helper-created empty accounting state, never user or certificate data.
            state=Path('/etc/harbor/gateway-usage.json')
            if state.exists() and state.read_text().strip()=='{}':state.unlink()
            directory=Path('/etc/harbor')
            if directory.exists():directory.rmdir()
    print('Gateway fresh-install pre-publication gate: PASS',flush=True)

if __name__=='__main__':main()

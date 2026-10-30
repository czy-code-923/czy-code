"""Isolated GitHub VM only: real systemd update, DB snapshot and rollback tests."""
import hashlib,importlib.util,json,os,secrets,shutil,subprocess,sys,tempfile,time,zipfile
from pathlib import Path
from unittest.mock import patch

def main():
    if os.geteuid()!=0 or os.environ.get('GITHUB_ACTIONS')!='true' or os.environ.get('RUNNER_ENVIRONMENT')!='github-hosted':raise ValueError('Disposable hosted CI VM required')
    root=Path('/opt/starhub-control');distribution=Path(sys.argv[1]);source=Path(__file__).resolve().parents[1]
    spec=importlib.util.spec_from_file_location('panel_updater',source/'scripts/panel-updater.py');u=importlib.util.module_from_spec(spec);spec.loader.exec_module(u)
    install=u.load_install(root/'control.env');api=lambda p,b=None:u.local_api(install['token'],p,b)
    version=api('session')['version'];test_version='0.99.0'
    subprocess.run(['systemctl','stop','starhub-panel-updater.timer','starhub-panel-updater.service'],check=True)
    sentinel=api('proxy-users',{'name':'update-backup-sentinel','enabled':True,'expires_at':'2027-01-01T00:00:00Z'})['id']
    version_file=source/'internal/buildinfo/version.go';original=version_file.read_text()
    try:
        with tempfile.TemporaryDirectory(prefix='starhub-updater-ci-',dir='/var/lib') as directory:
            state=Path(directory);candidate=state/'candidate';candidate.mkdir()
            try:
                version_file.write_text(original.replace('"'+version+'"','"'+test_version+'"'))
                subprocess.run([os.environ.get('STARHUB_TEST_GO','go'),'build','-trimpath','-ldflags=-s -w','-o',str(candidate/'harbor-control'),'./cmd/control'],cwd=source,check=True,timeout=240)
            finally:version_file.write_text(original)
            archive=state/'candidate.zip'
            with zipfile.ZipFile(archive,'w',zipfile.ZIP_DEFLATED) as z:
                z.write(candidate/'harbor-control','bin/harbor-control')
                for name in ('panel-updater.py','install-panel-updater.py'):z.write(distribution/'scripts'/name,'scripts/'+name)
                for file in (distribution/'web/dist').rglob('*'):
                    if file.is_file():z.write(file,'web/dist/'+file.relative_to(distribution/'web/dist').as_posix())
            digest=hashlib.sha256(archive.read_bytes()).hexdigest();asset={'size':archive.stat().st_size,'digest':'sha256:'+digest}
            def copy_download(a,path):
                shutil.copyfile(archive,path)
                if hashlib.sha256(path.read_bytes()).hexdigest()!=digest:raise ValueError('test distribution checksum mismatch')
            def make_job():
                job={'id':secrets.token_hex(16),'from_version':version,'target_version':test_version}
                sql="INSERT INTO panel_updates(tenant_id,id,from_version,target_version) VALUES('self','"+job['id']+"','"+version+"','"+test_version+"')"
                u.run('docker','exec',install['container'],'psql','-U','harbor','-d','harbor','-v','ON_ERROR_STOP=1','-c',sql,stdout=subprocess.DEVNULL)
                claimed=api('panel-updates/worker/claim',{})['job'];assert claimed['id']==job['id'];return job
            worker=u.Updater(install,state/'success',api);worker.state.mkdir(mode=0o700)
            job=make_job()
            try:
                with patch.object(u,'official_release',return_value=asset),patch.object(u,'download',side_effect=copy_download):worker.perform(job)
                assert api('session')['version']==test_version
                assert api('panel-updates')['items'][0]['state']=='succeeded'
                assert (Path(worker.job['directory'])/'database.dump').stat().st_size>100
                print('Actual native panel binary upgraded and verified with PostgreSQL backup: PASS',flush=True)
            finally:
                if worker.job and worker.job.get('applying'):
                    worker.restore();worker.phase('rolled_back','Disposable CI successful-update test restored its original version')
            assert api('session')['version']==version
            assert api('proxy-users/'+sentinel)['name']=='update-backup-sentinel'
            failure=u.Updater(install,state/'failure',api);failure.state.mkdir(mode=0o700);job=make_job();original_health=failure.health;original_copy=u.atomic_copy;changed=False
            def fail_health(v):
                if v==test_version:raise RuntimeError('Injected health failure after actual application')
                return original_health(v)
            def corrupt_then_copy(src,dst):
                nonlocal changed
                original_copy(src,dst)
                if not changed and dst==root/'bin/harbor-control' and 'stage' in src.parts:
                    changed=True
                    u.run('docker','exec',install['container'],'psql','-U','harbor','-d','harbor','-v','ON_ERROR_STOP=1','-c',"UPDATE proxy_users SET name='changed-after-backup' WHERE id='"+sentinel+"'",stdout=subprocess.DEVNULL)
            with patch.object(u,'official_release',return_value=asset),patch.object(u,'download',side_effect=copy_download),patch.object(failure,'health',side_effect=fail_health),patch.object(u,'atomic_copy',side_effect=corrupt_then_copy):
                try:failure.perform(job)
                except RuntimeError:pass
                else:raise AssertionError('Injected update failure was not surfaced')
            assert api('session')['version']==version
            assert api('proxy-users/'+sentinel)['name']=='update-backup-sentinel'
            assert api('panel-updates')['items'][0]['state']=='rolled_back'
            print('Failed update restored actual binary, frontend and PostgreSQL data: PASS',flush=True)
    finally:
        version_file.write_text(original)
        subprocess.run(['systemctl','start','starhub-panel-updater.timer'],check=True)

if __name__=='__main__':main()

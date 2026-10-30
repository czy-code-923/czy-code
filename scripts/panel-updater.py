"""Local root updater for verified Starhub releases; no API supplied paths or commands.

Uses a private crash-recovery journal and an independent systemd timer. The
control service is stopped before database backup and remains stopped during
rollback, so no administrative or metering writes are lost across restore.
"""
import argparse,fcntl,hashlib,io,json,os,platform,re,shutil,stat,subprocess,sys,time,urllib.error,urllib.parse,urllib.request,zipfile
from pathlib import Path,PurePosixPath

REPO='czy-code-923/czy-code'
VERSION=re.compile(r'^[0-9]{1,5}\.[0-9]{1,5}\.[0-9]{1,5}$')
IDENTITY=re.compile(r'^[a-f0-9]{32}$')
TERMINAL={'succeeded','rolled_back','failed'}

def run(*args,**kw):
    return subprocess.run(list(args),check=True,timeout=kw.pop('timeout',180),**kw)

def trusted(path,directory=False):
    path=Path(path)
    if not path.is_absolute() or path.resolve()!=path or path.is_symlink():raise ValueError('Noncanonical installation path')
    st=path.stat()
    if st.st_uid!=0 or st.st_mode&0o022 or (directory and not path.is_dir()):raise ValueError('Root-owned non-writable installation required')
    return path

def load_install(env_path):
    env_path=trusted(env_path)
    if env_path.stat().st_mode&0o077:raise ValueError('Private installation environment required')
    values=dict(line.split('=',1) for line in env_path.read_text().splitlines() if '=' in line and not line.startswith('#'))
    if len(values.get('ADMIN_TOKEN',''))<32:raise ValueError('Installation token missing')
    if env_path==Path('/etc/harbor/control.env'):
        root=Path('/opt/harbor');layout='legacy';service='harbor-control.service';container='harbor-postgres'
    elif env_path.name=='control.env' and env_path.parent.name=='starhub-control':
        root=env_path.parent;layout='quickstart';service='starhub-control.service';container='starhub-control-db'
    else:raise ValueError('Unsupported installation layout')
    trusted(root,True);trusted(root/'bin',True);trusted(root/'bin/harbor-control')
    machine={'x86_64':'amd64','aarch64':'arm64','AMD64':'amd64'}.get(platform.machine())
    if sys.platform!='linux' or machine is None:raise ValueError('Linux amd64/arm64 systemd installation required')
    unit=trusted(Path('/etc/systemd/system')/service)
    if str(root/'bin/harbor-control') not in unit.read_text():raise ValueError('Control service ownership mismatch')
    db=urllib.parse.urlsplit(values.get('DATABASE_URL',''))
    if db.hostname not in ('127.0.0.1','localhost','::1') or db.path!='/harbor' or db.username!='harbor':raise ValueError('Local project database required')
    info=json.loads(run('docker','container','inspect',container,capture_output=True,text=True).stdout)
    if len(info)!=1:raise ValueError('Unexpected database container')
    c=info[0]['Config'];e=dict(v.split('=',1) for v in c.get('Env',[]) if '=' in v)
    if not c.get('Image','').startswith('postgres:') or e.get('POSTGRES_DB')!='harbor' or e.get('POSTGRES_USER')!='harbor' or e.get('POSTGRES_PASSWORD')!=urllib.parse.unquote(db.password or ''):raise ValueError('Database container ownership mismatch')
    ports=info[0].get('NetworkSettings',{}).get('Ports',{}).get('5432/tcp',[])
    if not any(p.get('HostIp') in ('127.0.0.1','::1') and int(p.get('HostPort',0))==(db.port or 5432) for p in ports):raise ValueError('Database port does not match installation')
    return {'root':root,'layout':layout,'service':service,'container':container,'platform':'linux-'+machine,'token':values['ADMIN_TOKEN']}

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,*args):raise ValueError('Local API redirects forbidden')

class OfficialRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,req,fp,code,msg,headers,newurl):
        parsed=urllib.parse.urlsplit(newurl)
        if parsed.scheme!='https' or parsed.username or parsed.port or parsed.hostname not in ('github.com','release-assets.githubusercontent.com','objects.githubusercontent.com'):raise ValueError('Unexpected release redirect')
        return super().redirect_request(req,fp,code,msg,headers,newurl)

def local_api(token,path,body=None):
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect())
    req=urllib.request.Request('http://127.0.0.1:8080/api/v1/'+path,data=None if body is None else json.dumps(body).encode(),headers={'Authorization':'Bearer '+token,'Content-Type':'application/json'})
    with opener.open(req,timeout=12) as response:
        data=response.read((1<<20)+1)
        if len(data)>1<<20:raise ValueError('Oversized local response')
        return json.loads(data)

def official_release(version,target):
    if not VERSION.fullmatch(version) or target not in ('linux-amd64','linux-arm64'):raise ValueError('Invalid update version/platform')
    tag='v'+version+'-distribution'
    req=urllib.request.Request('https://api.github.com/repos/'+REPO+'/releases/tags/'+tag,headers={'User-Agent':'Starhub-panel-updater'})
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect())
    with opener.open(req,timeout=20) as response:data=response.read((1<<20)+1)
    if len(data)>1<<20:raise ValueError('Oversized release metadata')
    release=json.loads(data)
    if release.get('tag_name')!=tag or release.get('draft') or release.get('prerelease'):raise ValueError('Published stable release required')
    name='harbor-'+version+'-'+target+'.zip';url='https://github.com/'+REPO+'/releases/download/'+tag+'/'+name
    assets=[a for a in release.get('assets',[]) if a.get('name')==name and a.get('browser_download_url')==url]
    if len(assets)!=1:raise ValueError('Official distribution asset missing')
    a=assets[0]
    if type(a.get('size')) is not int or not 0<a['size']<=250<<20 or not re.fullmatch(r'sha256:[a-f0-9]{64}',a.get('digest','')):raise ValueError('Asset checksum/size missing')
    return a

def download(asset,path):
    req=urllib.request.Request(asset['browser_download_url'],headers={'User-Agent':'Starhub-panel-updater'})
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),OfficialRedirect())
    total=0;digest=hashlib.sha256()
    with opener.open(req,timeout=120) as response,path.open('xb') as out:
        while chunk:=response.read(1<<20):
            total+=len(chunk)
            if total>asset['size']:raise ValueError('Distribution exceeds expected size')
            out.write(chunk);digest.update(chunk)
        out.flush();os.fsync(out.fileno())
    if total!=asset['size'] or 'sha256:'+digest.hexdigest()!=asset['digest']:raise ValueError('Distribution checksum mismatch')

def extract(archive,stage):
    allowed_scripts={'panel-updater.py','install-panel-updater.py','admin-token.py','panel-access.py','uninstall.py','managed-gateway.py','gateway-config.py','domain-sync.py'}
    with zipfile.ZipFile(archive) as z:
        if len(z.infolist())>5000 or sum(f.file_size for f in z.infolist())>700<<20:raise ValueError('Distribution size bounds exceeded')
        seen=set()
        for f in z.infolist():
            p=PurePosixPath(f.filename)
            if p.is_absolute() or '..' in p.parts or '\\' in f.filename or f.filename in seen or stat.S_ISLNK(f.external_attr>>16):raise ValueError('Unsafe distribution member')
            seen.add(f.filename)
            if f.is_dir():continue
            if not (p.parts[0]=='bin' and len(p.parts)==2 or p.parts[:2]==('web','dist') or p.parts[0]=='scripts' and p.name in allowed_scripts):continue
            target=stage/Path(*p.parts);target.parent.mkdir(parents=True,exist_ok=True,mode=0o755)
            with z.open(f) as source,target.open('xb') as out:shutil.copyfileobj(source,out)
            target.chmod(0o755 if p.parts[0] in ('bin','scripts') else 0o644)
    if not (stage/'bin/harbor-control').is_file() or not (stage/'web/dist/index.html').is_file():raise ValueError('Incomplete distribution')
    with (stage/'bin/harbor-control').open('rb') as binary:
        if binary.read(4)!=b'\x7fELF':raise ValueError('Expected Linux executable')

def write_json(path,value):
    temp=path.with_suffix('.new')
    with temp.open('w') as out:json.dump(value,out);out.flush();os.fsync(out.fileno())
    temp.chmod(0o600);os.replace(temp,path)
    if os.name=='posix':
        fd=os.open(path.parent,os.O_RDONLY);os.fsync(fd);os.close(fd)

def atomic_copy(source,target):
    if target.is_symlink():raise ValueError('Symlinked destination refused')
    target.parent.mkdir(parents=True,exist_ok=True,mode=0o755)
    temp=target.with_name(target.name+'.starhub-update')
    with source.open('rb') as inp,temp.open('wb') as out:shutil.copyfileobj(inp,out);out.flush();os.fsync(out.fileno())
    temp.chmod(source.stat().st_mode&0o777);os.replace(temp,target)

def active(service):return subprocess.run(['systemctl','is-active','--quiet',service],timeout=10).returncode==0

class Updater:
    def __init__(self,install,state,api=None):
        self.i=install;self.state=state;self.api=api or (lambda p,b=None:local_api(install['token'],p,b));self.journal=state/'journal.json';self.job=None
    def save(self):write_json(self.journal,self.job)
    def phase(self,state,detail=''):
        self.job['state']=state;self.job['detail']=detail;self.save();self.api('panel-updates/worker/report',{'id':self.job['id'],'state':state,'detail':detail})
    def health(self,version):
        for _ in range(60):
            try:
                if self.api('session')['version']==version:return
            except Exception:pass
            time.sleep(1)
        raise RuntimeError('Control version health check failed')
    def restore(self):
        for unit in self.job['units']:run('systemctl','stop',unit,timeout=40)
        for entry in self.job['files']:
            target=Path(entry['target'])
            if entry['existed']:atomic_copy(Path(entry['backup']),target)
            else:target.unlink(missing_ok=True)
        database=Path(self.job['directory'])/'database.dump'
        with database.open('rb') as dump:run('docker','exec','-i',self.i['container'],'pg_restore','-U','harbor','-d','harbor','--clean','--if-exists','--exit-on-error',stdin=dump,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=600)
        for unit in reversed(self.job['units']):run('systemctl','start',unit,timeout=40)
        self.health(self.job['from_version'])
    def recover(self):
        if not self.journal.exists():return
        self.job=json.loads(self.journal.read_text())
        if self.job.get('state') in TERMINAL:
            # An API report may have been lost after the local terminal commit.
            try:self.api('panel-updates/worker/report',{'id':self.job['id'],'state':self.job['state'],'detail':self.job.get('detail','')})
            except urllib.error.HTTPError as e:
                if e.code!=409:raise
            return
        if self.job.get('applying'):self.restore();terminal='rolled_back'
        else:
            if self.job.get('services_stopped'):
                for unit in reversed(self.job['units']):run('systemctl','start',unit,timeout=40)
                self.health(self.job['from_version'])
            terminal='failed'
        self.phase(terminal,'Interrupted update recovered; backup retained')
    def perform(self,job):
        if not IDENTITY.fullmatch(job['id']) or not VERSION.fullmatch(job['from_version']) or not VERSION.fullmatch(job['target_version']):raise ValueError('Invalid update job')
        if tuple(map(int,job['target_version'].split('.')))<=tuple(map(int,job['from_version'].split('.'))):raise ValueError('Downgrade refused')
        if self.api('session')['version']!=job['from_version']:raise ValueError('Installed version changed')
        directory=self.state/job['id'];directory.mkdir(mode=0o700)
        self.job={**job,'state':'downloading','directory':str(directory),'applying':False,'units':[],'files':[]};self.save()
        try:
            asset=official_release(job['target_version'],self.i['platform'])
            db_size=int(run('docker','exec',self.i['container'],'psql','-U','harbor','-d','harbor','-Atc',"SELECT pg_database_size('harbor')",capture_output=True,text=True).stdout.strip())
            if shutil.disk_usage(self.state).free<asset['size']*6+db_size*2+(128<<20):raise ValueError('Insufficient space for verified update and rollback')
            archive=directory/'distribution.zip';download(asset,archive);self.phase('verifying')
            stage=directory/'stage';stage.mkdir(mode=0o700);extract(archive,stage)
            self.phase('backing_up')
            root=self.i['root'];web=root/('web/dist' if self.i['layout']=='legacy' else 'web')
            mappings=[]
            for source in sorted((stage/'bin').iterdir()):
                target=root/'bin'/source.name
                if target.exists():mappings.append((source,target))
            for source in sorted((stage/'web/dist').rglob('*')):
                if source.is_file():mappings.append((source,web/source.relative_to(stage/'web/dist')))
            # Preserve old hashed frontend assets for browser tabs during update.
            for source in sorted((stage/'scripts').iterdir()):
                target=root/'scripts'/source.name if source.name in ('panel-updater.py','install-panel-updater.py','managed-gateway.py','gateway-config.py','domain-sync.py') else root/source.name
                mappings.append((source,target))
            backup=directory/'files';backup.mkdir(mode=0o700)
            for index,(source,target) in enumerate(mappings):
                if not target.resolve().is_relative_to(root.resolve()) or target.is_symlink():raise ValueError('Update target escapes installation')
                entry={'source':str(source),'target':str(target),'existed':target.exists(),'backup':str(backup/str(index))}
                if target.exists():shutil.copy2(target,entry['backup'])
                self.job['files'].append(entry)
            self.job['units']=[self.i['service']]
            if self.i['layout']=='legacy' and active('harbor-agent.service'):self.job['units'].insert(0,'harbor-agent.service')
            self.save()
            # Stop writes before snapshot; restart original services if backup fails.
            self.job['services_stopped']=True;self.save()
            for unit in self.job['units']:run('systemctl','stop',unit,timeout=40)
            try:
                with (directory/'database.dump').open('xb') as dump:run('docker','exec',self.i['container'],'pg_dump','-U','harbor','-d','harbor','-Fc',stdout=dump,stderr=subprocess.DEVNULL,timeout=600);dump.flush();os.fsync(dump.fileno())
                if (directory/'database.dump').stat().st_size<100:raise ValueError('Invalid database backup')
            except BaseException:
                for unit in reversed(self.job['units']):run('systemctl','start',unit,timeout=40)
                raise
            self.job['applying']=True;self.job['state']='applying';self.save()
            for entry in self.job['files']:atomic_copy(Path(entry['source']),Path(entry['target']))
            for unit in reversed(self.job['units']):run('systemctl','start',unit,timeout=40)
            self.health(job['target_version']);self.phase('checking')
            self.phase('succeeded','Verified update completed; database and file backups retained')
            self.job['detail']='Verified update completed; database and file backups retained';self.save()
            archive.unlink();shutil.rmtree(stage)
        except BaseException:
            # The service may have committed the success report before a lost
            # response. Never roll back a locally completed healthy update.
            if self.job.get('state')=='succeeded':return
            if self.job['applying']:
                self.restore();self.phase('rolled_back','Update failed; previous database and files restored')
            else:
                if self.job.get('services_stopped'):
                    for unit in reversed(self.job['units']):run('systemctl','start',unit,timeout=40)
                    self.health(job['from_version'])
                self.phase('failed','Update verification or backup failed; installation retained')
            raise
    def once(self):
        self.recover()
        self.api('panel-updates/worker/heartbeat',{'layout':self.i['layout'],'platform':self.i['platform']})
        job=self.api('panel-updates/worker/claim',{})['job']
        if job:
            # Persist the claim before any validation or I/O can fail. A crash
            # before a backup exists must still leave a recoverable job.
            self.job={**job,'applying':False,'units':[],'files':[]};self.save()
            try:self.perform(job)
            except BaseException:
                if not self.job.get('applying') and self.job.get('state') not in TERMINAL:self.phase('failed','Update preflight failed; installation retained')
                raise

def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--env',required=True);args=p.parse_args()
    if os.geteuid()!=0:raise ValueError('Root required')
    os.umask(0o077);install=load_install(Path(args.env))
    state=Path('/var/lib/starhub-updater')/install['layout'];state.mkdir(parents=True,exist_ok=True,mode=0o700);trusted(state,True)
    if state.stat().st_mode&0o077:raise ValueError('Private updater state required')
    lock=os.open(state/'lock',os.O_CREAT|os.O_RDWR|os.O_NOFOLLOW,0o600)
    try:fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
    except BlockingIOError:return
    Updater(install,state).once()

if __name__=='__main__':
    try:main()
    except Exception:print('Panel updater stopped; inspect the private update journal and systemd service',file=sys.stderr);sys.exit(1)

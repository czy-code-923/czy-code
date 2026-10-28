"""Uninstall only this control plane. Preview by default; --apply executes.

Database volumes, private environment files, certificates and backups survive by
default. --purge-data explicitly destroys the selected installation's database
volume and private state. It never removes Docker, PostgreSQL packages, Nginx,
other sites, Let's Encrypt certificates, or Agents on other hosts.
"""
import argparse,json,os,re,shutil,subprocess,sys,xml.etree.ElementTree as ET
from pathlib import Path

def run(*args,**kwargs):
    return subprocess.run(list(args),check=True,**kwargs)

def private_env(path):
    if path.is_symlink() or not path.is_file():raise ValueError('Installation private environment is missing or symlinked')
    result={}
    for line in path.read_text(encoding='utf-8-sig').splitlines():
        if line and not line.startswith('#') and '=' in line:
            key,value=line.split('=',1);result[key]=value
    if len(result.get('ADMIN_TOKEN',''))<32 or not result.get('DATABASE_URL'):raise ValueError('Not a recognized control installation')
    return result

def bounded_path(path,roots):
    path=Path(path).absolute()
    for root in roots:
        root=Path(root).absolute()
        if root.is_symlink():raise ValueError('Refusing symlinked installation root: '+str(root))
        # Resolve the final target before every recursive operation on Windows too.
        if path.is_relative_to(root) and path.resolve().is_relative_to(root.resolve()):return path
    raise ValueError('Removal path escapes the selected installation: '+str(path))

def remove(path,roots):
    path=bounded_path(path,roots)
    if path.is_symlink():path.unlink()
    elif path.is_dir():shutil.rmtree(path)
    elif path.exists():path.unlink()

def owned_units(root,layout):
    names=['starhub-control.service'] if layout=='quickstart' else ['harbor-control.service','harbor-agent.service','harbor-domain-sync.service','harbor-domain-sync.timer','harbor-managed-gateway.service','harbor-managed-gateway.timer','harbor-backup.service','harbor-backup.timer']
    names += ['starhub-panel-rescue.service']
    found=[]
    for name in names:
        path=Path('/etc/systemd/system')/name
        if not path.exists():continue
        if path.is_symlink():raise ValueError('Refusing symlinked unit: '+name)
        text=path.read_text()
        if name.endswith('.service') and str(root) not in text and not any(p in text for p in ['/etc/harbor/','/usr/local/libexec/harbor-']):
            raise ValueError('Unit ownership could not be verified: '+name)
        if name.endswith('.timer') and name.replace('.timer','.service') not in text:
            # Systemd's matching-name default is valid; timers must still name Harbor.
            if '[Timer]' not in text:raise ValueError('Unexpected timer file: '+name)
        found.append(path)
    return found

def database_container(name):
    if not shutil.which('docker'):return None
    p=subprocess.run(['docker','container','inspect',name],capture_output=True,text=True)
    if p.returncode:
        if 'No such' in p.stderr:return None
        raise RuntimeError('Cannot inspect Docker; nothing was removed')
    rows=json.loads(p.stdout)
    if len(rows)!=1:raise ValueError('Unexpected database container identity')
    item=rows[0];config=item['Config'];env=dict(v.split('=',1) for v in config.get('Env',[]) if '=' in v)
    if not config.get('Image','').startswith('postgres:') or env.get('POSTGRES_USER')!='harbor' or env.get('POSTGRES_DB')!='harbor':
        raise ValueError('Database container is not the expected project PostgreSQL')
    mounts=[m for m in item.get('Mounts',[]) if m['Destination']=='/var/lib/postgresql/data']
    if len(mounts)!=1 or mounts[0].get('Type')!='volume' or not re.fullmatch(r'(?:starhub-control-pgdata|harbor[-_][A-Za-z0-9_-]{1,80})',mounts[0].get('Name','')):
        raise ValueError('Database mount is not a recognized named project volume; remove it manually after backup')
    return {'name':name,'volume':mounts[0]['Name']}

def plan(root,layout,purge,windows=False):
    if layout=='legacy' and (windows or root!=Path('/opt/harbor')):raise ValueError('Legacy installation root must be /opt/harbor on Linux')
    if layout=='quickstart' and root.name.lower() not in ('starhubcontrol','starhub-control'):raise ValueError('Quickstart prefix must end in StarhubControl or starhub-control')
    if not root.is_absolute() or root.is_symlink() or root.resolve()!=root:raise ValueError('Absolute non-symlink installation path required')
    env_path=root/'control.env' if layout=='quickstart' else Path('/etc/harbor/control.env')
    private_env(env_path)
    if not (root/'bin'/('harbor-control.exe' if windows else 'harbor-control')).is_file():
        # A previous safe uninstall may have retained only data; allow explicit purge.
        if not purge:raise ValueError('Control binary missing; installation may already be uninstalled')
    paths=[root/p for p in ('bin','web','agent-releases','run-control.ps1','uninstall.py','admin-token.py','panel-access.py','panel-access.json')]
    if layout=='legacy':paths.extend(p for p in root.glob('agent-releases-v[0-9]*') if p.is_dir() and re.fullmatch(r'agent-releases-v[0-9]+',p.name))
    if purge:
        paths=[root]
        if layout=='legacy':paths += [Path('/etc/harbor'),Path('/var/lib/harbor')]
    roots=[root]+([Path('/etc/harbor'),Path('/var/lib/harbor')] if layout=='legacy' and purge else [])
    if layout=='legacy':
        helper=Path('/usr/local/libexec/harbor-agent-cleanup')
        if helper.exists():
            if helper.is_symlink() or not helper.is_file():raise ValueError('Unexpected cleanup helper type')
            paths.append(helper);roots.append(helper)
    for path in paths:bounded_path(path,roots)
    return paths,roots

def owned_windows_task(root):
    p=subprocess.run(['schtasks','/Query','/TN','StarhubControl','/XML'],capture_output=True,text=True)
    if p.returncode:
        if 'cannot find' in p.stderr.lower() or '找不到' in p.stderr:return False
        raise RuntimeError('Cannot inspect the Windows task; nothing was removed')
    task=ET.fromstring(p.stdout)
    arguments=[e.text or '' for e in task.iter() if e.tag.rsplit('}',1)[-1]=='Arguments']
    launcher=str(root/'run-control.ps1').replace('/','\\').casefold()
    if not any(launcher in value.replace('/','\\').casefold() for value in arguments):raise ValueError('StarhubControl task does not point to this installation')
    return True

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--layout',choices=['auto','quickstart','legacy'],default='auto')
    parser.add_argument('--prefix',help='Quickstart directory; default /opt/starhub-control or ProgramData/StarhubControl')
    parser.add_argument('--apply',action='store_true',help='Execute the printed uninstall plan')
    parser.add_argument('--purge-data',action='store_true',help='Irreversibly remove project private state and its database volume; backups still retained')
    args=parser.parse_args();windows=os.name=='nt';layout=args.layout
    default=Path(os.environ.get('ProgramData','C:/ProgramData'))/'StarhubControl' if windows else Path('/opt/starhub-control')
    if layout=='auto':
        candidates=[('quickstart',Path(args.prefix).absolute() if args.prefix else default)]
        if not windows and not args.prefix:candidates.append(('legacy',Path('/opt/harbor')))
        found=[(kind,path) for kind,path in candidates if (path/'control.env' if kind=='quickstart' else Path('/etc/harbor/control.env')).is_file()]
        if len(found)!=1:raise ValueError('Choose --layout explicitly; no unique recognized installation')
        layout,root=found[0]
    else:root=Path('/opt/harbor') if layout=='legacy' else Path(args.prefix).absolute() if args.prefix else default
    paths,roots=plan(root,layout,args.purge_data,windows)
    units=[] if windows else owned_units(root,layout)
    db=database_container('starhub-control-db' if layout=='quickstart' else 'harbor-postgres')
    task=owned_windows_task(root) if windows else False
    retained=root/'uninstall-retained.json'
    if not db and retained.is_file() and not retained.is_symlink():
        if not shutil.which('docker'):raise RuntimeError('Docker is required to inspect or remove the retained project volume')
        saved=json.loads(retained.read_text())
        if saved.get('root')!=str(root) or not re.fullmatch(r'(?:starhub-control-pgdata|harbor[-_][A-Za-z0-9_-]{1,80})',saved.get('volume','')):raise ValueError('Retained database identity is invalid')
        db={'name':saved['name'],'volume':saved['volume'],'retained':True}
    print('Installation:',root,'layout:',layout)
    print('Stop/remove project units or task:',', '.join(p.name for p in units) if not windows else 'StarhubControl (current-user task)')
    print('Remove project paths:',', '.join(str(p) for p in paths))
    print('Database:',('remove container '+db['name']+'; '+('DESTROY volume ' if args.purge_data else 'KEEP volume ')+db['volume']) if db else 'external/unavailable container: preserved')
    print('KEEP all backups, Nginx, other websites, Docker, firewall rules and OS packages.')
    print('Remote Agents are not contacted. Retire them in the panel before uninstalling the manager.')
    if layout=='legacy':print('Disable the panel domain reverse proxy in BaoTa separately; unrelated/shared Nginx configuration is not edited.')
    if not args.apply:
        print('Preview only. Add --apply to uninstall; add --purge-data only after verifying a backup.');return
    if not windows and os.geteuid()!=0:raise ValueError('Use sudo to remove system services')
    if windows:
        if task:
            subprocess.run(['schtasks','/End','/TN','StarhubControl'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
            subprocess.run(['schtasks','/Delete','/TN','StarhubControl','/F'],check=True,stdout=subprocess.DEVNULL)
        # Only stop the binary at the verified installation path, never other panels.
        script="$root='"+str(root).replace("'","''")+"'; Get-Process -Name harbor-control -ErrorAction SilentlyContinue | Where-Object {$_.Path -eq (Join-Path $root 'bin\\harbor-control.exe')} | Stop-Process -Force"
        run('powershell.exe','-NoProfile','-NonInteractive','-Command',script)
    else:
        if units:run('systemctl','disable','--now',*[p.name for p in units])
        for path in units:path.unlink()
        if units:run('systemctl','daemon-reload')
    if db:
        if not args.purge_data:
            with retained.open('w',encoding='utf-8') as stream:json.dump({'root':str(root),'name':db['name'],'volume':db['volume']},stream)
            retained.chmod(0o600)
        if not db.get('retained'):run('docker','container','rm','--force',db['name'],stdout=subprocess.DEVNULL)
        if args.purge_data:run('docker','volume','rm',db['volume'],stdout=subprocess.DEVNULL)
    for path in paths:remove(path,roots)
    print('Project runtime removed. '+(('Project private state and the identified database volume were destroyed.' if db else 'Project private state removed; external databases were preserved.') if args.purge_data else 'Private state and database volume retained for recovery.'))

if __name__=='__main__':
    try:main()
    except Exception as error:print('Uninstall stopped:',str(error),file=sys.stderr);sys.exit(1)

"""Install the independent updater timer for a recognized Linux control plane."""
import argparse,importlib.util,os,shutil,subprocess,sys
from pathlib import Path

def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--env',required=True);args=p.parse_args()
    if sys.platform!='linux' or os.geteuid()!=0:raise ValueError('Linux root required')
    source=Path(__file__).resolve().with_name('panel-updater.py')
    spec=importlib.util.spec_from_file_location('panel_updater',source);helper=importlib.util.module_from_spec(spec);spec.loader.exec_module(helper)
    env=Path(args.env);install=helper.load_install(env);root=install['root'];scripts=root/'scripts'
    if any(c.isspace() or c in "'\"%" for c in str(root)+str(env)):raise ValueError('Unsupported unit path')
    if scripts.exists():helper.trusted(scripts,True)
    else:scripts.mkdir(mode=0o755)
    for name in ('panel-updater.py','install-panel-updater.py'):
        origin=Path(__file__).resolve().with_name(name);target=scripts/name
        if origin!=target:
            if target.is_symlink():raise ValueError('Symlinked updater file refused')
            helper.atomic_copy(origin,target)
        target.chmod(0o755);os.chown(target,0,0)
    units=Path('/etc/systemd/system');service=units/'starhub-panel-updater.service';timer=units/'starhub-panel-updater.timer'
    marker='# starhub-owned-panel-updater-v1\n'
    for file in (service,timer):
        if file.exists() and (file.is_symlink() or not file.read_text().startswith(marker)):raise ValueError('Existing unowned updater unit refused')
    service.write_text(marker+'[Unit]\nDescription=Starhub verified panel updater\nAfter=network-online.target docker.service\n[Service]\nType=oneshot\nUser=root\nUMask=0077\nExecStart=/usr/bin/python3 '+str(scripts/'panel-updater.py')+' --env '+str(env)+'\nTimeoutStartSec=1800\nMemoryMax=192M\nNice=10\nNoNewPrivileges=true\nPrivateTmp=true\n')
    timer.write_text(marker+'[Unit]\nDescription=Starhub panel update queue\n[Timer]\nOnBootSec=20s\nOnUnitInactiveSec=10s\nAccuracySec=1s\nUnit=starhub-panel-updater.service\n[Install]\nWantedBy=timers.target\n')
    for file in (service,timer):file.chmod(0o644)
    subprocess.run(['systemctl','daemon-reload'],check=True)
    subprocess.run(['systemctl','enable','--now','starhub-panel-updater.timer'],check=True)
    subprocess.run(['systemctl','start','starhub-panel-updater.service'],check=True)
    print('Independent updater ready; version page will show installation health')

if __name__=='__main__':
    try:main()
    except Exception:print('Updater installation failed; verify root ownership, local database and systemd layout',file=sys.stderr);sys.exit(1)

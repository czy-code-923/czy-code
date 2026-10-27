"""Download pinned official releases, verify GitHub's SHA-256, extract only named binaries."""
from pathlib import Path
import argparse
import hashlib
import io
import json
import os
import platform
import subprocess
import sys
import tarfile
import tempfile
import urllib.request
import zipfile

def fetch(url):
    req=urllib.request.Request(url,headers={'User-Agent':'Harbor-engine-installer'})
    with urllib.request.urlopen(req,timeout=120) as response:
        data=response.read(160*1024*1024+1)
        if len(data)>160*1024*1024: raise ValueError('Download too large')
        return data

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--os',choices=['linux','windows'],default='windows' if os.name=='nt' else 'linux')
    parser.add_argument('--arch',choices=['amd64','arm64'],default='arm64' if platform.machine().lower() in ('arm64','aarch64') else 'amd64')
    parser.add_argument('--go',default='go')
    parser.add_argument('--output',help='New staged engine directory; existing directories are refused')
    args=parser.parse_args()
    project=Path(__file__).resolve().parents[1]
    dest=Path(args.output).resolve() if args.output else project/'engines'
    if dest.exists(): raise SystemExit('engines/ already exists; keep the running version and stage upgrades in a separate checkout.')
    output={};receipt={}
    for repo,version in [('SagerNet/sing-box','v1.14.4'),('fatedier/frp','v0.71.1'),('XTLS/Xray-core','v26.10.21')]:
        if repo=='SagerNet/sing-box':
            name='sing-box'+('.exe' if args.os=='windows' else '')
            bundle=project/'patched-engines'/name
            if bundle.exists():
                build=json.loads((bundle.parent/'receipt.json').read_text(encoding='utf-8'))
                if build.get('platform')!=args.os+'-'+args.arch or build.get('version')!='1.14.4-harbor.1':raise ValueError('Patched core platform/version mismatch')
                data=bundle.read_bytes()
                if hashlib.sha256(data).hexdigest()!=build['binary_sha256']:raise ValueError('Patched core digest mismatch')
                output[name]=data
                for legal in ('sing-box-source.zip','LICENSE.sing-box'):
                    output[legal]=(bundle.parent/legal).read_bytes()
            else:
                with tempfile.TemporaryDirectory(prefix='harbor-engine-build-') as tmp:
                    built=Path(tmp)/name
                    subprocess.run([sys.executable,str(project/'scripts/build-singbox.py'),'--go',args.go,'--os',args.os,'--arch',args.arch,'--output',str(built)],check=True)
                    build=json.loads(built.with_suffix(built.suffix+'.receipt.json').read_text())
                    output[name]=built.read_bytes()
                    output['sing-box-source.zip']=(built.parent/'sing-box-1.14.4-harbor.1-source.zip').read_bytes()
                    output['LICENSE.sing-box']=(project/'third_party/sing-box-security/LICENSE').read_bytes()+b'\n'+(project/'third_party/sing-box-security/COPYING').read_bytes()
            if hashlib.sha256(output['sing-box-source.zip']).hexdigest()!=build['corresponding_source_sha256']:raise ValueError('Corresponding source digest mismatch')
            receipt[repo]={'version':'1.14.4-harbor.1','build':build}
            continue
        release=json.loads(fetch('https://api.github.com/repos/'+repo+'/releases/tags/'+version))
        extension='zip' if args.os=='windows' or repo=='XTLS/Xray-core' else 'tar.gz'
        name=(f'sing-box-{version[1:]}-{args.os}-{args.arch}.{extension}' if 'sing-box' in repo else f'frp_{version[1:]}_{args.os}_{args.arch}.{extension}')
        if repo=='XTLS/Xray-core': name='Xray-'+args.os+'-'+('64' if args.arch=='amd64' else 'arm64-v8a')+'.zip'
        asset=next(a for a in release['assets'] if a['name']==name)
        url=asset['browser_download_url']
        if not url.startswith('https://github.com/'+repo+'/releases/download/'+version+'/'): raise ValueError('Unexpected asset URL')
        data=fetch(url);digest=hashlib.sha256(data).hexdigest()
        if asset.get('digest')!='sha256:'+digest: raise ValueError('Missing or mismatched official archive digest')
        names=['xray'] if repo=='XTLS/Xray-core' else ['sing-box'] if 'sing-box' in repo else ['frps','frpc']
        names=[n+('.exe' if args.os=='windows' else '') for n in names]
        archive=zipfile.ZipFile(io.BytesIO(data)) if extension=='zip' else tarfile.open(fileobj=io.BytesIO(data),mode='r:gz')
        for wanted in names:
            members=archive.namelist() if extension=='zip' else [m.name for m in archive.getmembers() if m.isfile()]
            matches=[n for n in members if n.split('/')[-1]==wanted]
            if len(matches)!=1: raise ValueError('Ambiguous engine archive')
            output[wanted]=archive.read(matches[0]) if extension=='zip' else archive.extractfile(matches[0]).read()
        archive.close()
        receipt[repo]={'version':version,'archive_sha256':digest,'url':url}
    dest.mkdir(mode=0o700)
    for name,data in output.items():
        path=dest/name
        with path.open('xb') as file: file.write(data)
        path.chmod(0o644 if name in ('LICENSE.sing-box','sing-box-source.zip') else 0o755)
    receipt['binaries']={name:hashlib.sha256(data).hexdigest() for name,data in output.items()}
    (dest/'receipt.json').write_text(json.dumps(receipt,indent=2),encoding='utf-8')
    print('Patched sing-box and verified official engines ready:',', '.join(output))

if __name__=='__main__': main()

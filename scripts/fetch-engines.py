"""Download pinned official releases, verify GitHub's SHA-256, extract only named binaries."""
from pathlib import Path
import argparse
import hashlib
import io
import json
import os
import platform
import tarfile
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
    args=parser.parse_args()
    dest=Path(__file__).resolve().parents[1]/'engines'
    if dest.exists(): raise SystemExit('engines/ already exists; keep the running version and stage upgrades in a separate checkout.')
    output={};receipt={}
    for repo,version in [('SagerNet/sing-box','v1.14.3'),('fatedier/frp','v0.71.1'),('XTLS/Xray-core','v26.10.21')]:
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
        path.chmod(0o755)
    receipt['binaries']={name:hashlib.sha256(data).hexdigest() for name,data in output.items()}
    (dest/'receipt.json').write_text(json.dumps(receipt,indent=2),encoding='utf-8')
    print('Official engines downloaded and SHA-256 verified:',', '.join(output))

if __name__=='__main__': main()

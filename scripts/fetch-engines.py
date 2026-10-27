"""Fetch official, checksum-verified sing-box / Xray / FRP releases.

latest resolves the latest stable version. Specific tags are accepted. Existing
engine directories are refused; runtime switching is handled by the Agent.
"""
from pathlib import Path
import argparse,hashlib,io,json,os,platform,re,tarfile,urllib.request,zipfile

def fetch(url):
    request=urllib.request.Request(url,headers={'User-Agent':'Starhub-official-engine-installer'})
    with urllib.request.urlopen(request,timeout=120) as response:
        raw=response.read((160<<20)+1)
        if len(raw)>160<<20:raise ValueError('Download exceeds limit')
        return raw

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--os',choices=['linux','windows'],default='windows' if os.name=='nt' else 'linux')
    p.add_argument('--arch',choices=['amd64','arm64'],default='arm64' if platform.machine().lower() in ('arm64','aarch64') else 'amd64')
    p.add_argument('--output');p.add_argument('--go',default='go',help='Compatibility argument; official binaries need no Go build')
    for engine in ('sing-box','xray','frp'):p.add_argument('--'+engine+'-version',default='latest')
    a=p.parse_args();dest=Path(a.output).resolve() if a.output else Path(__file__).resolve().parents[1]/'engines'
    if dest.exists():raise SystemExit('Output exists; stage a new engine directory')
    output={};receipt={}
    for repo,key in [('SagerNet/sing-box','sing_box'),('XTLS/Xray-core','xray'),('fatedier/frp','frp')]:
        version=getattr(a,key+'_version');path='latest'
        if version!='latest':
            if not re.fullmatch(r'v?[0-9]+\.[0-9]+\.[0-9]+(?:[-.][a-zA-Z0-9.-]+)?',version):raise ValueError('Invalid official version')
            version=version if version.startswith('v') else 'v'+version;path='tags/'+version
        release=json.loads(fetch('https://api.github.com/repos/'+repo+'/releases/'+path));tag=release['tag_name']
        if (version=='latest' and release.get('prerelease')) or version!='latest' and tag!=version:raise ValueError('Unexpected official release')
        if not re.fullmatch(r'v[0-9]+\.[0-9]+\.[0-9]+(?:[-.][a-zA-Z0-9.-]+)?',tag):raise ValueError('Unexpected official tag')
        extension='zip' if a.os=='windows' or key=='xray' else 'tar.gz'
        wanted=(f'sing-box-{tag[1:]}-{a.os}-{a.arch}.{extension}' if key=='sing_box' else f'frp_{tag[1:]}_{a.os}_{a.arch}.{extension}')
        if key=='xray':wanted='Xray-'+a.os+'-'+('64' if a.arch=='amd64' else 'arm64-v8a')+'.zip'
        asset=next((v for v in release['assets'] if v['name']==wanted),None)
        if not asset or not asset['browser_download_url'].startswith('https://github.com/'+repo+'/releases/download/'+tag+'/'):raise ValueError('Official platform asset unavailable')
        raw=fetch(asset['browser_download_url']);digest=hashlib.sha256(raw).hexdigest()
        if asset.get('digest')!='sha256:'+digest or len(raw)!=asset['size']:raise ValueError('Official archive digest mismatch')
        archive=zipfile.ZipFile(io.BytesIO(raw)) if extension=='zip' else tarfile.open(fileobj=io.BytesIO(raw),mode='r:gz')
        members=archive.namelist() if extension=='zip' else [m.name for m in archive.getmembers() if m.isfile()]
        def extract(name,required=True):
            matches=[v for v in members if v.split('/')[-1]==name]
            if not matches and not required:return None
            if len(matches)!=1:raise ValueError('Missing or ambiguous official archive member')
            item=archive.getinfo(matches[0]) if extension=='zip' else archive.getmember(matches[0])
            if (item.file_size if extension=='zip' else item.size)>160<<20:raise ValueError('Engine exceeds extraction limit')
            return archive.read(matches[0]) if extension=='zip' else archive.extractfile(item).read()
        names=['sing-box'] if key=='sing_box' else ['xray'] if key=='xray' else ['frps','frpc']
        for name in names:
            filename=name+('.exe' if a.os=='windows' else '');output[filename]=extract(filename)
        if key=='xray':
            for name in ('geoip.dat','geosite.dat'):
                body=extract(name,False)
                if body:output[name]=body
        if key=='sing_box':
            source=fetch('https://codeload.github.com/SagerNet/sing-box/zip/refs/tags/'+tag);output['sing-box-source.zip']=source
            with zipfile.ZipFile(io.BytesIO(source)) as z:
                licenses=[n for n in z.namelist() if n.split('/')[-1]=='LICENSE']
                if not licenses:raise ValueError('Official source license missing')
                output['LICENSE.sing-box']=z.read(min(licenses,key=len))
        archive.close();receipt[repo]={'version':tag,'official':True,'archive_sha256':digest,'url':asset['browser_download_url']}
    dest.mkdir(mode=0o700)
    executables={name+('.exe' if a.os=='windows' else '') for name in ('sing-box','xray','frps','frpc')}
    for name,raw in output.items():
        file=dest/name
        with file.open('xb') as target:target.write(raw)
        file.chmod(0o755 if name in executables else 0o644)
    receipt['binaries']={name:hashlib.sha256(raw).hexdigest() for name,raw in output.items()}
    (dest/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n',encoding='utf-8')
    print('Verified official engines:',', '.join(v['version'] for k,v in receipt.items() if k!='binaries'))

if __name__=='__main__':main()

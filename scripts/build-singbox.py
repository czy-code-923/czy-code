"""Build the pinned sing-box source with patched dependencies and a complete source archive.

Does not change any running service. Requires Go 1.27.3+ and network access.
"""
import argparse, hashlib, io, json, os, re, shutil, subprocess, tarfile, tempfile, urllib.request, zipfile
from pathlib import Path

SOURCE='https://codeload.github.com/SagerNet/sing-box/tar.gz/refs/tags/v1.14.4'
SOURCE_SHA256='0958ccd23b1ea9a141d715d15478b1e040fd04bcca19d8ff5529636b1b753e18'
TAGS='with_quic,with_utls,with_clash_api'
VERSION='1.14.4-harbor.1'

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--go',default='go');p.add_argument('--os',choices=['linux','windows'],default='windows' if os.name=='nt' else 'linux')
    p.add_argument('--arch',choices=['amd64','arm64'],default='amd64');p.add_argument('--output',required=True);p.add_argument('--replace',action='store_true')
    a=p.parse_args();output=Path(a.output).resolve()
    if output.exists() and not a.replace:raise SystemExit('Output exists; use a new path or explicitly pass --replace for a staged file.')
    go=str(Path(shutil.which(a.go) or a.go).resolve())
    version=subprocess.check_output([go,'version'],text=True)
    match=re.search(r'go(\d+)\.(\d+)(?:\.(\d+))?',version)
    if not match or tuple(int(v or 0) for v in match.groups())<(1,27,3):raise SystemExit('Go 1.27.3 or newer is required for patched standard library.')
    metadata=Path(__file__).resolve().parents[1]/'third_party/sing-box-security'
    data=urllib.request.urlopen(urllib.request.Request(SOURCE,headers={'User-Agent':'Harbor-source-builder'}),timeout=120).read((32<<20)+1)
    if hashlib.sha256(data).hexdigest()!=SOURCE_SHA256:raise ValueError('Pinned source digest mismatch')
    with tempfile.TemporaryDirectory(prefix='harbor-singbox-') as temporary:
        root=Path(temporary)
        with tarfile.open(fileobj=io.BytesIO(data),mode='r:gz') as archive:
            for member in archive.getmembers():
                parts=Path(member.name).parts[1:]
                if not parts or member.issym() or member.islnk():continue
                target=root.joinpath(*parts)
                if not target.resolve().is_relative_to(root.resolve()):raise ValueError('Unsafe archive path')
                if member.isdir():target.mkdir(parents=True,exist_ok=True)
                elif member.isfile():
                    target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes(archive.extractfile(member).read())
        for name in ('go.mod','go.sum'):shutil.copyfile(metadata/name,root/name)
        env={**os.environ,'GOOS':a.os,'GOARCH':a.arch,'CGO_ENABLED':'0','GOTOOLCHAIN':'local'}
        output.parent.mkdir(parents=True,exist_ok=True)
        candidate=output.with_suffix(output.suffix+'.candidate')
        subprocess.run([go,'build','-mod=readonly','-trimpath','-tags',TAGS,'-ldflags','-s -w -X github.com/sagernet/sing-box/constant.Version='+VERSION,'-o',str(candidate),'./cmd/sing-box'],cwd=root,env=env,check=True)
        os.replace(candidate,output)
        source=output.parent/('sing-box-'+VERSION+'-source.zip')
        with zipfile.ZipFile(source,'w',zipfile.ZIP_DEFLATED) as archive:
            for file in sorted(root.rglob('*')):
                if file.is_file():archive.write(file,'sing-box-'+VERSION+'/'+file.relative_to(root).as_posix())
            archive.write(__file__,'build-singbox.py')
            archive.write(metadata/'COPYING','sing-box-'+VERSION+'/COPYING')
            archive.writestr('BUILD-HARBOR.txt','Complete corresponding source: sing-box-'+VERSION+'/. Dependencies are pinned in go.mod/go.sum. With Go 1.27.3+ run from that directory:\ngo build -mod=readonly -trimpath -tags '+TAGS+' -ldflags "-s -w -X github.com/sagernet/sing-box/constant.Version='+VERSION+'" ./cmd/sing-box\nSet GOOS/GOARCH and CGO_ENABLED=0 for cross compilation. See LICENSE for GPL-3.0-or-later terms.\n')
        receipt={'upstream':SOURCE,'source_sha256':SOURCE_SHA256,'version':VERSION,'platform':a.os+'-'+a.arch,'tags':TAGS,'go':version.strip(),'binary_sha256':hashlib.sha256(output.read_bytes()).hexdigest(),'corresponding_source_sha256':hashlib.sha256(source.read_bytes()).hexdigest()}
        output.with_suffix(output.suffix+'.receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')
        print('Patched core and complete corresponding source ready:',output,source)

if __name__=='__main__':main()

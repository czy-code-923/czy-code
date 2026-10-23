"""Build fixed Linux Agent bootstrap packages using verified official engines."""
import argparse, hashlib, json, os, shutil, subprocess, sys, tempfile, zipfile
from pathlib import Path

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--go',default='go')
    parser.add_argument('--output',default='agent-releases')
    args=parser.parse_args()
    project=Path(__file__).resolve().parents[1]
    output=Path(args.output).resolve()
    if output.exists() and any(p.name!='.gitkeep' for p in output.iterdir()):
        raise SystemExit('Output is not empty. Use a new version directory; do not overwrite active packages.')
    manifest={}
    with tempfile.TemporaryDirectory(prefix='harbor-release-') as temporary:
        stage=Path(temporary)
        for arch in ('amd64','arm64'):
            target=stage/arch; (target/'scripts').mkdir(parents=True)
            shutil.copyfile(project/'scripts/fetch-engines.py',target/'scripts/fetch-engines.py')
            subprocess.run([sys.executable,str(target/'scripts/fetch-engines.py'),'--os','linux','--arch',arch],check=True)
            binary=target/'harbor-agent'
            subprocess.run([args.go,'build','-trimpath','-ldflags=-s -w','-o',str(binary),'./cmd/agent'],cwd=project,env={**os.environ,'GOOS':'linux','GOARCH':arch,'CGO_ENABLED':'0'},check=True)
            archive=stage/f'linux-{arch}.zip'
            with zipfile.ZipFile(archive,'w',zipfile.ZIP_DEFLATED) as package:
                package.write(binary,'harbor-agent')
                for name in ('sing-box','frps','frpc','xray'):package.write(target/'engines'/name,name)
            manifest[f'linux-{arch}']={'sha256':hashlib.sha256(archive.read_bytes()).hexdigest(),'size':archive.stat().st_size}
        output.mkdir(exist_ok=True,parents=True)
        for arch in ('amd64','arm64'):shutil.copyfile(stage/f'linux-{arch}.zip',output/f'linux-{arch}.zip')
        (output/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n',encoding='utf-8')
    print('Verified packages ready:',output)

if __name__=='__main__':main()

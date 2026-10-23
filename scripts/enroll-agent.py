"""Enroll one host and save its one-time token privately. Never prints credentials."""
from pathlib import Path
import argparse
import getpass
import hashlib
import json
import os
import secrets
import urllib.parse
import urllib.request

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--control',default='http://127.0.0.1:8080')
    parser.add_argument('--name',required=True)
    parser.add_argument('--provider',default='self')
    parser.add_argument('--address',default='inventory.invalid',help='Inventory only; may be private address or IPv6')
    parser.add_argument('--state')
    parser.add_argument('--live',action='store_true')
    args=parser.parse_args();u=urllib.parse.urlsplit(args.control)
    if u.username or u.password or u.path or u.query or u.fragment or not u.hostname: raise ValueError('Control must be an origin')
    if u.scheme!='https' and not (u.scheme=='http' and u.hostname in ('localhost','127.0.0.1','::1')): raise ValueError('Remote control requires verified HTTPS')
    root=Path(__file__).resolve().parents[1];target=root/'.agent.env'
    if target.exists(): raise ValueError('.agent.env already exists; never overwrite an enrolled host')
    state=Path(args.state).resolve() if args.state else root/'state'/'live'
    if '\n' in str(state) or '\r' in str(state): raise ValueError('Invalid state path')
    settings={'CONTROL_URL':args.control,'AGENT_MODE':'simulation','ENABLE_LIVE':'1' if args.live else '0','ENGINE_SECRET':secrets.token_hex(32),'LIVE_STATE_DIR':str(state),'GOMEMLIMIT':'128MiB'}
    if args.live:
        receipt=json.loads((root/'engines'/'receipt.json').read_text(encoding='utf-8'))
        for name,prefix in [('sing-box','SING_BOX'),('frps','FRPS'),('frpc','FRPC'),('xray','XRAY')]:
            file=name+('.exe' if os.name=='nt' else '');path=root/'engines'/file
            digest=hashlib.sha256(path.read_bytes()).hexdigest()
            if digest!=receipt['binaries'][file]: raise ValueError('Engine no longer matches verified receipt')
            settings[prefix+'_BINARY']=str(path);settings[prefix+'_SHA256']=digest
    token=getpass.getpass('Harbor ADMIN_TOKEN (not stored): ')
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self,*args): raise ValueError('Control redirects forbidden')
    opener=urllib.request.build_opener(NoRedirect())
    request=urllib.request.Request(args.control+'/api/v1/hosts',data=json.dumps({'name':args.name,'provider':args.provider,'address':args.address}).encode(),headers={'Authorization':'Bearer '+token,'Content-Type':'application/json'})
    with opener.open(request,timeout=15) as response: host=json.loads(response.read(65536))
    settings.update(HOST_ID=host['id'],AGENT_TOKEN=host['agent_token'])
    fd=os.open(target,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
    with os.fdopen(fd,'w',encoding='utf-8',newline='\n') as file: file.write(''.join(f'{k}={v}\n' for k,v in settings.items()))
    print('Enrolled host',host['id'],'; private credentials saved in .agent.env.')

if __name__=='__main__': main()

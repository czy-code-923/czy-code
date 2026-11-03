"""Fixed Linux UDP port-hopping helper. No shell, supplied paths or native rules."""
import argparse,json,os,re,secrets,stat,subprocess,time
from pathlib import Path

MARKER='starhub-port-hopping-v1'
PROTECTED=(5173,8080,8091)

def secure_open(path,directory=False):
    """Resolve every component without following an Agent-controlled symlink."""
    path=Path(path)
    if not path.is_absolute() or '..' in path.parts:raise ValueError('Absolute canonical path required')
    fd=os.open('/',os.O_RDONLY|os.O_DIRECTORY)
    try:
        for i,part in enumerate(path.parts[1:]):
            flags=os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK
            if i<len(path.parts)-2 or directory:flags|=os.O_DIRECTORY
            child=os.open(part,flags,dir_fd=fd);os.close(fd);fd=child
        return fd
    except BaseException:os.close(fd);raise

def environment(path):
    fd=secure_open(path)
    with os.fdopen(fd,'rb') as stream:
        st=os.fstat(stream.fileno())
        if not stat.S_ISREG(st.st_mode) or st.st_uid!=0 or st.st_mode&0o077:raise ValueError('Private root-owned environment required')
        raw=stream.read(65537)
    if len(raw)>65536:raise ValueError('Environment too large')
    values=dict(line.split('=',1) for line in raw.decode().splitlines() if '=' in line and not line.startswith('#'))
    if not re.fullmatch('[a-f0-9]{32}',values.get('HOST_ID','')):raise ValueError('Invalid host identity')
    state=Path(values.get('LIVE_STATE_DIR',''))
    if not state.is_absolute() or any(c in str(state) for c in '\n\r% \t') or '..' in state.parts:raise ValueError('Invalid live state directory')
    return values['HOST_ID'],state

def ranges(text):
    if not isinstance(text,str) or not 0<len(text)<=256:raise ValueError('Invalid hop ports')
    result=[];count=0
    for part in text.split(','):
        if not re.fullmatch('[1-9][0-9]{3,4}(-[1-9][0-9]{3,4})?',part):raise ValueError('Invalid numeric port range')
        ends=list(map(int,part.split('-')));a=ends[0];b=ends[-1]
        if not 1024<=a<=b<=65535 or any(a<=p<=b for p in PROTECTED):raise ValueError('Protected or invalid port range')
        if any(a<=y and b>=x for x,y in result):raise ValueError('Overlapping hop ranges')
        count+=b-a+1
        if count>4096:raise ValueError('Too many hop ports')
        result.append((a,b))
    if not 1<=len(result)<=16 or count<2:raise ValueError('Invalid hop range count')
    return result

def compile_rules(host,rules):
    if not re.fullmatch('[a-f0-9]{32}',host) or not isinstance(rules,list) or len(rules)>64:raise ValueError('Invalid hop rules')
    table='starhub_hop_'+host
    lines=['table inet '+table+' {',' comment "'+MARKER+'"',' chain prerouting {','  type nat hook prerouting priority dstnat; policy accept;']
    reserved=[]
    for rule in rules:
        if not isinstance(rule,dict) or set(rule)!={'id','listen','port','ports'}:raise ValueError('Unexpected rule fields')
        if not re.fullmatch('[a-f0-9]{32}',rule['id']) or rule['listen'] not in ('0.0.0.0','::'):raise ValueError('Invalid hop listener')
        p=rule['port']
        if type(p)!=int or not 1024<=p<=65535 or p in PROTECTED:raise ValueError('Invalid target port')
        rr=ranges(rule['ports'])
        current=rr+[(p,p)]
        if any(a<=y and b>=x for a,b in current for x,y in reserved):raise ValueError('Cross-node port collision')
        reserved+=current
        family='meta nfproto ipv4 ' if rule['listen']=='0.0.0.0' else ''
        for a,b in rr:
            ports=str(a) if a==b else str(a)+'-'+str(b)
            lines.append('  iifname != "lo" fib daddr type local '+family+'udp dport '+ports+' counter redirect to :'+str(p))
    return table,'\n'.join(lines+[' }','}',''])

def nft_path():
    for name in ('/usr/sbin/nft','/sbin/nft','/usr/bin/nft'):
        p=Path(name)
        if not p.exists():continue
        st=p.stat()
        if st.st_uid==0 and not st.st_mode&0o022 and stat.S_ISREG(st.st_mode):return name
    raise ValueError('nftables is required; install nftables with your OS package manager')

def nft(path,args,body=None):
    r=subprocess.run([path,*args],input=body,text=True,capture_output=True,timeout=8,env={'PATH':'/usr/sbin:/usr/bin:/sbin:/bin','LANG':'C'})
    if r.returncode or len(r.stdout)>131072:raise ValueError('Fixed nftables operation failed')
    return r.stdout

def reconcile(host,rules,path=None):
    table,body=compile_rules(host,rules);path=path or nft_path()
    tables=json.loads(nft(path,['-j','list','tables']))['nftables']
    exists=any(x.get('table',{}).get('name')==table and x['table'].get('family')=='inet' for x in tables)
    if exists:
        detail=json.loads(nft(path,['-j','list','table','inet',table]))['nftables']
        if not any(x.get('table',{}).get('name')==table and x['table'].get('comment')==MARKER for x in detail):raise ValueError('Existing table is not owned by Starhub')
    batch=('delete table inet '+table+'\n' if exists else '')+(body if rules else '')
    if batch:nft(path,['--check','-f','-'],batch);nft(path,['-f','-'],batch)

def write_status(fd,data):
    name='port-hopping.status.'+secrets.token_hex(12)+'.tmp'
    out=os.open(name,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o644,dir_fd=fd)
    os.fchmod(out,0o644)
    with os.fdopen(out,'w') as stream:json.dump(data,stream);stream.flush();os.fsync(stream.fileno())
    os.rename(name,'port-hopping.status',src_dir_fd=fd,dst_dir_fd=fd)

def tick(env):
    host,state=environment(env);fd=secure_open(state,True)
    revision=''
    try:
        inp=os.open('port-hopping.json',os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK,dir_fd=fd)
        with os.fdopen(inp,'rb') as stream:
            if not stat.S_ISREG(os.fstat(stream.fileno()).st_mode):raise ValueError('Regular request required')
            raw=stream.read(65537)
        if len(raw)>65536:raise ValueError('Oversized request')
        request=json.loads(raw)
        if set(request)!={'revision','updated_at','rules'} or not re.fullmatch('[a-f0-9]{64}',request['revision']):raise ValueError('Invalid request')
        revision=request['revision']
        if type(request['updated_at']) not in (int,float):raise ValueError('Invalid request time')
        fresh=0<=time.time()-request['updated_at']<=45
        rules=request['rules'] if fresh else []
        reconcile(host,rules)
        write_status(fd,{'revision':revision,'updated_at':int(time.time()),'ready':fresh,'error':'' if fresh else 'Agent request expired; owned redirects removed'})
    except Exception:
        # An invalid or abandoned request cannot leave obsolete redirects enabled.
        try:reconcile(host,[])
        except Exception:pass
        write_status(fd,{'revision':revision,'updated_at':int(time.time()),'ready':False,'error':'Port hopping unavailable; inspect starhub-agent-network.service journal'})
        raise
    finally:os.close(fd)

def install(env):
    nft_path() # Do not report a usable helper without its trusted OS dependency.
    _,state=environment(env)
    target=Path('/usr/local/libexec/starhub-agent-network.py');target.parent.mkdir(parents=True,exist_ok=True)
    if target.is_symlink():raise ValueError('Helper symlink refused')
    source=Path(__file__).read_bytes()
    if target.exists() and (target.stat().st_uid!=0 or target.stat().st_mode&0o022):raise ValueError('Untrusted existing helper')
    target.write_bytes(source);target.chmod(0o755)
    unit=Path('/etc/systemd/system/starhub-agent-network.service');timer=unit.with_suffix('.timer')
    unit.write_text('[Unit]\nDescription=Starhub bounded UDP port hopping\nConditionPathExists='+str(state/'port-hopping.json')+'\n[Service]\nType=oneshot\nExecStart=/usr/bin/python3 '+str(target)+' --env '+str(env)+'\nUMask=0077\nNoNewPrivileges=true\nProtectHome=true\nProtectSystem=strict\nReadWritePaths=-'+str(state)+'\nPrivateTmp=true\nCapabilityBoundingSet=CAP_NET_ADMIN CAP_DAC_OVERRIDE CAP_FOWNER\nRestrictAddressFamilies=AF_UNIX AF_NETLINK AF_INET AF_INET6\nTimeoutStartSec=30\n')
    timer.write_text('[Unit]\nDescription=Starhub port hopping lease cleanup\n[Timer]\nOnBootSec=10\nOnUnitActiveSec=10\nAccuracySec=1s\n[Install]\nWantedBy=timers.target\n')
    unit.chmod(0o644);timer.chmod(0o644)
    subprocess.run(['systemctl','daemon-reload'],check=True,timeout=20)
    subprocess.run(['systemctl','enable','--now',timer.name],check=True,timeout=20)

if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--env',type=Path,required=True);p.add_argument('--install',action='store_true');p.add_argument('--clear',action='store_true');a=p.parse_args()
    if os.geteuid()!=0:raise SystemExit('Root helper required')
    if a.clear:reconcile(environment(a.env)[0],[])
    elif a.install:install(a.env)
    else:tick(a.env)

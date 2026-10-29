"""Validated Nginx renderer and bounded byte counters. No raw directives accepted."""
import ipaddress,json,os,re,socket,tempfile,urllib.parse
from pathlib import Path
HEADER='# harbor-managed-routing-v1\n'
ID=re.compile(r'^[a-f0-9]{32}$')

def public_ip(value):
    ip=ipaddress.ip_address(value)
    return ip.is_global and not ip.is_multicast and not ip.is_unspecified

def upstream(value):
    u=urllib.parse.urlsplit(value)
    if u.scheme not in ('http','https') or not u.hostname or u.username or u.password or u.query or u.fragment or re.search(r'[;{}\\\r\n\t"$ ]',u.path):raise ValueError('Invalid upstream URL')
    port=u.port or (443 if u.scheme=='https' else 80)
    if not 1<=port<=65535:raise ValueError('Invalid port')
    try:ip=ipaddress.ip_address(u.hostname)
    except ValueError:
        if not re.fullmatch(r'[a-zA-Z0-9.-]{1,253}',u.hostname):raise ValueError('Invalid hostname')
    else:
        if ip.is_loopback:
            if u.scheme!='http' or port<1024 or port in (8080,8091,5173,15432,5432,2375):raise ValueError('Reserved local upstream')
        elif not public_ip(str(ip)):raise ValueError('Non-public upstream denied')
    return value

def pinned_upstream(value,resolver=socket.getaddrinfo):
    upstream(value);u=urllib.parse.urlsplit(value);port=u.port or (443 if u.scheme=='https' else 80)
    try:ip=ipaddress.ip_address(u.hostname)
    except ValueError:
        ips=sorted({x[4][0] for x in resolver(u.hostname,port,type=socket.SOCK_STREAM)},key=lambda v:(ipaddress.ip_address(v).version,int(ipaddress.ip_address(v))))
        if not ips or len(ips)>16 or not all(public_ip(v) for v in ips):raise ValueError('Upstream DNS must resolve only to public addresses')
        ip=ipaddress.ip_address(ips[0])
    host='['+str(ip)+']' if ip.version==6 else str(ip)
    return u.scheme+'://'+host+':'+str(port)+u.path

def options(value):
    value=value or {};result={'connect_timeout':10,'read_timeout':300,'body_limit_mib':16,'websocket':True}
    if not isinstance(value,dict) or set(value)-set(result)-{'host_header'}:raise ValueError('Unknown reverse proxy option')
    result.update(value)
    for key,maximum in [('connect_timeout',60),('read_timeout',600),('body_limit_mib',64)]:
        if type(result[key]) is not int or not 1<=result[key]<=maximum:raise ValueError('Invalid numeric proxy option')
    if type(result['websocket']) is not bool:raise ValueError('Invalid websocket flag')
    if result.get('host_header') and not re.fullmatch(r'[a-zA-Z0-9.-]{1,253}',result['host_header']):raise ValueError('Invalid Host header')
    return result

def render(domain,target=None,certificate=False,settings=None,proxy_id=None,pinned=None):
    if not re.fullmatch(r'[a-z0-9.-]{1,253}',domain) or '.' not in domain or '..' in domain:raise ValueError('Invalid domain')
    o=options(settings);challenge='location ^~ /.well-known/acme-challenge/ { root /var/www/harbor-acme; }'
    text=HEADER
    if proxy_id:
        if not ID.fullmatch(proxy_id):raise ValueError('Invalid proxy id')
        text+='log_format starhub_'+proxy_id+' escape=json \'{"up":$request_length,"down":$bytes_sent}\';\n'
    text+='server { listen 80; listen [::]:80; server_name '+domain+'; '+challenge+' location / { '+('return 301 https://$host$request_uri;' if certificate and target else 'return 503;')+' } }\n'
    if certificate and target:
        upstream(target);u=urllib.parse.urlsplit(target);base='/etc/harbor/gateway-certificates/'+domain
        endpoint=pinned or target;host=o.get('host_header') or (u.hostname if u.hostname not in ('127.0.0.1','::1') else '$host')
        text+='server { listen 443 ssl; listen [::]:443 ssl; server_name '+domain+'; ssl_certificate '+base+'/fullchain.pem; ssl_certificate_key '+base+'/privkey.pem; ssl_protocols TLSv1.2 TLSv1.3; client_max_body_size '+str(o['body_limit_mib'])+'m; '
        if proxy_id:text+='access_log /var/log/harbor/reverse-'+proxy_id+'.jsonl starhub_'+proxy_id+' buffer=16k flush=10s; '
        text+='location / { proxy_pass '+endpoint+'; proxy_http_version 1.1; proxy_set_header Host '+host+'; proxy_set_header X-Real-IP $remote_addr; proxy_set_header X-Forwarded-For $remote_addr; proxy_set_header X-Forwarded-Proto https; '
        if o['websocket']:text+='proxy_set_header Upgrade $http_upgrade; proxy_set_header Connection "upgrade"; '
        if u.scheme=='https':text+='proxy_ssl_server_name on; proxy_ssl_name '+u.hostname+'; proxy_ssl_verify on; proxy_ssl_trusted_certificate /etc/ssl/certs/ca-certificates.crt; '
        text+='proxy_connect_timeout '+str(o['connect_timeout'])+'s; proxy_read_timeout '+str(o['read_timeout'])+'s; } }\n'
    return text.encode()

def collect(path,state,baseline=(0,0),limit=4*1024*1024):
    if path.is_symlink():raise ValueError('Log symlink refused')
    if not path.exists():return state or {'up':baseline[0],'down':baseline[1],'offset':0,'inode':0}
    stat=path.stat();new=not state;state=dict(state or {'up':baseline[0],'down':baseline[1],'offset':stat.st_size if any(baseline) else 0,'inode':stat.st_ino})
    if state.get('inode')!=stat.st_ino or state.get('offset',0)>stat.st_size:state['offset']=0
    with path.open('rb') as file:
        file.seek(state.get('offset',0));data=file.read(limit);cut=data.rfind(b'\n')
        if cut>=0:
            for line in data[:cut].splitlines():
                row=json.loads(line)
                if set(row)!={'up','down'} or any(type(row[k]) is not int or not 0<=row[k]<=1000000000000 for k in row):raise ValueError('Invalid byte counter line')
                state['up']+=row['up'];state['down']+=row['down']
            state['offset']+=cut+1
    state['inode']=stat.st_ino;return state

def save_state(path,state):
    if path.is_symlink():raise ValueError('State symlink refused')
    path.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
    fd,temporary=tempfile.mkstemp(dir=path.parent)
    with os.fdopen(fd,'w') as file:json.dump(state,file);file.flush();os.fsync(file.fileno())
    os.chmod(temporary,0o600);os.replace(temporary,path)

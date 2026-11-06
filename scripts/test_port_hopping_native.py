"""Real UDP IPv4/IPv6 NAT and lease cleanup, ONLY inside an isolated Linux netns."""
import json,os,socket,subprocess,sys,tempfile,threading,time,uuid
from pathlib import Path
import types
source=Path(__file__).resolve().parents[1]/'internal/api/bootstrap/agent-network.py'
m=types.ModuleType('network');m.__file__=str(source);exec(compile(source.read_text(),str(source),'exec'),m.__dict__)

def run(*args):return subprocess.run(args,check=True,capture_output=True,text=True,timeout=15).stdout
def main():
    if sys.platform!='linux' or os.geteuid()!=0 or os.readlink('/proc/self/ns/net')==os.readlink('/proc/1/ns/net'):raise SystemExit('Run with sudo unshare --net python3 scripts/test_port_hopping_native.py')
    ns='sh-hop-'+uuid.uuid4().hex[:8];host='e'*32
    try:
        run('ip','netns','add',ns);run('ip','link','add','hop-s','type','veth','peer','name','hop-c');run('ip','link','set','hop-c','netns',ns)
        for ip in ('10.203.0.1/24','fd12:203::1/64'):run('ip','addr','add',ip,'dev','hop-s')
        run('ip','link','set','hop-s','up');run('ip','link','set','lo','up')
        for ip in ('10.203.0.2/24','fd12:203::2/64'):run('ip','-n',ns,'addr','add',ip,'dev','hop-c')
        run('ip','-n',ns,'link','set','hop-c','up');run('ip','-n',ns,'link','set','lo','up');time.sleep(2)
        sockets=[]
        for family,address in ((socket.AF_INET,'0.0.0.0'),(socket.AF_INET6,'::')):
            sock=socket.socket(family,socket.SOCK_DGRAM)
            if family==socket.AF_INET6:sock.setsockopt(socket.IPPROTO_IPV6,socket.IPV6_V6ONLY,1)
            sock.bind((address,24000));sockets.append(sock)
            def echo(s=sock):
                while True:
                    try:data,peer=s.recvfrom(2048);s.sendto(data,peer)
                    except OSError:return
            threading.Thread(target=echo,daemon=True).start()
        m.nft(m.nft_path(),['-f','-'],'table inet untouched { comment "test-sentinel"; }\n')
        rules=[{'id':'f'*32,'listen':'::','port':24000,'ports':'30000-30003,30100'}]
        m.reconcile(host,rules)
        # Reapply must recognize our table even on nft 1.0.6, whose JSON
        # listing omits the table comment that its text listing preserves.
        m.reconcile(host,rules)
        client='import socket,sys;s=socket.socket(socket.AF_INET6 if ":" in sys.argv[1] else socket.AF_INET,socket.SOCK_DGRAM);s.settimeout(2);s.sendto(b"hop-ok",(sys.argv[1],int(sys.argv[2])));assert s.recv(100)==b"hop-ok"'
        for address in ('10.203.0.1','fd12:203::1'):
            for port in ('30001','30100'):run('ip','netns','exec',ns,sys.executable,'-c',client,address,port)
        with tempfile.TemporaryDirectory() as folder:
            state=Path(folder)/'live';state.mkdir();env=Path(folder)/'agent.env';env.write_text('HOST_ID='+host+'\nLIVE_STATE_DIR='+str(state)+'\n');env.chmod(0o600)
            (state/'port-hopping.json').write_text(json.dumps({'revision':'d'*64,'updated_at':int(time.time())-60,'rules':rules}))
            m.tick(env)
            assert json.loads((state/'port-hopping.status').read_text())['ready'] is False
        table='starhub_hop_'+host
        tables=m.nft(m.nft_path(),['-j','list','tables']);assert table not in tables and 'untouched' in tables
        stopped=subprocess.run(['ip','netns','exec',ns,sys.executable,'-c',client,'10.203.0.1','30002'],capture_output=True,timeout=5);assert stopped.returncode!=0
        # Main listener remains usable after hop redirects expire.
        for address in ('10.203.0.1','fd12:203::1'):run('ip','netns','exec',ns,sys.executable,'-c',client,address,'24000')
        m.nft(m.nft_path(),['-f','-'],'table inet '+table+' { comment "foreign"; }\n')
        try:m.reconcile(host,[])
        except ValueError as error:assert 'not owned' in str(error)
        else:raise AssertionError('Foreign same-name table was removed')
        assert 'foreign' in m.nft(m.nft_path(),['list','table','inet',table])
        print('Native IPv4/IPv6 UDP hop forwarding, reapply, lease cleanup and foreign-table preservation passed')
    finally:
        subprocess.run(['ip','netns','del',ns],capture_output=True)

if __name__=='__main__':main()

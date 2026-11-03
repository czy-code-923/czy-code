"""Port-hopping validation / ownership / lease tests, no real firewall changes."""
import importlib.util,json,time,types,unittest
from pathlib import Path
from unittest.mock import patch
source=Path(__file__).resolve().parents[1]/'internal/api/bootstrap/agent-network.py'
m=types.ModuleType('hopping');m.__file__=str(source);exec(compile(source.read_text(encoding='utf-8'),str(source),'exec'),m.__dict__)

class HoppingTests(unittest.TestCase):
    def rule(self,**kw):return {'id':'b'*32,'listen':'::','port':24000,'ports':'30000-30100',**kw}
    def test_numeric_only_and_cross_node_collisions(self):
        for text in ('30000;flush ruleset','30000\n30001','1-20','5000-9000','30000-40000','30000-30002,30002-30003'):
            with self.assertRaises(ValueError):m.ranges(text)
        with self.assertRaisesRegex(ValueError,'collision'):m.compile_rules('a'*32,[self.rule(),self.rule(id='c'*32,port=30020,ports='31000-31001')])
    def test_dual_stack_local_destination_and_ownership(self):
        name,body=m.compile_rules('a'*32,[self.rule()]);self.assertEqual(name,'starhub_hop_'+'a'*32)
        self.assertIn('fib daddr type local',body);self.assertIn('iifname != "lo"',body);self.assertIn('redirect to :24000',body);self.assertNotIn('flush ruleset',body)
        self.assertIn('meta nfproto ipv4',m.compile_rules('a'*32,[self.rule(listen='0.0.0.0')])[1])
        with patch.object(m,'nft',side_effect=[json.dumps({'nftables':[{'table':{'name':name,'family':'inet'}}]}),json.dumps({'nftables':[{'table':{'name':name,'family':'inet','comment':'foreign'}}]})]) as nft:
            with self.assertRaisesRegex(ValueError,'not owned'):m.reconcile('a'*32,[],path='/usr/sbin/nft')
            self.assertEqual(nft.call_count,2)
    def test_atomic_check_before_apply_and_cleanup(self):
        calls=[]
        def nft(path,args,body=None):
            calls.append((args,body))
            if args==['-j','list','tables']:return '{"nftables":[]}'
            return ''
        with patch.object(m,'nft',side_effect=nft):m.reconcile('a'*32,[self.rule()],'/usr/sbin/nft')
        self.assertEqual(calls[-2][0],['--check','-f','-']);self.assertEqual(calls[-1][0],['-f','-']);self.assertEqual(calls[-2][1],calls[-1][1])
        with patch.object(m,'nft',side_effect=nft):m.reconcile('a'*32,[],'/usr/sbin/nft')
        self.assertEqual(calls[-1][0],['-j','list','tables'])
    def test_input_schema_is_closed(self):
        with self.assertRaises(ValueError):m.compile_rules('a'*32,[self.rule(command='rm')])
        with self.assertRaises(ValueError):m.compile_rules('a'*32,[self.rule(port=True)])
        with self.assertRaises(ValueError):m.compile_rules('a'*32,[self.rule(listen='127.0.0.1')])

if __name__=='__main__':unittest.main()

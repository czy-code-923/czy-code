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

    def test_nft_106_missing_json_comment_can_update_and_clear_owned_table(self):
        table='starhub_hop_'+'a'*32
        entry={'table':{'family':'inet','name':table,'handle':23}}
        detail={'nftables':[{'metainfo':{'version':'1.0.6'}},entry,{'chain':{'family':'inet','table':table,'name':'prerouting'}}]}
        for rules in ([],[self.rule()]):
            calls=[]
            def nft(path,args,body=None):
                calls.append((args,body))
                if args==['-j','list','tables']:return json.dumps({'nftables':[entry]})
                if args==['-j','list','table','inet',table]:return json.dumps(detail)
                if args==['-nn','list','table','inet',table]:return 'table inet '+table+' {\n\tcomment "'+m.MARKER+'"\n\tchain prerouting {\n\t}\n}\n'
                return ''
            with patch.object(m,'nft',side_effect=nft):m.reconcile('a'*32,rules,'/usr/sbin/nft')
            self.assertEqual(calls[-2][0],['--check','-f','-'])
            self.assertEqual(calls[-1][0],['-f','-'])
            self.assertEqual(calls[-2][1],calls[-1][1])
            self.assertTrue(calls[-1][1].startswith('delete table inet '+table+'\n'))
            self.assertEqual('redirect to' in calls[-1][1],bool(rules))

    def test_json_marker_needs_no_fallback_and_foreign_marker_cannot_be_overridden(self):
        table='starhub_hop_'+'a'*32
        for marker,expected in ((m.MARKER,True),('foreign',False),('',False),(None,False)):
            detail=json.dumps({'nftables':[{'table':{'family':'inet','name':table,'comment':marker}}]})
            with patch.object(m,'nft',return_value=detail) as nft:
                self.assertEqual(m.owned_table('/usr/sbin/nft',table),expected)
                self.assertEqual(nft.call_count,1)

    def test_missing_json_marker_never_accepts_nested_or_foreign_native_marker(self):
        table='starhub_hop_'+'a'*32
        detail=json.dumps({'nftables':[{'table':{'family':'inet','name':table}}]})
        for text in (
            'table inet '+table+' {\n chain prerouting { comment "'+m.MARKER+'"; }\n}\n',
            'table inet '+table+' {\n chain prerouting {\n  udp dport 24000 accept comment "'+m.MARKER+'"\n }\n}\n',
            'table inet '+table+' {\n comment "foreign"\n chain prerouting { comment "'+m.MARKER+'"; }\n}\n',
            'table inet '+table+'other {\n comment "'+m.MARKER+'"\n}\n',
            'table ip '+table+' {\n comment "'+m.MARKER+'"\n}\n',
            'table inet '+table+' {\n # comment "'+m.MARKER+'"\n}\n',
            'table inet '+table+' {\n comment "'+m.MARKER+'-foreign"\n}\n',
        ):
            with self.subTest(text=text),patch.object(m,'nft',side_effect=[detail,text]):
                self.assertFalse(m.owned_table('/usr/sbin/nft',table))

    def test_native_fallback_requires_one_exact_json_table_identity(self):
        table='starhub_hop_'+'a'*32
        for entries in ([],[{'table':{'family':'ip','name':table}}],[{'table':{'family':'inet','name':table+'x'}}],[{'table':{'family':'inet','name':table}}]*2):
            with patch.object(m,'nft',return_value=json.dumps({'nftables':entries})) as nft:
                self.assertFalse(m.owned_table('/usr/sbin/nft',table))
                self.assertEqual(nft.call_count,1)

if __name__=='__main__':unittest.main()

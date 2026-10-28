// @vitest-environment happy-dom
import {it,expect} from 'vitest'
import {mount} from '@vue/test-utils'
import Members from './PolicyMembers.vue'
import {replaceNodeSelectors,selectedEndpoints,selectionMode,toggleEndpoint} from './policySelection'
const nodes=[{id:'a',node_name:'trojan',name:'trojan-v4',kind:'trojan',family:'ipv4',address:'192.0.2.1',reference:'endpoint:a:ipv4'},{id:'a',node_name:'trojan',name:'trojan-v6',kind:'trojan',family:'ipv6',address:'2001:db8::1',reference:'endpoint:a:ipv6'}]
const mountMembers=(modelValue:string[])=>mount(Members,{props:{modelValue,nodes,references:['ALL','family:ipv4','family:ipv6','DIRECT','protocol:trojan'],label:s=>s}})
it('shows IPv4 and IPv6 separately inside one node and preserves the original proxy default',async()=>{
 const w=mountMembers(['ALL','DIRECT'])
 expect(w.findAll('.node-family-card')).toHaveLength(1)
 expect(w.findAll('.node-checks input')).toHaveLength(2)
 expect(w.findAll('.node-checks input').every(input=>(input.element as HTMLInputElement).checked)).toBe(true)
 await w.get('[aria-label="trojan IPv6 a"]').setValue(false)
 expect(w.emitted('update:modelValue')!.at(-1)).toEqual([['endpoint:a:ipv4','DIRECT']])
 await w.setProps({modelValue:['endpoint:a:ipv4','DIRECT']})
 await w.get('[aria-label="全部节点"]').setValue()
 expect(w.emitted('update:modelValue')!.at(-1)).toEqual([['ALL','DIRECT']])
 w.unmount()
})
it('changes dynamic IPv4 to manual without adding IPv6 or dropping the selected node',async()=>{
 const w=mountMembers(['family:ipv4','DIRECT'])
 expect((w.get('[aria-label="trojan IPv4 a"]').element as HTMLInputElement).checked).toBe(true)
 expect((w.get('[aria-label="trojan IPv6 a"]').element as HTMLInputElement).checked).toBe(false)
 await w.get('[aria-label="手动勾选"]').setValue()
 expect(w.emitted('update:modelValue')!.at(-1)).toEqual([['endpoint:a:ipv4','DIRECT']])
 await w.setProps({modelValue:['family:ipv4','DIRECT']})
 await w.get('[aria-label="trojan IPv6 a"]').setValue(true)
 expect(w.emitted('update:modelValue')!.at(-1)).toEqual([['endpoint:a:ipv4','endpoint:a:ipv6','DIRECT']])
 w.unmount()
})
it('splits legacy whole-node references when one family is unchecked',async()=>{
 const w=mountMembers(['node:a','DIRECT'])
 await w.get('[aria-label="trojan IPv6 a"]').setValue(false)
 expect(w.emitted('update:modelValue')!.at(-1)).toEqual([['endpoint:a:ipv4','DIRECT']])
 w.unmount()
})
it('replaces redundant protocol and family filters when selecting an explicit family',async()=>{
 const w=mountMembers(['protocol:trojan','family:ipv4','DIRECT'])
 await w.get('[aria-label="仅 IPv6"]').setValue()
 expect(w.emitted('update:modelValue')!.at(-1)).toEqual([['family:ipv6','DIRECT']])
 expect(selectionMode(['family:ipv6'])).toBe('ipv6')
 expect(replaceNodeSelectors(['DIRECT','ALL'],['family:ipv4'])).toEqual(['DIRECT','family:ipv4'])
 w.unmount()
})
it('dynamic selectors include new endpoints while manual choices stay fixed',async()=>{
 const w=mountMembers(['family:ipv4'])
 const newer=[...nodes,{id:'b',node_name:'new',name:'new-v4',kind:'vless',family:'ipv4',reference:'endpoint:b:ipv4'}]
 await w.setProps({nodes:newer})
 expect(w.text()).toContain('已匹配 2 个地址端点')
 await w.setProps({modelValue:['endpoint:a:ipv4']})
 expect(w.text()).toContain('已匹配 1 个地址端点')
 expect(selectedEndpoints(['family:ipv4'],newer)).toEqual(['endpoint:a:ipv4','endpoint:b:ipv4'])
 expect(toggleEndpoint(['endpoint:a:ipv6','endpoint:offline:ipv4','DIRECT'],nodes,'endpoint:a:ipv4',true)).toEqual(['endpoint:a:ipv6','endpoint:offline:ipv4','endpoint:a:ipv4','DIRECT'])
 w.unmount()
})

// @vitest-environment happy-dom
import {it,expect} from 'vitest'
import {mount} from '@vue/test-utils'
import Members from './PolicyMembers.vue'
it('keeps all nodes dynamic by default and manual selection uses stable IDs, not duplicate names',async()=>{
 const w=mount(Members,{props:{modelValue:['ALL','DIRECT'],nodes:[{id:'a',name:'trojan-v4',kind:'trojan',enabled:true,reference:'endpoint:a:ipv4'},{id:'a',name:'trojan-v6',kind:'trojan',enabled:true,reference:'endpoint:a:ipv6'},{id:'frp',name:'frp',kind:'frpc'}],references:['ALL','DIRECT'],label:s=>s}})
 expect(w.findAll('input[type=checkbox]')).toHaveLength(1)
 await w.findAll('input[type=radio]')[1]!.setValue()
 expect(w.emitted('update:modelValue')!.at(-1)).toEqual([['DIRECT']])
 await w.setProps({modelValue:['DIRECT']})
 const choices=w.findAll('.node-checks')[0]!.findAll('input')
 expect(choices).toHaveLength(2)
 await choices[1]!.setValue(true)
 expect(w.emitted('update:modelValue')!.at(-1)).toEqual([['DIRECT','endpoint:a:ipv6']])
 await w.setProps({modelValue:['DIRECT','endpoint:a:ipv6']})
 await w.findAll('input[type=radio]')[0]!.setValue()
 expect(w.emitted('update:modelValue')!.at(-1)).toEqual([['DIRECT','ALL']])
 w.unmount()
})
it('splits a legacy dual-stack node and removes dynamic family filters when one endpoint is selected',async()=>{
 const nodes=[{id:'a',name:'trojan-v4',kind:'trojan',reference:'endpoint:a:ipv4'},{id:'a',name:'trojan-v6',kind:'trojan',reference:'endpoint:a:ipv6'}]
 const w=mount(Members,{props:{modelValue:['node:a'],nodes,references:['family:ipv4'],label:s=>s}})
 await w.findAll('.node-checks')[0]!.findAll('input')[1]!.setValue(false)
 expect(w.emitted('update:modelValue')!.at(-1)).toEqual([['endpoint:a:ipv4']])
 await w.setProps({modelValue:['family:ipv4']});await w.findAll('.node-checks')[0]!.findAll('input')[1]!.setValue(true)
 expect(w.emitted('update:modelValue')!.at(-1)).toEqual([['endpoint:a:ipv6']]);w.unmount()
})

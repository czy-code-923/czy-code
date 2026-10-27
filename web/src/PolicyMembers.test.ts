// @vitest-environment happy-dom
import {it,expect} from 'vitest'
import {mount} from '@vue/test-utils'
import Members from './PolicyMembers.vue'
it('keeps all nodes dynamic by default and manual selection uses stable IDs, not duplicate names',async()=>{
 const w=mount(Members,{props:{modelValue:['ALL','DIRECT'],nodes:[{id:'a',name:'same',kind:'trojan',enabled:true},{id:'b',name:'same',kind:'trojan',enabled:false},{id:'frp',name:'frp',kind:'frpc'}],references:['ALL','DIRECT'],label:s=>s}})
 expect(w.findAll('input[type=checkbox]')).toHaveLength(1)
 await w.findAll('input[type=radio]')[1]!.setValue()
 expect(w.emitted('update:modelValue')!.at(-1)).toEqual([['DIRECT']])
 await w.setProps({modelValue:['DIRECT']})
 const choices=w.findAll('.node-checks')[0]!.findAll('input')
 expect(choices).toHaveLength(2)
 await choices[1]!.setValue(true)
 expect(w.emitted('update:modelValue')!.at(-1)).toEqual([['DIRECT','node:b']])
 await w.setProps({modelValue:['DIRECT','node:b']})
 await w.findAll('input[type=radio]')[0]!.setValue()
 expect(w.emitted('update:modelValue')!.at(-1)).toEqual([['DIRECT','ALL']])
 w.unmount()
})

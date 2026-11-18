// @vitest-environment happy-dom
import {it,expect} from 'vitest'
import {mount} from '@vue/test-utils'
import NetworkFields from './NetworkFields.vue'
it('shows stale selected IPs and replaces the same family without retaining hidden values',async()=>{
 const w=mount(NetworkFields,{props:{host:{address:'14.137.229.62',reported_addresses:['14.137.229.62','2001:db8::2']},endpoints:['14.137.229.42','2001:db8::1'],egress:[]}})
 expect(w.text()).toContain('旧发布 IP · 14.137.229.42')
 expect(w.text()).toContain('旧发布 IP · 2001:db8::1')
 await w.findAll('input[type=checkbox]')[0].setValue(true)
 expect(w.emitted('endpoints')?.at(-1)).toEqual([['2001:db8::1','14.137.229.62']])
 await w.findAll('input[type=checkbox]')[2].setValue(false)
 expect(w.emitted('endpoints')?.at(-1)).toEqual([['2001:db8::1']])
 w.unmount()
})
it('offers all reported addresses so multi-IP hosts can select a specific IP',()=>{
 const w=mount(NetworkFields,{props:{host:{address:'192.0.2.1',reported_addresses:['192.0.2.1','192.0.2.2']},endpoints:[],egress:[]}})
 expect(w.text()).toContain('IPv4 · 192.0.2.2');w.unmount()
})

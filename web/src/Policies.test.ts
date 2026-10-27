// @vitest-environment happy-dom
import {it,expect,vi} from 'vitest'
import {mount,flushPromises} from '@vue/test-utils'
import Policies from './Policies.vue'
import NetworkFields from './NetworkFields.vue'
it('keeps the user/client scope and version when editing a routing policy',async()=>{
 const source='rules:\n  - MATCH,DIRECT\n',api=vi.fn(async(p:string,m='GET',body?:any)=>{
  if(p==='client-policies/parse')return {policy:{'proxy-groups':[],rules:['MATCH,DIRECT']},source:body?.source??source}
  if(m!=='GET')return {}
  if(p==='client-policies')return [{id:'p',name:'Netflix',user_id:'u',client_id:'mihomo',source,version:3,native_options:{}}]
  if(p==='proxy-users')return [{id:'u',name:'Alice'}]
  if(p==='client-catalog')return [{id:'mihomo',name:'Mihomo',full_config:true}]
  return []
 })
 const w=mount(Policies,{props:{api}});await flushPromises();await w.findAll('button').find(b=>b.text()==='编辑')!.trigger('click');await flushPromises();await w.get('textarea[maxlength="32768"]').setValue('rules:\n  - DOMAIN-SUFFIX,netflix.com,DIRECT\n  - MATCH,REJECT\n');await w.get('form').trigger('submit');await flushPromises();expect(api).toHaveBeenCalledWith('client-policies/p','PUT',expect.objectContaining({version:3,user_id:'u',client_id:'mihomo',source:expect.stringContaining('netflix.com')}));expect(w.find('[role=dialog]').exists()).toBe(false);w.unmount()
})
it('disables IPv6 for a host with no IPv6 and emits addresses separately from egress families',async()=>{
 const w=mount(NetworkFields,{props:{host:{address:'192.0.2.8',reported_addresses:['192.0.2.8']},endpoints:[],egress:[]}})
 const inputs=w.findAll('input');expect(inputs[1].attributes('disabled')).toBeDefined();expect(w.text()).toContain('无 IPv6');await inputs[0].setValue(true);expect(w.emitted('endpoints')?.[0]).toEqual([['192.0.2.8']]);await inputs[2].setValue(true);expect(w.emitted('egress')?.[0]).toEqual([['ipv4']]);w.unmount()
})

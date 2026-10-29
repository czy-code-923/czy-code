// @vitest-environment happy-dom
import {afterEach,it,expect,vi} from 'vitest'
import {mount,flushPromises,type VueWrapper} from '@vue/test-utils'
import Settings from './Settings.vue'
let w:VueWrapper
afterEach(()=>w?.unmount())
it('saves domain and HTTPS without claiming the gateway already changed',async()=>{const api=vi.fn(async(p,m='GET')=>p==='site'?{domain:'vps.example.com',https:true,gateway_state:'pending'}:[]);w=mount(Settings,{props:{api,hosts:[]}});await flushPromises();await w.get('input[placeholder="vps.example.com"]').setValue('new.example.com');await w.get('form').trigger('submit');await flushPromises();expect(api).toHaveBeenCalledWith('site','PUT',expect.objectContaining({domain:'new.example.com',https:true,api_origins:[]}));expect(w.text()).toContain('网关同步器会检查 DNS')})
it('refreshes gateway status without discarding unsaved fields',async()=>{
 let active=false
 const api=vi.fn(async(p:string)=>p==='site'?{domain:'vps.example.com',https:true,share_domain:'sub.example.com',share_https:true,gateway_state:'active',share_gateway_domain:active?'sub.example.com':'',share_gateway_state:active?'active':'pending'}:[])
 w=mount(Settings,{props:{api,hosts:[]}});await flushPromises()
 await w.get('input[placeholder="vps.example.com"]').setValue('draft.example.com')
 expect(w.text()).toContain('等待网关同步')
 active=true;await w.findAll('button').find(b=>b.text()==='刷新网关状态')!.trigger('click');await flushPromises()
 expect((w.get('input[placeholder="vps.example.com"]').element as HTMLInputElement).value).toBe('draft.example.com')
 expect(w.text()).toContain('sub.example.com · active')
})

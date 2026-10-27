// @vitest-environment happy-dom
import {afterEach,it,expect,vi} from 'vitest'
import {mount,flushPromises,type VueWrapper} from '@vue/test-utils'
import Settings from './Settings.vue'
let w:VueWrapper
afterEach(()=>w?.unmount())
it('saves domain and HTTPS without claiming the gateway already changed',async()=>{const api=vi.fn(async(p,m='GET')=>p==='site'?{domain:'vps.example.com',https:true,gateway_state:'pending'}:[]);w=mount(Settings,{props:{api,hosts:[]}});await flushPromises();await w.get('input[placeholder="vps.example.com"]').setValue('new.example.com');await w.get('form').trigger('submit');await flushPromises();expect(api).toHaveBeenCalledWith('site','PUT',expect.objectContaining({domain:'new.example.com',https:true,api_origins:[]}));expect(w.text()).toContain('网关同步器会检查 DNS')})

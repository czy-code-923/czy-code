// @vitest-environment happy-dom
import {it,expect,vi} from 'vitest'
import {mount,flushPromises} from '@vue/test-utils'
import GatewayManager from './GatewayManager.vue'
it('edits issued ACME certificate metadata without exposing its private key',async()=>{
 const api=vi.fn(async(p:string,m='GET')=>m==='GET'&&p==='certificates'?[{id:'cert',domain:'app.example.com',provider:'cloudflare',email:'ops@example.com',version:4,state:'ready'}]:[])
 const w=mount(GatewayManager,{props:{api,view:'certificates'}});await flushPromises()
 await w.findAll('button').find(b=>b.text()==='编辑证书')!.trigger('click');await w.get('form').trigger('submit');await flushPromises()
 expect(api).toHaveBeenCalledWith('certificates/cert','PUT',expect.objectContaining({provider:'cloudflare',domain:'app.example.com',version:4,dns_token:'',private_key:''}));w.unmount()
})
it('edits typed reverse config with optimistic version and confirms deletion',async()=>{
 const api=vi.fn(async(p:string,m='GET')=>m==='GET'&&p==='reverse-proxies'?[{id:'p',domain:'app.example.com',upstream:'https://www.example.com',certificate_id:'c',enabled:true,version:3,engine:'nginx',options:{read_timeout:40},upload_bytes:1024,download_bytes:2048}]:[])
 const w=mount(GatewayManager,{props:{api,view:'reverse'}});await flushPromises()
 expect(w.text()).toContain('nginx');await w.findAll('button').find(b=>b.text()==='编辑反代')!.trigger('click')
 await w.get('[aria-label="反代配置参数"]').setValue('{"read_timeout":50}');await w.get('form').trigger('submit');await flushPromises()
 expect(api).toHaveBeenCalledWith('reverse-proxies/p','PUT',expect.objectContaining({version:3,options:{read_timeout:50}}))
 api.mockClear();await w.findAll('button').find(b=>b.text()==='删除反代')!.trigger('click');expect(api).not.toHaveBeenCalled()
 await w.findAll('button').find(b=>b.text()==='确认删除')!.trigger('click');await flushPromises();expect(api).toHaveBeenCalledWith('reverse-proxies/p','DELETE');w.unmount()
})

it('displays the detected Nginx version and shared owner',async()=>{
 const api=vi.fn(async(p:string)=>p==='gateway/nginx'?{version:'1.30.4',source:'baota',checked_at:'2026-10-29T00:00:00Z'}:[])
 const w=mount(GatewayManager,{props:{api,view:'reverse'}});await flushPromises()
 expect(w.text()).toContain('1.30.4');expect(w.text()).toContain('宝塔管理')
 w.unmount()
})

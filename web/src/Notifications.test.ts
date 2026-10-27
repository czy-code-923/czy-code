// @vitest-environment happy-dom
import {reactive} from 'vue'
import {it,expect,vi} from 'vitest'
import {mount,flushPromises} from '@vue/test-utils'
import Notifications from './Notifications.vue'
import ProtocolLayers from './ProtocolLayers.vue'
it('keeps bot tokens write-only and distinguishes preview from sending',async()=>{
 const api=vi.fn(async(p:string,m='GET',b?:any)=>p==='notifications/telegram'?{config:{version:2,token_configured:true,chat_id:'12345',enabled:true,commands:['status'],events:[],allowed_user_ids:[99],report_sections:['status'],report_minutes:60,quota_percent:80}}:p.endsWith('/test')?{message:'星枢 status',preview:b.preview}:[])
 const w=mount(Notifications,{props:{api}});await flushPromises();expect((w.get('input[type=password]').element as HTMLInputElement).value).toBe('');await w.findAll('button').find(b=>b.text()==='预览通知')!.trigger('click');await flushPromises();expect(api).toHaveBeenCalledWith('notifications/telegram/test','POST',{preview:true});await w.get('form').trigger('submit');await flushPromises();expect(api).toHaveBeenCalledWith('notifications/telegram','PUT',expect.objectContaining({version:2,bot_token:'',allowed_user_ids:[99]}));w.unmount()
})
it('offers transport and security only under the chosen core adapter',async()=>{
 const form=reactive({engine:'sing-box',kind:'vless',transport:'xhttp',tls_mode:'reality',path:''}),template={layers:{'sing-box':[{transport:'tcp',security_modes:['tls','reality']},{transport:'ws',security_modes:['tls']}],xray:[{transport:'tcp',security_modes:['tls','reality']},{transport:'xhttp',security_modes:['tls','reality']}]}}
 const w=mount(ProtocolLayers,{props:{form,template}});await flushPromises();expect(form.transport).toBe('tcp');expect(w.findAll('select')[0].text()).not.toContain('XHTTP');await w.findAll('select')[0].setValue('ws');await flushPromises();expect(form.tls_mode).toBe('tls');expect(form.path).toBe('/proxy');w.unmount()
})

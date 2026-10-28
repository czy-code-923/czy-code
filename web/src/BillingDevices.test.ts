// @vitest-environment happy-dom
import {it,expect,vi,afterEach} from 'vitest'
import {mount,flushPromises,type VueWrapper} from '@vue/test-utils'
import HostTraffic from './HostTraffic.vue'
import Devices from './DeviceManager.vue'
import Logs from './AccessLog.vue'
let wrappers:VueWrapper[]=[]
afterEach(()=>{for(const w of wrappers)w.unmount();wrappers=[];vi.useRealTimers();vi.restoreAllMocks()})
it('preserves quota bytes when switching GB/GiB and sends the host period configuration',async()=>{
 const stats={version:1,quota_bytes:1073741824,used_bytes:200,remaining_bytes:1073741624,mode:'both',reset_day:1,auto_reset:true,interfaces:[],fresh:true,interfaces_available:[]}
 const api=vi.fn(async(_p:string,m='GET')=>m==='GET'?stats:{saved:true});const w=mount(HostTraffic,{props:{host:{id:'h',name:'VPS'},api}});wrappers.push(w);await flushPromises()
 await w.get('[aria-label="主机流量额度"]').setValue(3);await w.find('.host-billing-fields select').setValue('GB');await w.get('[aria-label="主机计费方式"]').setValue('max');await w.find('form').trigger('submit');await flushPromises()
 expect(api).toHaveBeenCalledWith('hosts/h/traffic','PUT',expect.objectContaining({quota_bytes:3221225472,mode:'max',version:1,reset_day:1}));expect(w.text()).toContain('套餐实时剩余')
})
it('shows automatically identified clients without manual registration and preserves editable permissions',async()=>{
 const id='a'.repeat(32),rows:Record<string,any>[]=[{id:'device',name:'Phone',client_id:'sing-box',version:1,enabled:true,all_nodes:true,service_ids:[],registration_source:'subscription_hwid',hwid_registered:true,device_os:'Android',os_version:'15',device_model:'Pixel',user_agent:'Hiddify/fixture'}]
 const api=vi.fn(async(p:string,m='GET',b?:any)=>{
 if(p==='client-catalog')return [{id:'mihomo',name:'Mihomo'},{id:'sing-box',name:'sing-box'}]
 if(p.startsWith('client-policies/nodes'))return [{id,kind:'vmess',name:'entry-v4'},{id,kind:'vmess',name:'entry-v6'}]
 if(m==='PUT'){rows[0]={...rows[0],...b,version:rows[0]!.version+1};return {}}
 return rows
 });const w=mount(Devices,{props:{user:{id:'user',name:'Alice',effective_max_devices:1},api}});wrappers.push(w);await flushPromises()
 expect(w.text()).toContain('Android · 15 · Pixel');expect(w.text()).toContain('HWID 自报身份');expect(w.text()).toContain('Clash Verge / FlClash');expect(w.findAll('button').some(b=>b.text().includes('登记设备'))).toBe(false);expect(w.findAll('button').some(b=>b.text()==='旧版订阅')).toBe(false)
 await w.findAll('button').find(b=>b.text()==='编辑权限')!.trigger('click');await w.get('.device-editor input[type="checkbox"]').setValue(false);await w.get('.device-editor .choice-list input').setValue(true);await w.get('.device-editor').trigger('submit');await flushPromises()
 expect(api).toHaveBeenCalledWith('proxy-users/user/devices/device','PUT',expect.objectContaining({all_nodes:false,service_ids:[id],version:1}));expect(api.mock.calls.some(c=>c[1]==='POST')).toBe(false)
 await w.findAll('button').find(b=>b.text()==='停用')!.trigger('click');await flushPromises();expect(api).toHaveBeenCalledWith('proxy-users/user/devices/device','PUT',expect.objectContaining({enabled:false,version:2}));expect(w.text()).toContain('不能精确代表物理设备')
})
it('browses all retained pages and requests traffic ordering without inventing unknown byte counts',async()=>{
 const api=vi.fn(async(p:string)=>{const q=new URLSearchParams(p.split('?')[1]);return {items:[{host_id:'h',event_id:q.get('page'),destination:'example.test',destination_port:443,source_ip:'2001:db8::1',observed_at:'2026-10-28T00:00:00Z',total_bytes:null,uplink:null,downlink:null}],total:205,pages:5}})
 const w=mount(Logs,{props:{api,expanded:true,userId:'u'}});wrappers.push(w);await flushPromises();expect(w.text()).toContain('205 条');expect(w.text()).toContain('—')
 await w.findAll('button').find(b=>b.text()==='下一页')!.trigger('click');await flushPromises();expect(api.mock.calls.at(-1)![0]).toContain('page=2');await w.get('[aria-label="访问记录排序"]').setValue('traffic');await flushPromises();expect(api.mock.calls.at(-1)![0]).toContain('sort=traffic');expect(api.mock.calls.at(-1)![0]).toContain('page=1')
})
it('does not poll collapsed or hidden connection metadata and stops after unmount',async()=>{
 vi.useFakeTimers();const api=vi.fn(async()=>({items:[],total:0,pages:1}));const w=mount(Logs,{props:{api,expanded:false}});wrappers.push(w);await flushPromises();await vi.advanceTimersByTimeAsync(60000);expect(api).not.toHaveBeenCalled();await w.setProps({expanded:true});await flushPromises();api.mockClear();vi.spyOn(document,'visibilityState','get').mockReturnValue('hidden');await vi.advanceTimersByTimeAsync(60000);expect(api).not.toHaveBeenCalled();w.unmount();wrappers=[];await vi.advanceTimersByTimeAsync(60000);expect(api).not.toHaveBeenCalled()
})

it('shows ordinary Clash observations and edits them without unsupported device permissions',async()=>{
 const api=vi.fn(async(p:string,m='GET',_b?:any)=>m==='PUT'?{}:p.endsWith('/devices')?[{id:'o',name:'FlClash',client_id:'mihomo',enabled:true,version:1,registration_source:'observed_client',user_agent:'FlClash/1.0',source_ip:'192.0.2.1',last_subscription_at:'2026-10-28T00:00:00Z'}]:[])
 const w=mount(Devices,{props:{user:{id:'u',name:'Alice'},api}});wrappers.push(w);await flushPromises()
 expect(w.text()).toContain('FlClash/1.0');expect(w.text()).toContain('192.0.2.1');expect(w.text()).toContain('归入用户总量')
 await w.findAll('button').find(b=>b.text()==='编辑权限')!.trigger('click');expect(w.find('.device-editor input[type=checkbox]').exists()).toBe(false)
 await w.get('form').trigger('submit');await flushPromises()
 const body=api.mock.calls.find(c=>c[1]==='PUT')!;expect(body).toHaveLength(3);expect(body[2]).not.toHaveProperty('all_nodes')
})

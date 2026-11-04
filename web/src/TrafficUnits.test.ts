// @vitest-environment happy-dom
import {afterEach,it,expect,vi} from 'vitest'
import {ref} from 'vue'
import {mount,flushPromises,type VueWrapper} from '@vue/test-utils'
import {trafficBytes,quotaDraft,quotaToBytes,changeQuotaUnit,trafficUnitKey,type TrafficUnit} from './trafficUnits'
import Traffic from './Traffic.vue'
import Access from './Access.vue'
import TrafficDisplay from './TrafficDisplay.vue'
let w:VueWrapper|undefined
afterEach(()=>{w?.unmount();w=undefined;vi.restoreAllMocks()})
it('keeps decimal, binary and automatic scaling explicit and shows small transfers',()=>{
 expect(trafficBytes(500e9)).toBe('500.00 GB');expect(trafficBytes(1<<30,'GiB')).toBe('1.00 GiB');expect(trafficBytes(1<<20)).toBe('0.001048576 GB');expect(trafficBytes(2048,'auto')).toBe('2.05 KB');expect(trafficBytes(2e12,'auto')).toBe('2.00 TB')
})
it('preserves every byte when editing or switching an existing large quota',()=>{
 const original=8_999_999_999_999_999,d=quotaDraft(original,'GB');expect(quotaToBytes(d)).toBe(original);changeQuotaUnit(d,'GiB');expect(quotaToBytes(d)).toBe(original);changeQuotaUnit(d,'GB');expect(quotaToBytes(d)).toBe(original)
 const edited=quotaDraft(1<<30,'GB');edited.quota_amount=100;expect(quotaToBytes(edited)).toBe(100e9);changeQuotaUnit(edited,'GiB');expect(quotaToBytes(edited)).toBe(100e9)
})
it('updates global totals reactively while keeping host-specific units',async()=>{
 const unit=ref<TrafficUnit>('GB'),api=vi.fn(async(p:string)=>p==='dashboard'?{used_bytes:50e9,allocated_bytes:500e9,remaining_bytes:450e9,host_traffic:[{host_id:'h',name:'Binary host',used_bytes:1073741824,quota_bytes:2*1073741824,remaining_bytes:1073741824,display_unit:'GiB'}]}:[])
 w=mount(Traffic,{props:{api},global:{provide:{[trafficUnitKey as symbol]:unit}}});await flushPromises();expect(w.text()).toContain('50.00 GB');expect(w.text()).toContain('1.00 GiB / 2.00 GiB');unit.value='auto';await flushPromises();expect(w.text()).toContain('自动换算');expect(w.text()).toContain('1.00 GiB / 2.00 GiB')
})
it('retains a user quota when only the name or input unit changes',async()=>{
 const user={id:'u',name:'Alice',enabled:true,expires_at:'2027-01-01T00:00:00Z',quota_bytes:1073741824,used_bytes:123,group_ids:[]}
 const api=vi.fn(async(p:string,m='GET')=>m==='GET'?(p==='proxy-users'?[user]:[]):{})
 w=mount(Access,{props:{api}});await flushPromises();await w.findAll('button').find(b=>b.text()==='编辑权限')!.trigger('click');expect((w.get('[aria-label="用户流量额度"]').element as HTMLInputElement).value).toBe('1.073741824');expect((w.get('[aria-label="用户流量额度"]').element as HTMLInputElement).checkValidity()).toBe(true)
 await w.get('[aria-label="用户配额单位"]').setValue('GiB');expect((w.get('[aria-label="用户流量额度"]').element as HTMLInputElement).value).toBe('1');await w.get('form').trigger('submit');await flushPromises();expect(api).toHaveBeenCalledWith('proxy-users/u','PUT',expect.objectContaining({quota_bytes:1073741824}));expect((api.mock.calls.find(c=>c[1]==='PUT') as unknown as any[])[2]).not.toHaveProperty('used_bytes')
})
it('saves automatic display independently of gateway and quota settings',async()=>{
 const api=vi.fn(async(_p:string,m='GET',b?:any)=>m==='GET'?{traffic_unit:'GB'}:b)
 w=mount(TrafficDisplay,{props:{api}});await flushPromises();await w.get('[aria-label="全局流量显示单位"]').setValue('auto');await w.get('form').trigger('submit');await flushPromises();expect(api).toHaveBeenCalledWith('display-settings','PUT',{traffic_unit:'auto'});expect(w.emitted('saved')).toHaveLength(1);expect(api.mock.calls.some(c=>c[0]==='site')).toBe(false)
})

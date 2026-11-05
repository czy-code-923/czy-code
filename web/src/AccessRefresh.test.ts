// @vitest-environment happy-dom
import {it,expect,vi} from 'vitest'
import {mount,flushPromises} from '@vue/test-utils'
import Access from './Access.vue'

it('refreshes visible user usage without reopening the page and stops on unmount',async()=>{
 vi.useFakeTimers()
 let used=1024
 const api=vi.fn(async(p:string)=>p==='proxy-users'?[{id:'u',name:'Alice',expires_at:'2030-01-01',active:true,used_bytes:used,quota_bytes:1048576,metered_nodes:1,group_ids:[]}]:[])
 const w=mount(Access,{props:{api}});await flushPromises()
 expect(w.text()).toContain('0.000001024 GB')
 used=2048;await vi.advanceTimersByTimeAsync(30000);await flushPromises()
 expect(w.text()).toContain('0.000002048 GB')
 w.unmount();const calls=api.mock.calls.length
 await vi.advanceTimersByTimeAsync(30000)
 expect(api.mock.calls.length).toBe(calls)
 vi.useRealTimers()
})

it('shows group nodes and members in a wrapping card instead of a cramped table',async()=>{
 const api=vi.fn(async(p:string)=>p==='proxy-users'?[{id:'u',name:'Alice',expires_at:'2030-01-01',group_ids:['g']}]:p==='user-groups'?[{id:'g',name:'AnyTLS Commercial',user_ids:['u'],service_ids:['n']}]:[{id:'n',name:'AnyTLS-v6',kind:'anytls'}]);const w=mount(Access,{props:{api}});await flushPromises();await w.findAll('.subnav button')[1]!.trigger('click');expect(w.find('.groups-table').exists()).toBe(false);expect(w.find('.access-group-card').text()).toContain('ANYTLS');expect(w.find('.access-group-card').text()).toContain('Alice');w.unmount()
})

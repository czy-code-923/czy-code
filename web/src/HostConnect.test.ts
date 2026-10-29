// @vitest-environment happy-dom
import {afterEach,expect,it,vi} from 'vitest'
import {mount,flushPromises} from '@vue/test-utils'
import HostConnect from './HostConnect.vue'
let wrapper:ReturnType<typeof mount>|undefined
afterEach(()=>{wrapper?.unmount();vi.useRealTimers();vi.restoreAllMocks()})
it('generates a one-time command for a new host and stops polling after unmount',async()=>{
 vi.useFakeTimers();const api=vi.fn(async(_path:string,method='GET')=>method==='POST'?{command:'safe-test-command',expires_at:new Date(Date.now()+900000).toISOString()}:{state:'waiting'})
 wrapper=mount(HostConnect,{props:{host:{id:'a',name:'NAT host'},api}});await flushPromises()
 expect(wrapper.get('textarea').element.value).toBe('safe-test-command');expect(wrapper.text()).toContain('不要求目标机有公网 IPv4')
 wrapper.unmount();wrapper=undefined;api.mockClear();await vi.advanceTimersByTimeAsync(15000);expect(api).not.toHaveBeenCalled()
})
it('never automatically replaces an already connected host credential',async()=>{
 const api=vi.fn(async()=>({state:'connected'}));wrapper=mount(HostConnect,{props:{host:{id:'b',name:'Connected'},api}});await flushPromises();expect(api).toHaveBeenCalledTimes(1);expect(wrapper.find('textarea').exists()).toBe(false);expect(wrapper.text()).toContain('主机已连接')
})
it('checks an existing offline host without generating a replacement credential',async()=>{
 const api=vi.fn(async(p:string)=>p==='hosts'?[{id:'b',online:false}]:{state:'waiting'})
 wrapper=mount(HostConnect,{props:{host:{id:'b',name:'Existing',agent_version:'0.23.0'},api}});await flushPromises()
 expect(api).toHaveBeenCalledWith('hosts');expect(api).toHaveBeenCalledTimes(1);expect(wrapper.text()).toContain('主机暂时离线');expect(wrapper.text()).toContain('systemctl restart harbor-agent.service')
 await wrapper.findAll('button').find(b=>b.text()==='检查连接')!.trigger('click');await flushPromises();expect(api).toHaveBeenCalledTimes(2)
})


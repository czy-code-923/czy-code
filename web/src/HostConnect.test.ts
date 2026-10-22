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

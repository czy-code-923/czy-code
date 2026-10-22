// @vitest-environment happy-dom
import {afterEach,beforeEach,describe,expect,it,vi,type Mock} from 'vitest'
import {mount,flushPromises,type VueWrapper} from '@vue/test-utils'
import Publish from './Publish.vue'
let wrapper:VueWrapper
let api:Mock<(path:string,method?:string,body?:any)=>Promise<any>>
const host={id:'a'.repeat(32),name:'Device'}
const service={id:'b'.repeat(32),name:'Encrypted entry',host_id:host.id,kind:'shadowsocks',address:'2001:db8::1',port:2080,enabled:false,version:1,reported_version:0,state:'pending'}
beforeEach(()=>{vi.useFakeTimers();api=vi.fn(async(path:string,method='GET')=>path==='site'?{domain:'vps.example.com',https:true}:path==='live'?[service]:method==='POST'?{url:'https://vps.example.com/s/secret-test-token'}:path==='subscriptions'?[]:{});wrapper=mount(Publish,{props:{api,hosts:[host]},attachTo:document.body})})
afterEach(()=>{wrapper.unmount();vi.useRealTimers();vi.restoreAllMocks()})
describe('domains and actual service controls',()=>{
 it('saves the domain without pretending a certificate was applied',async()=>{await flushPromises();await wrapper.get('input[placeholder="vps.example.com"]').setValue('new.example.com');await wrapper.findAll('form')[0].trigger('submit');await flushPromises();expect(api).toHaveBeenCalledWith('site','PUT',{domain:'new.example.com',https:true});expect(wrapper.text()).toContain('否则需运行部署文档');expect(wrapper.text()).toContain('网关待同步')})
 it('asks before starting actual services',async()=>{await flushPromises();api.mockClear();await wrapper.findAll('button').find(b=>b.text()==='启用')!.trigger('click');expect(api).not.toHaveBeenCalled();expect(wrapper.find('[role=dialog]').text()).toContain('启动真实服务');await wrapper.findAll('button').find(b=>b.text()==='确认执行')!.trigger('click');await flushPromises();expect(api).toHaveBeenCalledWith('live/'+service.id+'/toggle','POST',{enabled:true})})
 it('only reveals a share link after successful creation, and can hide it',async()=>{await flushPromises();const forms=wrapper.findAll('form');await forms[2].find('input').setValue('Personal');await wrapper.get('input[type=checkbox]').setValue(true);await forms[2].trigger('submit');await flushPromises();expect(wrapper.find('input[aria-label="订阅分享链接"]').attributes('value')??(wrapper.get('input[aria-label="订阅分享链接"]').element as HTMLInputElement).value).toContain('secret-test-token');await wrapper.findAll('button').find(b=>b.text()==='隐藏链接')!.trigger('click');expect(wrapper.find('input[aria-label="订阅分享链接"]').exists()).toBe(false)})
 it('does not poll in hidden browser tabs',async()=>{await flushPromises();api.mockClear();vi.spyOn(document,'visibilityState','get').mockReturnValue('hidden');await vi.advanceTimersByTimeAsync(45000);expect(api).not.toHaveBeenCalled()})
})

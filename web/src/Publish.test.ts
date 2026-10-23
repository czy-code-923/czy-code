// @vitest-environment happy-dom
import {afterEach,beforeEach,describe,expect,it,vi,type Mock} from 'vitest'
import {mount,flushPromises,type VueWrapper} from '@vue/test-utils'
import Publish from './Publish.vue'
let wrapper:VueWrapper
let api:Mock<(path:string,method?:string,body?:any)=>Promise<any>>
const host={id:'a'.repeat(32),name:'Device',agent_version:'0.7.0'}
const service={id:'b'.repeat(32),name:'Encrypted entry',host_id:host.id,node_id:'node',engine:'sing-box',kind:'vmess',address:'2001:db8::1',port:2080,enabled:false,version:1,reported_version:0,state:'pending'}
const button=(text:string)=>wrapper.findAll('button').find(b=>b.text()===text)!
beforeEach(()=>{vi.useFakeTimers();api=vi.fn(async(path:string,method='GET')=>{
 if(method==='GET'){
 if(path==='protocol-templates')return [{kind:'shadowsocks',name:'Shadowsocks',engines:['sing-box'],certificate:false},{kind:'vless',name:'VLESS',engines:['sing-box','xray'],certificate:true},{kind:'vmess',name:'VMess',engines:['sing-box','xray'],certificate:true},{kind:'hysteria2',name:'Hysteria2',engines:['sing-box'],certificate:true}]
 if(path==='site')return {domain:'vps.example.com',https:true}
 if(path==='live')return [service]
 if(path==='nodes')return [{id:'node',name:'Logical',engine:'sing-box'}]
 if(path==='live/'+service.id)return {...service,certificate_id:'',next_id:'',spec:{...service,listen:'::1',transport:'ws',path:'/unchanged',tls_mode:'none',server_name:''},rendered:{inbounds:[{protocol:'vmess'}],outbounds:[{type:'direct'}]}}
 return []
 }
 return {url:'https://vps.example.com/s/secret-test-token'}
 });wrapper=mount(Publish,{props:{api,hosts:[host]},attachTo:document.body})})
afterEach(()=>{wrapper.unmount();vi.useRealTimers();vi.restoreAllMocks()})
describe('domains and actual service controls',()=>{
 it('saves the domain without pretending a certificate was applied',async()=>{await flushPromises();await wrapper.get('input[placeholder="vps.example.com"]').setValue('new.example.com');await wrapper.findAll('form')[0].trigger('submit');await flushPromises();expect(api).toHaveBeenCalledWith('site','PUT',{domain:'new.example.com',https:true});expect(wrapper.text()).toContain('网关待同步')})
 it('asks before starting actual services',async()=>{await flushPromises();api.mockClear();await button('启用').trigger('click');expect(api).not.toHaveBeenCalled();expect(wrapper.find('[role=dialog]').text()).toContain('启动真实服务');await button('确认执行').trigger('click');await flushPromises();expect(api).toHaveBeenCalledWith('live/'+service.id+'/toggle','POST',{enabled:true})})
 it('reveals a share link only after creation and can hide it',async()=>{await flushPromises();const form=wrapper.findAll('form').at(-1)!;await wrapper.get('input[type=checkbox]').setValue(true);await form.trigger('submit');await flushPromises();expect((wrapper.get('[aria-label="订阅分享链接"]').element as HTMLInputElement).value).toContain('secret-test-token');await button('隐藏链接').trigger('click');expect(wrapper.find('[aria-label="订阅分享链接"]').exists()).toBe(false)})
 it('pauses polling in hidden tabs',async()=>{await flushPromises();api.mockClear();vi.spyOn(document,'visibilityState','get').mockReturnValue('hidden');await vi.advanceTimersByTimeAsync(45000);expect(api).not.toHaveBeenCalled()})
})
it('filters protocols under the selected kernel',async()=>{await flushPromises();await button('新建节点部署').trigger('click');const dialog=wrapper.get('[role=dialog]');const selects=dialog.findAll('select');await selects[1].setValue('xray');await flushPromises();const protocol=dialog.findAll('select')[3];expect(protocol.text()).toContain('VMess');expect(protocol.text()).toContain('VLESS');expect(protocol.text()).not.toContain('Hysteria2');expect(protocol.text()).not.toContain('sing-box')})
it('keeps transport settings when opening details and sends expected version',async()=>{await flushPromises();await button('详情 / 编辑').trigger('click');await flushPromises();const dialog=wrapper.get('[role=dialog]');expect((dialog.get('input[placeholder="/proxy"]').element as HTMLInputElement).value).toBe('/unchanged');expect(dialog.text()).toContain('完整入站 / 出站');await dialog.trigger('submit');await flushPromises();expect(api).toHaveBeenCalledWith('live/'+service.id,'PUT',expect.objectContaining({version:1,transport:'ws',path:'/unchanged',tls_mode:'none',kind:'vmess'}))})
it('exposes certificate and reverse proxy configuration as separate flows',async()=>{await flushPromises();await button('免费证书').trigger('click');expect(wrapper.text()).toContain('HTTP-01');expect(wrapper.text()).toContain('免费一键申请');await button('反向代理').trigger('click');expect(wrapper.text()).toContain('创建反代配置');expect(wrapper.find('input[placeholder="http://127.0.0.1:3000"]').exists()).toBe(true)})
it('keeps explicit client format links independent of the server kernel',async()=>{await flushPromises();await wrapper.get('input[type=checkbox]').setValue(true);await wrapper.findAll('form').at(-1)!.trigger('submit');await flushPromises();for(const client of ['sing-box','mihomo','shadowrocket']){await wrapper.get('[aria-label="订阅客户端格式"]').setValue(client);expect((wrapper.get('[aria-label="订阅分享链接"]').element as HTMLInputElement).value).toContain('format='+client)}})

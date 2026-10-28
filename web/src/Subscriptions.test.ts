// @vitest-environment happy-dom
import {it,expect,vi} from 'vitest'
import {mount,flushPromises} from '@vue/test-utils'
import Subscriptions from './Subscriptions.vue'
it('defaults a share to automatic recognition and prevents copying a stopped-node subscription',async()=>{
 let ready=false
 const api=vi.fn(async(path:string)=>{
  if(path==='subscriptions')return [{id:'s',name:'test',active:true,expires_at:'2030-01-01'}]
  if(path==='subscriptions/s/link')return {url:'https://example.test/s/private'}
  if(path.startsWith('subscriptions/s/check'))return {ready,reason:ready?'可导入':'没有运行中的节点'}
  return []
 })
 const w=mount(Subscriptions,{props:{api}});await flushPromises()
 const button=(t:string)=>w.findAll('button').find(b=>b.text()===t)!
 await button('分享链接').trigger('click');await flushPromises()
 expect((w.get('select[aria-label=客户端格式]').element as HTMLSelectElement).value).toBe('auto')
 expect(w.text()).toContain('没有运行中的节点')
 expect(button('复制订阅链接').attributes('disabled')).toBeDefined()
 ready=true;await button('重新检查').trigger('click');await flushPromises()
 expect(button('复制订阅链接').attributes('disabled')).toBeUndefined()
 expect(api).toHaveBeenCalledWith('subscriptions/s/check')
 w.unmount()
})

it('opens the existing user share without an add-subscription modal',async()=>{
 const api=vi.fn(async(p:string)=>p==='proxy-users/u/subscription'?{id:'s',url:'https://example.test/s/private',reused:true}:p==='proxy-users'?[{id:'u',name:'Alice',active:true}]:p.startsWith('subscriptions/s/check')?{ready:true,reason:'ok'}:[])
 const w=mount(Subscriptions,{props:{api,initialUser:'u'}});await flushPromises()
 expect(api).toHaveBeenCalledWith('proxy-users/u/subscription','POST',{})
 expect(w.find('[role=dialog]').exists()).toBe(false)
 expect(w.text()).toContain('原订阅链接')
 expect(w.text()).not.toContain('＋ 添加订阅')
 await w.get('select[aria-label=客户端格式]').setValue('shadowrocket');await flushPromises()
 expect((w.get('input[readonly]').element as HTMLInputElement).value).toContain('?client=shadowrocket&policy=nodes-only')
 w.unmount()
})

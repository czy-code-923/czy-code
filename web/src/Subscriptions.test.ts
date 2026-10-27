// @vitest-environment happy-dom
import {it,expect,vi} from 'vitest'
import {mount,flushPromises} from '@vue/test-utils'
import Subscriptions from './Subscriptions.vue'
it('defaults a share to Mihomo and prevents copying a stopped-node subscription',async()=>{
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
 expect(w.get('select').element.value).toBe('mihomo')
 expect(w.text()).toContain('没有运行中的节点')
 expect(button('复制订阅链接').attributes('disabled')).toBeDefined()
 ready=true;await button('重新检查').trigger('click');await flushPromises()
 expect(button('复制订阅链接').attributes('disabled')).toBeUndefined()
 expect(api).toHaveBeenCalledWith('subscriptions/s/check?client=mihomo')
 w.unmount()
})

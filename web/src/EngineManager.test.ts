// @vitest-environment happy-dom
import {it,expect,vi} from 'vitest'
import {mount,flushPromises} from '@vue/test-utils'
import Manager from './EngineManager.vue'
it('requests official latest or a selected tag without accepting a path or command',async()=>{
 const api=vi.fn(async(path:string,method='GET',body?:any)=>{
  if(path.endsWith('/kernels'))return [{engine:'sing-box',version:'sing-box version 1.14.4',ready:true}]
  if(path.endsWith('/engine-updates')&&method==='GET')return []
  if(path.startsWith('engine-releases'))return {version:'v1.14.4',official:true}
  return {id:'task',version:body?.version==='latest'?'v1.14.4':body?.version}
 })
 const w=mount(Manager,{props:{host:{id:'host',name:'VPS'},api}});await flushPromises()
 const button=(t:string)=>w.findAll('button').find(b=>b.text()===t)!
 await button('查询最新稳定版').trigger('click');await flushPromises();expect((w.get('[aria-label="指定内核版本"]').element as HTMLInputElement).value).toBe('v1.14.4')
 await button('更新至最新稳定版').trigger('click');await flushPromises();expect(api).toHaveBeenCalledWith('hosts/host/engine-updates','POST',{engine:'sing-box',version:'latest'})
 await w.get('select').setValue('xray');await w.get('[aria-label="指定内核版本"]').setValue('v26.10.21');await button('安装 / 切换指定版本').trigger('click');await flushPromises();expect(api).toHaveBeenCalledWith('hosts/host/engine-updates','POST',{engine:'xray',version:'v26.10.21'})
 w.unmount()
})

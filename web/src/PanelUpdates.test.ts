// @vitest-environment happy-dom
import {afterEach,it,expect,vi} from 'vitest'
import {mount,flushPromises,type VueWrapper} from '@vue/test-utils'
import PanelUpdates from './PanelUpdates.vue'
let wrapper:VueWrapper|undefined
afterEach(()=>{wrapper?.unmount();vi.restoreAllMocks()})
it('confirms a specific release before queuing and shows recovery status',async()=>{
 const jobs:any[]=[];const api=vi.fn(async(p:string,m='GET',b?:any)=>{
  if(p==='panel-release')return {current:'0.23.8',latest:'0.24.0',update_available:true,automatic_update:true,release_url:'https://github.com/czy-code-923/czy-code/releases/tag/v0.24.0-distribution'}
  if(m==='POST'){jobs.push({id:'job',from_version:b.current,target_version:b.version,state:'queued',updated_at:new Date().toISOString()});return {id:'job'}}
  return {current:'0.23.8',helper_ready:true,items:jobs}
 });wrapper=mount(PanelUpdates,{props:{api}});await flushPromises()
 await wrapper.findAll('button').find(b=>b.text()==='更新到 0.24.0')!.trigger('click')
 expect(api.mock.calls.some(c=>c[1]==='POST')).toBe(false);expect(wrapper.get('[role=dialog]').text()).toContain('备份数据库')
 await wrapper.findAll('button').find(b=>b.text()==='确认更新')!.trigger('click');await flushPromises()
 expect(api).toHaveBeenCalledWith('panel-updates','POST',{version:'0.24.0',current:'0.23.8'})
 expect(wrapper.text()).toContain('等待更新服务')
 expect(wrapper.findAll('button').find(b=>b.text()==='更新到 0.24.0')!.attributes('disabled')).toBeDefined()
})
it('does not allow updates without a live local helper',async()=>{
 const api=vi.fn(async(p:string)=>p==='panel-release'?{current:'0.23.8',latest:'0.24.0',update_available:true,automatic_update:false}:{current:'0.23.8',helper_ready:false,items:[]})
 wrapper=mount(PanelUpdates,{props:{api}});await flushPromises()
 expect(wrapper.text()).toContain('未就绪');expect(wrapper.findAll('button').find(b=>b.text()==='更新到 0.24.0')!.attributes('disabled')).toBeDefined()
})

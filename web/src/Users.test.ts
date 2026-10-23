// @vitest-environment happy-dom
import {afterEach,describe,it,expect,vi} from 'vitest'
import {mount,flushPromises,type VueWrapper} from '@vue/test-utils'
import Users from './Users.vue'
let w:VueWrapper
afterEach(()=>{w?.unmount();vi.restoreAllMocks()})
describe('users and group based subscriptions',()=>{
 it('saves explicit expiry and enabled state without submitting manual credentials',async()=>{const api=vi.fn(async()=>[]);w=mount(Users,{props:{api}});await flushPromises();await w.get('input[maxlength="100"]').setValue('Alice');await w.get('form').trigger('submit');await flushPromises();const mutation=api.mock.calls.find(c=>(c as any[])[0]==='proxy-users'&&(c as any[])[1]==='POST') as any[];expect(mutation[2]).toMatchObject({name:'Alice',enabled:true});expect(mutation[2].expires_at).toMatch(/Z$/);expect(mutation[2].password).toBeUndefined()})
 it('offers only managed inbounds for group authorization',async()=>{const api=vi.fn(async p=>p==='live'?[{id:'managed',name:'Managed VLESS',kind:'vless',managed_users:true},{id:'shared',name:'Shared Snell',kind:'snell',managed_users:false}]:[]);w=mount(Users,{props:{api}});await flushPromises();await w.findAll('nav button').find(b=>b.text()==='用户组与入站权限')!.trigger('click');expect(w.text()).toContain('Managed VLESS');expect(w.text()).not.toContain('Shared Snell')})
 it('sends a user and client template when creating a subscription',async()=>{const api=vi.fn(async(p,method='GET')=>method==='POST'?{url:'https://vps.example.com/s/fixture'}:p==='proxy-users'?[{id:'u',name:'Alice',active:true}]:p==='client-profiles'?[{id:'p',name:'Mihomo profile',format:'mihomo'}]:[]);w=mount(Users,{props:{api}});await flushPromises();await w.findAll('nav button').find(b=>b.text()==='用户订阅')!.trigger('click');await w.get('input[required]').setValue('Personal');await w.findAll('select')[0].setValue('u');await w.findAll('select')[1].setValue('p');await w.get('form').trigger('submit');await flushPromises();expect(api).toHaveBeenCalledWith('subscriptions','POST',{name:'Personal',user_id:'u',profile_id:'p',days:30});expect(w.text()).toContain('?format=mihomo')})
})

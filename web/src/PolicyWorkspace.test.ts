// @vitest-environment happy-dom
import {it,expect,vi} from 'vitest'
import {mount,flushPromises} from '@vue/test-utils'
import Policies from './Policies.vue'
type Row=Record<string,any>
function fixture(policy:Row){const source=JSON.stringify(policy);return vi.fn(async(path:string,method='GET',body?:any)=>{
 if(path==='client-policies/parse'){const p=body.policy??JSON.parse(body.source);return {policy:p,source:JSON.stringify(p)}}
 if(path==='client-policies/default')return {name:'Mihomo Max 默认模板',policy,source,native_options:{},notes:[]}
 if(path==='client-policies'&&method==='GET')return [{id:'p',name:'默认',user_id:'u',client_id:'mihomo',source,version:4,native_options:{}}]
 if(path==='proxy-users')return [{id:'u',name:'Alice'}]
 if(path==='client-catalog')return [{id:'mihomo',name:'Mihomo',full_config:true}]
 if(path==='live')return [{id:'n1',name:'VLESS',kind:'vless'}]
 return {}
 })}
const button=(w:any,text:string)=>w.findAll('button').find((b:any)=>b.text()===text)!
it('edits a single rule, paginates and saves the original scope without losing other sections',async()=>{
 const policy={basic:{'mixed-port':7890},dns:{nameserver:['1.1.1.1']},'proxy-groups':[{name:'choice',type:'select',proxies:['ALL']}],rules:[...Array.from({length:50},(_,i)=>`DOMAIN-SUFFIX,d${i}.example,choice`),'MATCH,choice']}
 const api=fixture(policy),w=mount(Policies,{props:{api}});await flushPromises();await button(w,'编辑').trigger('click');await flushPromises();expect(w.findAll('.rule-list li')).toHaveLength(25);await button(w,'下一页').trigger('click');expect(w.text()).toContain('d25.example');await w.get('[aria-label="编辑规则 26"]').trigger('click');await w.get('[aria-label="规则匹配内容"]').setValue('changed.example');await button(w,'保存此规则到草稿').trigger('click');await w.get('form').trigger('submit');await flushPromises();const body=api.mock.calls.find(c=>c[0]==='client-policies/p'&&c[1]==='PUT')![2];expect(body).toMatchObject({version:4,user_id:'u',client_id:'mihomo'});const saved=JSON.parse(body.source);expect(saved.rules[25]).toBe('DOMAIN-SUFFIX,changed.example,choice');expect(saved.rules.at(-1)).toBe('MATCH,choice');expect(saved.basic['mixed-port']).toBe(7890);expect(saved.dns.nameserver).toEqual(['1.1.1.1']);w.unmount()
})
it('renames group references in other groups, rules and DNS together',async()=>{
 const api=fixture({basic:{},dns:{nameserver:['https://1.1.1.1/dns-query#old']},'proxy-groups':[{name:'old',type:'select',proxies:['ALL']},{name:'child',type:'select',proxies:['old']}],rules:['MATCH,old']});const w=mount(Policies,{props:{api}});await flushPromises();await button(w,'编辑').trigger('click');await flushPromises();await button(w,'编辑组').trigger('click');await w.get('[aria-label="组名"]').setValue('new');await button(w,'保存此组到草稿').trigger('click');await w.get('form').trigger('submit');await flushPromises();const saved=JSON.parse(api.mock.calls.find(c=>c[0]==='client-policies/p'&&c[1]==='PUT')![2].source);expect(saved['proxy-groups'][1].proxies).toEqual(['new']);expect(saved.rules).toEqual(['MATCH,new']);expect(saved.dns.nameserver[0]).toContain('#new');w.unmount()
})
it('blocks save and tab switches until a manual section JSON edit is applied',async()=>{
 const api=fixture({basic:{'mixed-port':7890},dns:{},'proxy-groups':[],rules:['MATCH,DIRECT']});const w=mount(Policies,{props:{api}});await flushPromises();await button(w,'编辑').trigger('click');await flushPromises();await button(w,'基础配置').trigger('click');await w.get('[aria-label="分区 JSON"]').setValue('{"mixed-port":7999}');await button(w,'DNS').trigger('click');expect(w.text()).toContain('请先应用或取消分区 JSON 修改');await w.get('form').trigger('submit');await flushPromises();expect(api.mock.calls.some(c=>c[1]==='PUT')).toBe(false);await button(w,'应用分区 JSON').trigger('click');await w.get('form').trigger('submit');await flushPromises();const body=api.mock.calls.find(c=>c[1]==='PUT')![2];expect(JSON.parse(body.source).basic['mixed-port']).toBe(7999);w.unmount()
})

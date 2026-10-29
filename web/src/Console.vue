<script setup lang="ts">
import {ref,computed,watch,onUnmounted,onMounted,defineAsyncComponent} from 'vue'
import Hosts from './Hosts.vue'
const Traffic=defineAsyncComponent(()=>import('./Traffic.vue'))
const Topology=defineAsyncComponent(()=>import('./Topology.vue'))
const Users=defineAsyncComponent(()=>import('./Access.vue'))
const Subscriptions=defineAsyncComponent(()=>import('./Subscriptions.vue'))
const Policies=defineAsyncComponent(()=>import('./Policies.vue'))
const Settings=defineAsyncComponent(()=>import('./Settings.vue'))
const hostname=window.location.hostname
const token=ref(''),connected=ref(false),busy=ref(false),error=ref(''),page=ref('overview'),hosts=ref<any[]>([]),selectedHost=ref(''),selectedUser=ref('')
const pages=[{id:'hosts',name:'连接主机',sub:'登记 VPS · 一键接入',icon:'▣'},{id:'nodes',name:'节点配置',sub:'协议 · 入站 · 出站',icon:'◈'},{id:'users',name:'用户权限',sub:'用户 · 用户组',icon:'♙'},{id:'policies',name:'客户端策略',sub:'策略组 · 规则 · 自动转换',icon:'⇄'},{id:'subscriptions',name:'订阅分享',sub:'用户 · 有效期 · 链接',icon:'↗'}]
const current=computed(()=>page.value==='overview'?{name:'首页',sub:'运行状态 · 流量 · 配额 · 访问记录'}:page.value==='usage'?{name:'用户使用记录',sub:'用量与连接元数据'}:pages.find(p=>p.id===page.value)??{name:'系统设置',sub:'域名、证书、反代与防火墙'})
function preference(){try{return localStorage.getItem('harbor-theme')??'dark'}catch{return 'dark'}}
const theme=ref(preference()),media=window.matchMedia('(prefers-color-scheme: dark)'),dark=ref(media.matches)
const changed=(e:MediaQueryListEvent)=>dark.value=e.matches;media.addEventListener('change',changed)
watch(theme,v=>{try{localStorage.setItem('harbor-theme',v)}catch{}})
const resolved=computed(()=>theme.value==='system'?(dark.value?'dark':'light'):theme.value)
let epoch=0,abort=new AbortController(),timer:ReturnType<typeof setTimeout>,alive=true
async function api(path:string,method='GET',body?:unknown){
 const session=epoch
 const res=await fetch('/api/v1/'+path,{method,headers:{Authorization:'Bearer '+token.value,'Content-Type':'application/json'},signal:AbortSignal.any([abort.signal,AbortSignal.timeout(12000)]),body:body===undefined?undefined:JSON.stringify(body)})
 const data=await res.json();if(session!==epoch)throw new Error('会话已结束')
 if(!res.ok){if(res.status===401&&connected.value)logout();throw new Error(data.error??'请求失败')};return data
}
function schedule(){clearTimeout(timer);if(connected.value)timer=setTimeout(()=>{if(document.visibilityState==='visible')void refresh();else schedule()},30000)}
async function refresh(){const session=epoch;try{const data=await api('hosts');if(session!==epoch||!alive)return;hosts.value=data;error.value=''}catch(e){if(session===epoch)error.value=String(e)}finally{if(session===epoch)schedule()}}
async function login(){if(busy.value)return;busy.value=true;error.value='';const session=epoch;try{const data=await api('hosts');if(session!==epoch||!alive)return;hosts.value=data;connected.value=true;schedule()}catch(e){if(session===epoch)error.value=String(e)}finally{if(session===epoch)busy.value=false}}
function logout(){epoch++;abort.abort();abort=new AbortController();clearTimeout(timer);connected.value=false;token.value='';hosts.value=[];selectedUser.value='';selectedHost.value='';page.value='overview';busy.value=false}
function configure(id:string){selectedHost.value=id;page.value='nodes'}
function usage(id:string){selectedUser.value=id;page.value='usage'}
function share(id:string){selectedUser.value=id;page.value='subscriptions'}
onUnmounted(()=>{alive=false;logout();media.removeEventListener('change',changed)})
const siteTitle=ref('星枢')
async function branding(){try{const r=await fetch('/api/v1/branding',{signal:AbortSignal.timeout(5000)});if(r.ok){const b=await r.json();siteTitle.value=b.title||'星枢';document.title=siteTitle.value+' · VPS 多机管理'}}catch{}}
onMounted(()=>void branding())
</script>
<template>
<div class="console-root" :data-theme="resolved">
 <form v-if="!connected" class="entry" @submit.prevent="login"><div class="entry-brand">◈ {{siteTitle}}<span>VPS 多机管理</span></div><h1>让每一台 VPS，各司其职。</h1><p>连接主机，配置协议，为用户分配权限并分享订阅。</p><label>管理员令牌<input v-model="token" type="password" autocomplete="off" required placeholder="输入管理员令牌"></label><p v-if="error" role="alert" class="error">{{error}}</p><button class="primary" :disabled="busy">{{busy?'连接中…':'进入控制台'}}</button></form>
 <div v-else class="console-layout">
 <aside class="console-sidebar"><a class="console-brand" href="#" @click.prevent="page='overview'"><b>◈</b> {{siteTitle}}<small>VPS 多机管理</small></a><button class="secondary console-home" :class="{active:page==='overview'}" @click="page='overview'">◉ 首页与用量</button><span class="nav-caption">管理工作流</span><nav aria-label="主管理导航"><button v-for="(p,i) in pages" :key="p.id" :class="{active:page===p.id}" @click="selectedUser='';page=p.id"><span class="step-number">0{{i+1}}</span><span>{{p.name}}<small>{{p.sub}}</small></span></button></nav><div class="sidebar-bottom"><button class="secondary" :class="{active:page==='settings'}" @click="page='settings'">⚙ 系统设置</button><p>主机 → 协议 → 权限 → 策略 → 订阅</p><small>星枢 0.23.1 · 统一控制面</small></div></aside>
 <main class="console-main"><header class="console-header"><span>工作区 <b>/ {{current.name}}</b></span><div><label class="theme-picker">主题<select v-model="theme" aria-label="主题"><option value="dark">深色</option><option value="light">浅色</option><option value="system">跟随系统</option></select></label><button class="text-button" @click="logout">退出登录</button></div></header>
 <div class="console-content"><div class="console-title"><div><span class="eyebrow">STARHUB / {{page.toUpperCase()}}</span><h1>{{current.name}}</h1><p>{{current.sub}}</p></div><span class="control-domain">{{hostname}}</span></div><p v-if="error" class="error" role="alert">{{error}}</p>
 <Traffic v-if="page==='overview'||page==='usage'" :key="page+selectedUser" :api="api" :user-id="page==='usage'?selectedUser:undefined"/>
 <Hosts v-else-if="page==='hosts'" :api="api" :hosts="hosts" @changed="refresh" @configure="configure"/>
 <Topology v-else-if="page==='nodes'" :api="api" :hosts="hosts" :initial-host="selectedHost" @changed="refresh"/>
 <Users v-else-if="page==='users'" :api="api" @share="share" @usage="usage"/>
 <Policies v-else-if="page==='policies'" :api="api"/>
 <Subscriptions v-else-if="page==='subscriptions'" :api="api" :initial-user="selectedUser" @policies="page='policies'"/>
 <Settings v-else :api="api" :hosts="hosts" @branding="branding"/>
 </div><footer class="console-footer">{{siteTitle}} <span>连接状态与配置结果以 Agent 回报为准</span></footer>
 </main></div>
</div>
</template>

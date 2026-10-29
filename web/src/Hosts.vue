<script setup lang="ts">
import {ref,computed,defineAsyncComponent} from 'vue'
import HostConnect from './HostConnect.vue'
import HostTrafficFields from './HostTrafficFields.vue'
import {hostDraft,hostPayload,hostBytes,hostModes} from './hostBilling'
const HostTraffic=defineAsyncComponent(()=>import('./HostTraffic.vue'))
const trafficHost=ref<Record<string,any>|null>(null)
const EngineManager=defineAsyncComponent(()=>import('./EngineManager.vue'))
const kernelHost=ref<Record<string,any>|null>(null)
type Row=Record<string,any>
const props=defineProps<{api:(p:string,m?:string,b?:any)=>Promise<any>,hosts:Row[]}>()
const emit=defineEmits<{changed:[],configure:[id:string]}>()
const editing=ref<Row|null>(null),connecting=ref<Row|null>(null),removing=ref<Row|null>(null),busy=ref(false),error=ref(''),search=ref('')
const rows=computed(()=>props.hosts.filter(h=>(h.name+' '+h.address+' '+h.provider).toLowerCase().includes(search.value.toLowerCase())))
const online=(h:Row)=>h.online===true
async function run(fn:()=>Promise<void>){busy.value=true;error.value='';try{await fn();emit('changed')}catch(e){error.value=String(e)}finally{busy.value=false}}
function save(){const h=editing.value;if(!h)return;void run(async()=>{const saved=await props.api('hosts'+(h.id?'/'+h.id:''),h.id?'PATCH':'POST',{name:h.name,provider:h.provider,address:h.address,traffic:hostPayload(h.billing,!h.id)});editing.value=null;if(!h.id)connecting.value={id:saved.id,name:h.name}})}
function remove(){const h=removing.value;if(!h)return;void run(async()=>{await props.api('hosts/'+h.id+(h.agent_version?'/retirement':''),h.agent_version?'POST':'DELETE',h.agent_version?{}:undefined);removing.value=null})}
</script>
<template>
<section class="panel publish-card"><div class="section-heading"><div><h2>你的服务器</h2><p class="muted">添加主机后，复制接入命令到该 VPS 执行即可。支持主动出站连接，无需开放 Agent 管理端口。</p></div><button class="primary" @click="editing={name:'',provider:'',address:'',billing:hostDraft()}">＋ 连接新主机</button></div><input v-model="search" class="console-search" placeholder="搜索名称、地址或服务商" aria-label="搜索主机"><p v-if="error" class="error" role="alert">{{error}}</p>
<div class="host-card-grid"><article v-for="h in rows" :key="h.id" class="server-card"><div class="server-heading"><span class="server-icon">▣</span><span :class="['status',online(h)?'succeeded':'pending']">{{h.retire_state?'等待远端卸载 · '+h.retire_state:online(h)?'已连接':'未连接'}}</span></div><h3>{{h.name}}</h3><p>{{h.address}}</p><small>{{h.provider}} · {{h.platform||'等待安装'}} · Agent {{h.agent_version||'—'}}</small><div class="host-quota"><strong>{{h.traffic?.quota_bytes?hostBytes(h.traffic.remaining_bytes):'不限额'}}</strong><small>套餐剩余 · {{hostModes[h.traffic?.mode??'both']}}</small><progress v-if="h.traffic?.quota_bytes" :max="h.traffic.quota_bytes" :value="h.traffic.used_bytes"></progress><small>已用 {{hostBytes(h.traffic?.used_bytes)}} / {{h.traffic?.quota_bytes?hostBytes(h.traffic.quota_bytes):'未设额度'}}</small><small :class="{error:!h.traffic?.fresh}">{{h.traffic?.fresh?'最新采样':'等待 / 已断报'}}{{h.traffic?.last_sample_at?' · '+new Date(h.traffic.last_sample_at).toLocaleTimeString():''}}{{h.traffic?.quality==='gap'?' · 需核对校准':''}}</small></div><div class="server-actions"><button class="primary" @click="emit('configure',h.id)">配置协议 →</button><button class="text-button" @click="trafficHost=h">主机流量</button><button class="text-button" @click="kernelHost=h">内核版本</button><button class="text-button" @click="connecting=h">{{h.agent_version?'连接 / 恢复':'接入命令'}}</button><button class="text-button" @click="editing={...h,billing:hostDraft(h.traffic??{})}">编辑</button><button class="text-button danger" @click="removing=h">删除</button></div></article></div><div v-if="!rows.length" class="console-empty">还没有主机。连接第一台 VPS，开始配置协议。</div>
</section>
<HostTraffic v-if="trafficHost" :host="trafficHost" :api="api" @close="trafficHost=null" @changed="emit('changed')"/>
<EngineManager v-if="kernelHost" :host="kernelHost" :api="api" @close="kernelHost=null"/>
<HostConnect v-if="connecting" :host="connecting as {id:string,name:string,agent_version?:string,last_seen?:string}" :api="api" @close="connecting=null" @connected="emit('changed')"/>
<div v-if="editing" class="modal-backdrop" @keydown.esc="editing=null"><form class="modal" role="dialog" aria-modal="true" aria-label="主机信息" @submit.prevent="save"><div class="modal-heading"><h2>{{editing.id?'编辑主机':'连接新主机'}}</h2><button type="button" class="secondary" @click="editing=null">关闭</button></div><div class="form-content"><p v-if="error" role="alert" class="error">{{error}}</p><div class="fields"><label>主机名称<input v-model="editing.name" required maxlength="100"></label><label>服务商 / 标签<input v-model="editing.provider" required placeholder="DMIT、绿云…"></label><label class="full-field">主机地址<input v-model="editing.address" placeholder="留空由 Agent 回报 IP，也可手动填写"></label></div><h3>主机套餐与流量</h3><HostTrafficFields :draft="editing.billing" :available="editing.traffic?.interfaces_available" :fresh="!editing.id"/><p class="muted">地址用于识别主机。Agent 主动连接控制端；没有公网 IPv4 也可以接入。对外分享地址在协议配置中设置。</p></div><div class="modal-footer"><button type="button" class="secondary" @click="editing=null">取消</button><button class="primary" :disabled="busy">{{editing.id?'保存修改':'保存并生成接入命令'}}</button></div></form></div>
<div v-if="removing" class="modal-backdrop"><section class="modal" role="dialog" aria-modal="true" aria-label="删除主机"><div class="form-content"><h2>删除 {{removing.name}}？</h2><p>此操作会停止并清理远端 Agent、托管内核与配置，并删除后台对此主机的部署及流量数据。控制面、数据库、审计和其他软件会保留。离线主机等待回执；需要 Agent 0.12.0 的受控卸载器。跨主机链式 / FRP 依赖需要先解除。</p><p v-if="error" role="alert" class="error">{{error}}</p></div><div class="modal-footer"><button class="secondary" @click="removing=null">取消</button><button class="primary" :disabled="busy" @click="remove">确认删除</button></div></section></div>
</template>

<style scoped>.host-quota{margin:18px 0;padding:14px;border:1px solid var(--border);border-radius:10px}.host-quota strong{display:block;font-size:23px;margin-bottom:6px}.host-quota small{display:block;font-size:11px;line-height:1.9;color:var(--muted)}.host-quota progress{width:100%;height:5px;accent-color:var(--accent);margin:10px 0}</style>


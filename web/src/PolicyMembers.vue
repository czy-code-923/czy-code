<script setup lang="ts">
import {computed,ref} from 'vue'
import {matchesEndpoint,replaceNodeSelectors,selectedEndpoints,selectionMode,toggleEndpoint} from './policySelection'
const props=defineProps<{modelValue:string[],nodes:Record<string,any>[],references:string[],label:(s:string)=>string}>()
const emit=defineEmits<{ 'update:modelValue':[string[]] }>()
const search=ref('')
const mode=computed(()=>selectionMode(props.modelValue))
const inventory=computed(()=>props.nodes.filter(n=>!n.delete_requested_at&&!['frps','frpc'].includes(n.kind)&&n.reference))
const selected=computed(()=>selectedEndpoints(props.modelValue,inventory.value))
const missing=computed(()=>props.modelValue.filter(ref=>ref.startsWith('endpoint:')&&!inventory.value.some(n=>n.reference===ref)))
const families:Record<string,string>={ipv4:'IPv4',ipv6:'IPv6',dns:'域名'}
const nodeGroups=computed(()=>{
 const grouped=new Map<string,Record<string,any>>()
 for(const n of inventory.value){
  if(search.value&&!`${n.node_name??n.name} ${n.kind} ${n.address}`.toLowerCase().includes(search.value.toLowerCase()))continue
  if(!grouped.has(n.id))grouped.set(n.id,{id:n.id,name:n.node_name??n.name,kind:n.kind,endpoints:[]})
  grouped.get(n.id)!.endpoints.push(n)
 }
 return Array.from(grouped.values())
})
const advanced=computed(()=>props.references.filter(r=>r!=='ALL'&&!['node:','endpoint:','family:'].some(prefix=>r.startsWith(prefix))))
function setMode(value:string){
 const next=value==='manual'?selected.value:[value==='all'?'ALL':'family:'+value]
 emit('update:modelValue',replaceNodeSelectors(props.modelValue,next))
}
function toggleNode(n:Record<string,any>,checked:boolean){emit('update:modelValue',toggleEndpoint(props.modelValue,inventory.value,n.reference,checked))}
function toggle(ref:string,checked:boolean){emit('update:modelValue',checked?Array.from(new Set([...props.modelValue,ref])):props.modelValue.filter(r=>r!==ref))}
</script>
<template><fieldset class="member-picker"><legend>节点成员</legend><div class="member-modes"><label v-for="[value,title] in [['all','全部节点'],['ipv4','仅 IPv4'],['ipv6','仅 IPv6'],['manual','手动勾选']]" :key="value"><input type="radio" name="policy-node-mode" :checked="mode===value" :aria-label="title" @change="setMode(value!)">{{title}}</label></div><p class="muted">动态模式自动包含以后新增的授权节点。直接勾选下方地址会转为手选，保留当前选中的其他地址与默认顺序；同一节点可独立选择 IPv4 / IPv6。实际订阅仅发布已授权、运行中且兼容客户端的端点。</p><p v-if="mode==='custom'" class="selection-summary">当前使用组合动态筛选；勾选地址会固定当前匹配结果。</p><p class="selection-summary">已匹配 {{selected.length}} 个地址端点 · {{mode==='manual'?'手选端点保持固定':'动态选择随节点更新'}}</p><p v-if="missing.length" class="muted">{{missing.length}} 个手选端点暂不在当前授权清单，保留其引用；实际订阅不会发布未授权或已删除的端点。</p><input v-model="search" aria-label="搜索策略组节点" placeholder="搜索节点、协议或地址"><div class="node-checks"><article v-for="group in nodeGroups" :key="group.id" class="node-family-card"><header><strong>{{group.name}}</strong><span>{{group.kind}} · {{group.id.slice(0,8)}}</span></header><label v-for="n in group.endpoints" :key="n.reference"><input type="checkbox" :aria-label="`${group.name} ${families[n.family]??'端点'} ${group.id.slice(0,8)}`" :checked="modelValue.some(r=>matchesEndpoint(r,n))" @change="toggleNode(n,($event.target as HTMLInputElement).checked)"><span><b>{{families[n.family]??n.family??'地址'}}</b> {{n.address}}<small>{{n.enabled===false?'已停用':n.online===true?'运行中':n.state==='running'?'等待最新回报':'未就绪'}} · {{n.reference}}</small></span></label></article><p v-if="!nodeGroups.length" class="muted">没有匹配节点；动态选择会在以后出现授权节点时自动匹配。</p></div><details><summary>其他策略组、直连与协议 / 主机动态引用</summary><div class="advanced-checks"><label v-for="r in advanced" :key="r"><input type="checkbox" :checked="modelValue.includes(r)" @change="toggle(r,($event.target as HTMLInputElement).checked)">{{label(r)}}</label></div></details></fieldset></template>
<style scoped>.member-picker{min-width:0;border:1px solid var(--border);border-radius:10px;padding:14px;margin:16px 0}.member-picker legend{font-size:12px;color:var(--accent)}.member-modes{display:flex;flex-wrap:wrap;gap:12px}.member-picker label{display:flex;align-items:flex-start;gap:8px;font-size:12px;line-height:1.6}.member-picker input[type=radio],.member-picker input[type=checkbox]{flex:none;margin-top:3px}.member-picker>input{width:100%;padding:10px;background:var(--input);border:1px solid var(--border);color:var(--ink);border-radius:8px}.node-checks,.advanced-checks{max-height:340px;overflow:auto;display:grid;gap:10px;padding:12px 0;overscroll-behavior:contain}.node-family-card{border:1px solid var(--border);border-radius:8px;padding:10px;min-width:0}.node-family-card header{display:flex;flex-wrap:wrap;gap:6px;justify-content:space-between;margin-bottom:10px;font-size:12px}.node-family-card header span{color:var(--muted);font-size:10px}.node-family-card label+label{margin-top:9px}.node-family-card label>span{overflow-wrap:anywhere;min-width:0}.node-checks small{display:block;color:var(--muted);font-size:10px}.member-picker p{font-size:12px;line-height:1.7}.member-picker summary{font-size:12px;margin-top:10px}.selection-summary{color:var(--accent);margin:6px 0}</style>

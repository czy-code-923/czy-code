<script setup lang="ts">
import {ref,onMounted,computed} from 'vue'
const props=defineProps<{api:(p:string,m?:string,b?:any)=>Promise<any>}>()
const rows=ref<Record<string,any>[]>([]),error=ref(''),notice=ref(''),busy=ref(false),confirm=ref(false)
const candidates=computed(()=>rows.value.filter(r=>!r.protected))
async function reload(){rows.value=await props.api('maintenance/trials')}
onMounted(()=>void reload().catch(e=>error.value=String(e)))
async function clean(){busy.value=true;error.value='';try{const r=await props.api('maintenance/trials','POST',{ids:candidates.value.map(r=>r.id)});confirm.value=false;notice.value=`已移除 ${r.removed} 项，${r.pending_stop} 项等待 Agent 停止回执。`;await reload()}catch(e){error.value=String(e)}finally{busy.value=false}}
</script>
<template><section class="panel publish-card"><h2>清理前期验收数据</h2><p class="muted">仅识别停用且名称以 Acceptance 开头的验收配置。运行中配置、实际用户授权与普通订阅会受到保护；保留审计。清理其他配置请在相应列表查看依赖后删除。</p><p v-if="notice" class="notice" role="status">{{notice}}</p><p v-if="error" class="error" role="alert">{{error}}</p><div v-for="r in rows" :key="r.id" class="setting-row"><div><b>{{r.name}}</b><small class="block-muted">{{r.kind}} · {{r.state}}</small></div><span class="status">{{r.protected?'有实际引用 · 保护':'可清理'}}</span></div><p v-if="!rows.length" class="console-empty">没有可识别的旧验收配置。</p><button class="secondary" :disabled="busy||!candidates.length" @click="confirm=true">清理 {{candidates.length}} 项验收配置</button></section><div v-if="confirm" class="modal-backdrop"><section class="modal" role="dialog" aria-modal="true" aria-label="清理验收数据"><div class="form-content"><h2>清理这些停用验收配置？</h2><p>删除配套测试订阅及未引用的验收节点，释放已确认停止的端口；未确认停止的配置等待 Agent 回执。受实际授权或外部链式配置引用时整个操作拒绝执行。</p><p v-if="error" class="error">{{error}}</p></div><div class="modal-footer"><button class="secondary" @click="confirm=false">取消</button><button class="primary" :disabled="busy" @click="clean">确认清理</button></div></section></div></template>

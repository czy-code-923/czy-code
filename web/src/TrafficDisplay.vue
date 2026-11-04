<script setup lang="ts">
import {onMounted,ref} from 'vue'
import {normalizeTrafficUnit,trafficBytes,type TrafficUnit} from './trafficUnits'
const props=defineProps<{api:(p:string,m?:string,b?:any)=>Promise<any>}>(),emit=defineEmits<{saved:[]}>()
const unit=ref<TrafficUnit>('GB'),busy=ref(false),loaded=ref(false),error=ref(''),notice=ref('')
onMounted(async()=>{try{unit.value=normalizeTrafficUnit((await props.api('display-settings')).traffic_unit);loaded.value=true}catch(e){error.value=String(e)}})
async function save(){if(busy.value||!loaded.value)return;busy.value=true;error.value='';try{const s=await props.api('display-settings','PUT',{traffic_unit:unit.value});unit.value=normalizeTrafficUnit(s.traffic_unit);emit('saved');notice.value='流量显示设置已保存，首页和用户用量已同步。'}catch(e){error.value=String(e)}finally{busy.value=false}}
</script>
<template><section class="panel publish-card"><h2>流量显示单位</h2><p class="muted">统一首页、用户额度、入口流量和访问记录的显示；各主机套餐仍跟随主机编辑中选择的 GB / GiB。设置保存在后台，电脑和手机共用。</p><p v-if="error" class="error" role="alert">{{error}}</p><p v-if="notice" class="notice" role="status">{{notice}}</p><form @submit.prevent="save"><div class="fields"><label>显示方式<select v-model="unit" aria-label="全局流量显示单位" :disabled="!loaded"><option value="GB">GB · 十进制（1 GB = 10⁹ 字节）</option><option value="GiB">GiB · 二进制（1 GiB = 2³⁰ 字节）</option><option value="auto">自动 · 按大小显示 B / KB / MB / GB / TB</option></select></label><div><small class="block-muted">显示示例 · 500,000,000,000 字节</small><strong>{{trafficBytes(500e9,unit)}}</strong></div></div><p class="form-note">切换单位只改变显示和配额输入换算，保留实际字节额度、已用量与计费周期。小额流量保留更多小数，避免显示成 0.00 GB。订阅用量头继续按标准传递字节数，客户端自行显示单位。</p><button class="primary" :disabled="busy||!loaded">保存显示设置</button></form></section></template>

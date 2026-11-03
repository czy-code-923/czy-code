<script setup lang="ts">
import {ref,watch} from 'vue'
const props=defineProps<{form:Record<string,any>}>()
const enabled=ref(!!props.form.hop_ports)
watch(()=>props.form,()=>{enabled.value=!!props.form.hop_ports})
function toggle(){props.form.hop_ports=enabled.value?'30000-30100':'';props.form.hop_interval_seconds=enabled.value?30:0}
</script>
<template><fieldset v-if="['hysteria','hysteria2'].includes(form.kind)&&form.engine!=='xray'" class="port-hopping"><legend>UDP 端口跳跃 · 可选</legend><label class="check-label"><input v-model="enabled" type="checkbox" aria-label="启用端口跳跃" @change="toggle">启用端口跳跃</label><div v-if="enabled" class="fields"><label>跳跃端口 / 范围<input v-model="form.hop_ports" required maxlength="256" placeholder="30000-30100,30200-30250" aria-label="跳跃端口范围"><small>逗号分隔，1024–65535，最多 16 段 / 4096 个端口；不能与已有部署重叠。</small></label><label>跳跃间隔（秒）<input v-model.number="form.hop_interval_seconds" type="number" min="5" max="3600" required aria-label="跳跃间隔"><small>默认 30 秒，适用于支持跳跃的客户端。</small></label></div><p class="muted">{{enabled?'服务端仍使用原监听端口；Linux Agent 0.23.14+ 配合本机受限助手将指定 UDP 范围转发到此端口。请先在主机「连接 / 恢复」生成并执行端口跳跃助手命令，放行云安全组及系统防火墙的原 UDP 监听端口和跳跃范围。':'默认关闭，保持原有单端口配置。Hysteria / HY2 支持此选项，TUIC 等协议不套用。'}}</p><p v-if="enabled" class="muted">Mihomo / sing-box 完整配置自动输出对应字段；纯节点 URI 客户端可能仅使用原监听端口。IPv4 / IPv6 随节点监听配置，助手不修改已有防火墙放行策略。</p></fieldset></template>
<style scoped>.port-hopping{margin:20px 0;padding:18px;border:1px solid var(--line);border-radius:12px;background:var(--panel-soft)}legend{padding:0 8px;color:var(--accent);font-size:12px}small{display:block;margin-top:7px;color:var(--muted);font-weight:400}p{margin:12px 0 0}@media(max-width:600px){.port-hopping{padding:12px}}</style>

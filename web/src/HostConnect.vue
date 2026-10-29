<script setup lang="ts">
import {ref,onMounted,onUnmounted} from 'vue'
const props=defineProps<{host:{id:string,name:string,agent_version?:string,last_seen?:string},api:(path:string,method?:string,body?:unknown)=>Promise<any>}>()
const emit=defineEmits<{close:[],connected:[]}>()
const allowHTTP=ref(false)
const existing=!!(props.host.agent_version||props.host.last_seen)
const command=ref(''),expires=ref(''),state=ref('waiting'),error=ref(''),busy=ref(false),copied=ref(false)
let timer:ReturnType<typeof setTimeout>|undefined,disposed=false
const names:Record<string,string>={waiting:'等待执行安装命令',installed:'已注册，等待 Agent 心跳',connected:'主机已连接',expired:'安装命令已过期',revoked:'安装命令已撤销'}
function schedule(){if(!disposed)timer=setTimeout(()=>{if(document.visibilityState==='hidden')schedule();else void status()},5000)}
async function status(){try{const r=await props.api(`hosts/${props.host.id}/enrollment`);if(disposed)return;state.value=r.state;if(r.state==='connected'){command.value='';emit('connected');return}}catch(e){if(!disposed)error.value=String(e)}schedule()}
async function generate(){busy.value=true;error.value='';copied.value=false;try{const r=await props.api(`hosts/${props.host.id}/enrollment`,'POST',{allow_insecure_http:allowHTTP.value});if(disposed)return;command.value=r.command;expires.value=new Date(r.expires_at).toLocaleTimeString('zh-CN');state.value='waiting';clearTimeout(timer);void status()}catch(e){if(!disposed)error.value=String(e)}finally{if(!disposed)busy.value=false}}
async function revoke(){busy.value=true;try{await props.api(`hosts/${props.host.id}/enrollment/revoke`,'POST',{});command.value='';state.value='revoked'}catch(e){error.value=String(e)}finally{busy.value=false}}
async function copy(){try{await navigator.clipboard.writeText(command.value);copied.value=true}catch{error.value='浏览器未允许复制，请选中下方命令手动复制。'}}
async function checkConnection(){busy.value=true;error.value='';try{const hosts=await props.api('hosts');const host=hosts.find((h:any)=>h.id===props.host.id);if(!host)throw new Error('主机不存在或已删除');state.value=host.online?'connected':'offline';emit('connected')}catch(e){error.value=String(e)}finally{busy.value=false}}
onMounted(async()=>{try{if(existing){await checkConnection();return}const r=await props.api(`hosts/${props.host.id}/enrollment`);if(disposed)return;state.value=r.state;if(r.state!=='connected')await generate()}catch(e){error.value=String(e)}})
onUnmounted(()=>{disposed=true;clearTimeout(timer);command.value=''})
</script>

<template>
<div class="modal-backdrop" @click.self="emit('close')"><section class="modal connect-modal" role="dialog" aria-modal="true" aria-labelledby="connect-title">
 <div class="modal-heading"><div><span class="eyebrow">CONNECT A HOST</span><h2 id="connect-title">连接 {{host.name}}</h2></div><button class="icon-button" aria-label="关闭连接向导" @click="emit('close')">×</button></div>
 <div class="form-content">
  <div :class="['connect-status',state==='connected'?'online':'']" role="status"><span class="connect-light"></span><div><b>{{state==='offline'?'主机暂时离线':names[state]??state}}</b><small>由主机的真实心跳确认连接</small></div></div>
  <div v-if="error" class="error" role="alert">{{error}}</div>
  <div v-if="existing && !command"><p>Agent 会主动重试连接。点击“检查连接”读取最新心跳；离线时面板无法远程启动 Agent。</p><p>若仍未恢复，请在该主机的 ServerBox / SSH 终端执行：</p><pre class="install-command">sudo systemctl restart harbor-agent.service</pre><p class="muted">重启可能短暂中断该主机由 Agent 管理的服务。若仍离线，执行 sudo systemctl status harbor-agent.service --no-pager 查看状态，并检查控制端地址、DNS 和 HTTPS。</p><button class="secondary" :disabled="busy" @click="checkConnection">{{busy?'正在检查…':'检查连接'}}</button></div>
  <ol v-if="!existing || command" class="connect-steps"><li><b>复制安装命令</b><span>支持 Linux amd64 / arm64、systemd；目标机需有 curl 和 Python 3。</span></li><li><b>在目标 VPS 的 SSH 终端粘贴执行</b><span>自动校验并安装 Agent、sing-box 和 FRP，注册后设置开机启动。</span></li><li><b>回到这里，等待显示“主机已连接”</b><span>Agent 主动通过 HTTPS 连接总管理端，无需开放管理端口，也不要求目标机有公网 IPv4。</span></li></ol>
  <template v-if="command"><label class="wide-field">在目标 VPS 执行<textarea class="install-command" :value="command" readonly spellcheck="false" @focus="($event.target as HTMLTextAreaElement).select()"></textarea></label><div class="connect-command-actions"><button class="primary" @click="copy">{{copied?'已复制':'复制一键安装命令'}}</button><small>有效至 {{expires}} · 仅可注册一次</small></div><p class="muted">命令包含临时注册凭证，请勿公开分享。实际 Agent 凭据由目标机自行保存，总管理令牌不会传过去。</p></template>
  <p v-if="state==='connected'" class="notice">可以关闭向导，开始为这台主机创建代理服务或 FRP 隧道。</p>
  <label class="check-label"><input v-model="allowHTTP" type="checkbox">允许 HTTP 控制入口（仅在未启用 HTTPS 时使用；凭据与配置将明文传输）</label><details class="connect-details"><summary>已有 Agent、重装与故障排查</summary><p>重复安装会保护已有 Agent 并停止。生成新命令会撤销未使用的旧命令；只有新命令完成注册后，旧连接凭据才失效。新安装失败会清理本次安装文件；修复终端提示的问题后，重新生成命令重试。</p><p>只需总管理端部署数据库和控制台。目标主机使用单个普通用户 Agent，代理实例按需启动。暂不自动接管其他面板或已有代理配置。</p></details>
 </div>
 <div class="modal-footer"><button v-if="command" class="text-button" :disabled="busy" @click="revoke">撤销这条命令</button><button class="secondary" :disabled="busy" @click="generate">{{busy?'请稍候…':existing?'生成安装命令（更换或重装）':state==='connected'?'生成重新接入命令':'重新生成命令'}}</button><button class="primary" @click="emit('close')">{{state==='connected'?'完成':'关闭'}}</button></div>
</section></div>
</template>


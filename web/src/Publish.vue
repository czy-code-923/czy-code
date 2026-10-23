<script setup lang="ts">
import Topology from './Topology.vue'
import {ref,onMounted,onUnmounted,watch,nextTick} from 'vue'
type Row=Record<string,any>
const props=defineProps<{api:(path:string,method?:string,body?:any)=>Promise<any>,hosts:Row[]}>()
const site=ref<Row>({domain:'',https:true}),services=ref<Row[]>([]),shares=ref<Row[]>([]),busy=ref(false),error=ref(''),secretURL=ref(''),notice=ref('')
const shareFormat=ref('sing-box')
function formattedShare(){if(!secretURL.value)return '';const url=new URL(secretURL.value);url.searchParams.set('format',shareFormat.value);return url.toString()}
async function copyShare(){try{await navigator.clipboard.writeText(formattedShare());notice.value='已复制所选客户端的订阅链接'}catch{error.value='复制失败，请选中链接手动复制'}}
const subscription=ref({name:'',days:30,service_ids:[] as string[]})
let alive=true
onUnmounted(()=>{alive=false;secretURL.value='';clearTimeout(timer)})
async function action(f:()=>Promise<void>){if(busy.value)return;busy.value=true;error.value='';notice.value='';try{await f()}catch(e){if(alive)error.value=String(e)}finally{if(alive)busy.value=false}}
async function reload(){const [a,b,c]=await Promise.all([props.api('site'),props.api('live'),props.api('subscriptions')]);if(!alive)return;site.value=a;services.value=b;shares.value=c}
async function refreshRows(){if(document.visibilityState==='hidden'||busy.value)return;await action(async()=>{const [b,c]=await Promise.all([props.api('live'),props.api('subscriptions')]);if(alive){services.value=b;shares.value=c}})}
let timer:ReturnType<typeof setTimeout>
function schedule(){timer=setTimeout(async()=>{await refreshRows();if(alive)schedule()},15000)}
onMounted(()=>{void action(reload);schedule()})
function saveSite(){void action(async()=>{await props.api('site','PUT',{domain:site.value.domain,https:site.value.https});notice.value='域名设置已保存。已安装域名同步器时会自动申请证书并更新网关；否则需运行部署文档中的域名配置命令。';site.value=await props.api('site')})}
const confirm=ref<Row|null>(null),confirmDialog=ref<HTMLElement|null>(null)
let previousFocus:HTMLElement|null=null
watch(confirm,async value=>{if(value){previousFocus=document.activeElement as HTMLElement;await nextTick();confirmDialog.value?.querySelector<HTMLButtonElement>('button')?.focus()}else previousFocus?.focus()})
function toggle(row:Row){confirm.value={...row,operation:'toggle'}}
function replaceServices(row:Row){confirm.value={...row,operation:'services',service_ids:[...subscription.value.service_ids]}}
function revoke(row:Row){confirm.value={...row,operation:'revoke'}}
function confirmed(){const row=confirm.value;if(!row)return;confirm.value=null;void action(async()=>{if(row.operation==='toggle')await props.api('live/'+row.id+'/toggle','POST',{enabled:!row.enabled});else if(row.operation==='services')await props.api('subscriptions/'+row.id+'/services','PUT',{service_ids:row.service_ids});else await props.api('subscriptions/'+row.id+'/revoke','POST',{});await reload()})}
function createShare(){void action(async()=>{const v=await props.api('subscriptions','POST',subscription.value);secretURL.value=v.url;await reload()})}
async function rotate(row:Row){await action(async()=>{const v=await props.api('subscriptions/'+row.id+'/rotate','POST',{});secretURL.value=v.url;await reload()})}
function hostName(id:string){return props.hosts.find(h=>h.id===id)?.name??id.slice(0,8)}
</script>
<template>
<div class="publish-grid">
<div class="publish-panels" :inert="!!confirm">
 <div v-if="error" class="error" role="alert">{{error}}</div><div v-if="notice" class="form-note" role="status">{{notice}}</div>
 <form class="panel publish-card" @submit.prevent="saveSite"><div class="section-heading"><div><span class="eyebrow">DOMAIN / ACCESS</span><h2>域名与 HTTPS</h2></div><span :class="['status',site.gateway_state==='active'&&site.gateway_domain===site.domain&&site.gateway_https===site.https?'succeeded':'pending']">{{site.gateway_state==='active'&&site.gateway_domain===site.domain&&site.gateway_https===site.https?'网关已回报生效':'网关待同步 / 验证'}}</span></div><p class="muted">订阅链接按这里的域名生成。修改域名后仍需更新 DNS、网关和证书；旧域名可在迁移期保留反向代理。</p><div class="fields"><label>分享域名<input v-model="site.domain" required placeholder="vps.example.com" maxlength="253"></label><label>分享协议<select v-model="site.https"><option :value="true">HTTPS（推荐）</option><option :value="false">HTTP（明文，包含代理凭据）</option></select></label></div><p v-if="!site.https" class="error">HTTP 分享会泄露订阅令牌及代理密码。仅在可信网络临时使用。</p><div class="actions"><button class="primary" :disabled="busy">保存域名设置</button><button class="secondary" type="button" :disabled="busy" @click="action(reload)">刷新状态</button></div></form>
 <Topology :api="api" :hosts="hosts" @changed="action(reload)"/>
 <section class="panel publish-card"><div class="section-heading"><div><span class="eyebrow">SUBSCRIPTIONS / SHARE</span><h2>共享入口订阅</h2><p class="muted">此处为独立共享密钥入口；按用户授权的入站请在“用户与用户组 → 用户订阅”生成专属链接。</p></div></div><form @submit.prevent="createShare"><div class="fields"><label>订阅名称<input v-model="subscription.name" required maxlength="100"></label><label>有效天数<input v-model.number="subscription.days" type="number" min="1" max="365" required></label></div><div class="service-choices"><label v-for="s in services.filter(s=>!s.managed_users&&!['frps','frpc'].includes(s.kind))" :key="s.id" class="check-label"><input v-model="subscription.service_ids" type="checkbox" :value="s.id">{{s.name}} · {{s.address}}</label></div><button class="primary" :disabled="busy||!subscription.service_ids.length">创建分享链接</button></form>
 <div v-if="secretURL" class="credential" role="status"><strong>一次性显示，请妥善保存</strong><label>客户端格式<select v-model="shareFormat" aria-label="订阅客户端格式"><option value="xray">Xray JSON</option><option value="sing-box">sing-box · JSON 配置</option><option value="mihomo">Mihomo / Clash Meta · JSON（兼容 YAML）</option><option value="shadowrocket">Shadowrocket · Base64 URI 列表</option></select></label><input :value="formattedShare()" readonly aria-label="订阅分享链接" @focus="($event.target as HTMLInputElement).select()"><small>Snell / AnyTLS 支持 sing-box / Mihomo，本平台暂不提供这两类协议的 Shadowrocket URI。手动切换格式后复制对应链接；格式不会根据浏览器或 User-Agent 自动改变。</small><button class="primary" @click="copyShare">复制所选格式链接</button><button class="text-button" @click="secretURL=''">隐藏链接</button></div><p class="muted">勾选上方节点后可用“替换为所选节点”更新已有订阅，链接和到期时间不变。撤销或轮换链接只阻止后续下载。已导入客户端的代理密码仍有效；泄露后需停用该服务，并创建新服务替换凭据。</p><div class="table-scroll"><table><thead><tr><th>订阅</th><th>到期时间</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="s in shares" :key="s.id"><td>{{s.name}}</td><td>{{new Date(s.expires_at).toLocaleString()}}</td><td>{{s.revoked_at?'已撤销':Date.parse(s.expires_at)<Date.now()?'已过期':'有效'}}</td><td><button v-if="!s.revoked_at&&!s.user_id" class="text-button" :disabled="busy||!subscription.service_ids.length" @click="replaceServices(s)">替换为所选节点</button><button v-if="!s.revoked_at" class="text-button" :disabled="busy" @click="rotate(s)">轮换链接</button><button v-if="!s.revoked_at" class="text-button" :disabled="busy" @click="revoke(s)">撤销</button></td></tr></tbody></table></div></section>
 </div>
 <div v-if="confirm" class="modal-backdrop"><section ref="confirmDialog" tabindex="-1" class="modal publish-confirm" role="dialog" aria-modal="true" aria-labelledby="publish-confirm-title" @keydown.esc="confirm=null"><div class="modal-heading"><h2 id="publish-confirm-title">确认{{confirm.operation==='services'?'替换订阅节点':confirm.operation==='revoke'?'撤销订阅':confirm.enabled?'停止服务':'启动真实服务'}}</h2></div><div class="form-content"><p>{{confirm.name}}</p><p>{{confirm.operation==='services'?'订阅将替换为当前勾选的节点；链接和到期时间不变。':confirm.operation==='revoke'?'已有客户端中的代理凭据不会被删除。':confirm.enabled?'停止后连接会中断，Agent 将在下一次同步执行。':'将使用固定引擎启动网络服务。请确认监听地址、端口和防火墙；公网监听会提供对外入口。'}}</p></div><div class="modal-footer"><button class="secondary" @click="confirm=null">取消</button><button class="primary" @click="confirmed">确认执行</button></div></section></div>
</div>
</template>

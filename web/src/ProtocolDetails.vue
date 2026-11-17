<script setup lang="ts">
import {computed,watch} from 'vue'
import {applyTransportRecommendations} from './transportOptions'
const props=defineProps<{form:Record<string,any>,editing?:boolean}>()
const o=computed(()=>props.form.transport_options)
const fingerprint=computed(()=>props.form.tls_mode==='tls'&&['vless','vmess','trojan','anytls','http'].includes(props.form.kind))
// Opening an existing node never rewrites its saved handshake or transport.
watch(()=>[props.form,props.form.engine,props.form.kind,props.form.transport,props.form.tls_mode],(value,previous)=>{if(value[0]===previous[0])applyTransportRecommendations(props.form)})
</script>
<template>
<fieldset v-if="fingerprint||form.engine==='xray'&&['xhttp','grpc','kcp'].includes(form.transport)" class="protocol-tuning">
 <legend>握手与封装参数</legend>
 <div class="recommendation-row"><p class="muted">{{editing?'已保留当前配置，可按需应用推荐。':'已按当前协议选择推荐参数，可直接保存。'}}推荐兼顾兼容性与常见网络；速度仍取决于线路。</p><button class="secondary" type="button" @click="applyTransportRecommendations(form)">应用推荐参数</button></div>
 <div v-if="fingerprint" class="fields"><label>客户端浏览器指纹（uTLS）<select v-model="o.tls_fingerprint" aria-label="TLS 浏览器指纹"><option value="">沿用客户端 / 内核默认值</option><option v-for="fp in ['chrome','firefox','safari','edge','ios','android','randomized']" :key="fp" :value="fp">{{fp}}</option></select></label></div>
 <p v-if="fingerprint" class="form-note">推荐 Chrome：常用且适配范围广。仅作用于客户端 TLS 握手，证书验证保持开启；REALITY 使用专用指纹。浏览器指纹不等同于真实浏览器网络行为。</p>
 <p v-if="fingerprint&&form.kind==='http'" class="form-note">Mihomo 等未实现 HTTP 浏览器指纹的客户端会自动使用其原生 TLS；节点、认证和证书验证保留。</p>
 <template v-if="form.engine==='xray'&&form.transport==='xhttp'">
  <div class="fields"><label>XHTTP 模式<select v-model="o.xhttp_mode" aria-label="XHTTP 模式" @change="o.xhttp_mode==='packet-up'&&(o.xhttp_no_grpc_header=false)"><option value="">沿用内核默认值</option><option value="auto">auto · 自动协商</option><option value="packet-up">packet-up · 分包上行</option><option value="stream-up">stream-up · 流式上行</option><option value="stream-one">stream-one · 单流双向</option></select></label><label>填充字节数 / 范围<input v-model="o.xhttp_padding_bytes" aria-label="XHTTP 填充字节" placeholder="留空默认；例如 100-1000" maxlength="11"></label></div>
  <label class="check-label"><input v-model="o.xhttp_no_grpc_header" type="checkbox" :disabled="o.xhttp_mode==='packet-up'">客户端取消 gRPC Content-Type 伪装（流式模式）</label>
  <p class="form-note">推荐 auto + 100–1000 字节填充，由内核选择模式，保留 gRPC 伪装头。路径和 Host 必须匹配；CDN 仍需支持对应行为。XHTTP 当前使用完整 Xray JSON，不支持此传输的客户端自动过滤该节点。</p>
 </template>
 <template v-if="form.engine==='xray'&&form.transport==='grpc'"><label class="check-label"><input v-model="o.grpc_multi_mode" type="checkbox" aria-label="gRPC multiMode">客户端使用 gRPC multiMode</label><p class="form-note">推荐关闭，兼容更多客户端。开启后完整 Xray JSON 保留 multiMode，其他客户端自动使用同一服务端支持的普通 gRPC。</p></template>
 <template v-if="form.engine==='xray'&&form.transport==='kcp'">
  <div class="fields"><label>mKCP Seed<input v-model="o.kcp_seed" aria-label="mKCP Seed" maxlength="128" placeholder="可选，客户端与服务端一致"></label><label>mKCP 数据包头<select v-model="o.kcp_header" aria-label="mKCP 数据包头"><option value="">沿用现有 none</option><option v-for="v in ['none','srtp','utp','wechat-video','dtls','wireguard']" :key="v" :value="v">{{v}}</option></select></label><label>MTU（字节，0 为默认）<input v-model.number="o.kcp_mtu" aria-label="mKCP MTU" type="number" min="0" max="1460"></label><label>TTI（毫秒，0 为默认）<input v-model.number="o.kcp_tti" aria-label="mKCP TTI" type="number" min="0" max="100"></label><label>上行容量（MB/s，0 为默认）<input v-model.number="o.kcp_uplink_capacity" aria-label="mKCP 上行容量" type="number" min="0" max="10000"></label><label>下行容量（MB/s，0 为默认）<input v-model.number="o.kcp_downlink_capacity" aria-label="mKCP 下行容量" type="number" min="0" max="10000"></label></div>
  <label class="check-label"><input v-model="o.kcp_congestion" type="checkbox">启用 mKCP 拥塞控制</label><p class="form-note">推荐内核基础值：none 包头、MTU 1350、TTI 50 ms、上行 5 / 下行 20 MB/s，Seed 留空、拥塞控制关闭。避免未测线路直接增大带宽。mKCP 使用 UDP；仅向支持它的客户端导出。</p>
 </template>
 <p v-if="form.engine==='xray'&&['xhttp','kcp'].includes(form.transport)" class="muted">启用新的封装参数需 Agent 0.25.7+；仅修改普通 TLS 客户端指纹无需升级入口 Agent。引用这些参数的链式入口需同步升级。</p>
</fieldset>
<p v-if="form.kind==='naive'" class="form-note">NaiveProxy 使用 Chromium 网络栈提供浏览器 TLS / HTTP2 行为，无需选择 uTLS 指纹。本适配为 TCP + TLS + HTTP/2；需匹配域名证书，导出 NaiveProxy 专用 JSON。不能作为当前内核的链式下一跳。</p>
<p v-if="['hysteria','hysteria2','tuic'].includes(form.kind)" class="form-note">此协议使用 QUIC 自身的 TLS 握手，不套用 TCP 浏览器指纹。请配置证书、SNI 及对应协议的带宽 / 拥塞参数。</p>
</template>
<style scoped src="./protocolSection.css"></style>

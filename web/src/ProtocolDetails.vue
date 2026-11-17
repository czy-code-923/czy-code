<script setup lang="ts">
import {computed} from 'vue'
const props=defineProps<{form:Record<string,any>}>()
const o=computed(()=>props.form.transport_options)
const fingerprint=computed(()=>props.form.tls_mode==='tls'&&['vless','vmess','trojan','anytls','http'].includes(props.form.kind))
</script>
<template>
<fieldset v-if="fingerprint||form.engine==='xray'&&['xhttp','grpc','kcp'].includes(form.transport)" class="protocol-tuning">
 <legend>握手与封装参数</legend>
 <div v-if="fingerprint" class="fields"><label>客户端浏览器指纹（uTLS）<select v-model="o.tls_fingerprint" aria-label="TLS 浏览器指纹"><option value="">沿用客户端 / 内核默认值</option><option v-for="fp in ['chrome','firefox','safari','edge','ios','android','randomized']" :key="fp" :value="fp">{{fp}}</option></select></label></div>
 <p v-if="fingerprint" class="form-note">指纹只控制客户端和链式出站的 TLS 握手，随订阅导出；证书验证保持开启。REALITY 使用其专用指纹。浏览器指纹并不等同于真实浏览器网络行为。</p>
 <p v-if="fingerprint&&form.kind==='http'" class="form-note">HTTP CONNECT 的浏览器指纹仅支持 sing-box / Xray JSON；Mihomo HTTP 未实现该选项，选择指纹后会明确提示格式不兼容。</p>
 <template v-if="form.engine==='xray'&&form.transport==='xhttp'">
  <div class="fields"><label>XHTTP 模式<select v-model="o.xhttp_mode" aria-label="XHTTP 模式" @change="o.xhttp_mode==='packet-up'&&(o.xhttp_no_grpc_header=false)"><option value="">沿用内核默认值</option><option value="auto">auto · 自动协商</option><option value="packet-up">packet-up · 分包上行</option><option value="stream-up">stream-up · 流式上行</option><option value="stream-one">stream-one · 单流双向</option></select></label><label>填充字节数 / 范围<input v-model="o.xhttp_padding_bytes" aria-label="XHTTP 填充字节" placeholder="留空默认；例如 100-1000" maxlength="11"></label></div>
  <label class="check-label"><input v-model="o.xhttp_no_grpc_header" type="checkbox" :disabled="o.xhttp_mode==='packet-up'">客户端取消 gRPC Content-Type 伪装（流式模式）</label>
  <p class="form-note">客户端与服务端共用所选模式、路径和 Host。CDN / 反代需支持相应流式行为；不确定时保留 auto。当前导出使用完整 Xray JSON，避免订阅格式丢失 XHTTP 参数。</p>
 </template>
 <template v-if="form.engine==='xray'&&form.transport==='grpc'"><label class="check-label"><input v-model="o.grpc_multi_mode" type="checkbox" aria-label="gRPC multiMode">客户端使用 gRPC multiMode</label><p class="form-note">启用后需使用 Xray JSON 客户端；服务端支持普通与 multi 模式。其他订阅格式不会静默丢弃此选项。</p></template>
 <template v-if="form.engine==='xray'&&form.transport==='kcp'">
  <div class="fields"><label>mKCP Seed<input v-model="o.kcp_seed" aria-label="mKCP Seed" maxlength="128" placeholder="可选，客户端与服务端一致"></label><label>mKCP 数据包头<select v-model="o.kcp_header" aria-label="mKCP 数据包头"><option value="">沿用现有 none</option><option v-for="v in ['none','srtp','utp','wechat-video','dtls','wireguard']" :key="v" :value="v">{{v}}</option></select></label><label>MTU（字节，0 为默认）<input v-model.number="o.kcp_mtu" aria-label="mKCP MTU" type="number" min="0" max="1460"></label><label>TTI（毫秒，0 为默认）<input v-model.number="o.kcp_tti" aria-label="mKCP TTI" type="number" min="0" max="100"></label><label>上行容量（MB/s，0 为默认）<input v-model.number="o.kcp_uplink_capacity" aria-label="mKCP 上行容量" type="number" min="0" max="10000"></label><label>下行容量（MB/s，0 为默认）<input v-model.number="o.kcp_downlink_capacity" aria-label="mKCP 下行容量" type="number" min="0" max="10000"></label></div>
  <label class="check-label"><input v-model="o.kcp_congestion" type="checkbox">启用 mKCP 拥塞控制</label><p class="form-note">mKCP 使用 UDP 端口；MTU 自定义范围 576–1460，TTI 为 10–100。容量单位是 MB/s。Seed / 数据包头是传输参数；本组合使用 VMess 认证加密。</p>
 </template>
 <p v-if="form.engine==='xray'&&['xhttp','kcp'].includes(form.transport)" class="muted">启用新的封装参数需 Agent 0.25.7+；仅修改普通 TLS 客户端指纹无需升级入口 Agent。引用这些参数的链式入口需同步升级。</p>
</fieldset>
<p v-if="form.kind==='naive'" class="form-note">NaiveProxy 使用 Chromium 网络栈提供浏览器 TLS / HTTP2 行为，无需选择 uTLS 指纹。本适配为 TCP + TLS + HTTP/2；需匹配域名证书，导出 NaiveProxy 专用 JSON。不能作为当前内核的链式下一跳。</p>
<p v-if="['hysteria','hysteria2','tuic'].includes(form.kind)" class="form-note">此协议使用 QUIC 自身的 TLS 握手，不套用 TCP 浏览器指纹。请配置证书、SNI 及对应协议的带宽 / 拥塞参数。</p>
</template>

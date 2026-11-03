<script setup lang="ts">
const props=defineProps<{form:Record<string,any>,editing?:boolean}>()
function bandwidthMode(){if(props.form.hy2_client_mode!=='brutal'){props.form.hy2_client_up_mbps=0;props.form.hy2_client_down_mbps=0}}
</script>
<template>
<fieldset v-if="form.kind==='hysteria2'" class="protocol-tuning">
 <legend>HY2 · 带宽与拥塞控制</legend>
 <p class="form-note">带宽参数不是必填，也不是越大越快。默认客户端使用 BBR 自动估计；线路丢包、CPU 和运营商限速仍可能限制速度。1 Mbps = 0.125 MB/s。</p>
 <h4>客户端 · 写入订阅出站</h4>
 <div class="fields">
  <label>客户端带宽模式<select v-model="form.hy2_client_mode" aria-label="HY2 客户端带宽模式" @change="bandwidthMode"><option value="">保留旧版 · 与服务端共用数值</option><option value="bbr">BBR 自动估计（新建默认）</option><option value="brutal">Brutal · 手动填写实测带宽</option></select></label>
  <label>客户端 BBR 配置<select v-model="form.hy2_bbr_profile" aria-label="HY2 客户端 BBR 配置"><option value="">内核默认 standard</option><option value="standard">standard</option><option value="conservative">conservative</option><option value="aggressive">aggressive</option></select></label>
  <template v-if="form.hy2_client_mode==='brutal'"><label>客户端上传 Mbps<input v-model.number="form.hy2_client_up_mbps" type="number" min="0" max="100000" aria-label="HY2 客户端上传 Mbps"><small>客户端 → VPS；0 使用该方向自动估计。</small></label><label>客户端下载 Mbps<input v-model.number="form.hy2_client_down_mbps" type="number" min="0" max="100000" aria-label="HY2 客户端下载 Mbps"><small>VPS → 客户端；至少一个方向填写正值。</small></label></template>
 </div>
 <p v-if="form.hy2_client_mode==='brutal'" class="form-note">按客户端到该 VPS 的实际可用带宽填写，移动网络建议先用自动模式。填低会限制速率，虚高可能增加丢包、延迟和流量消耗；两端不需要填写同一组数值。</p>
 <p v-if="!form.hy2_client_mode" class="form-note">此选项保留旧节点的下发行为，升级不会自动改动已有带宽。</p>
 <details><summary>服务端 · Brutal 协商上限与混淆</summary><div class="fields">
  <label>VPS 发出上限 up_mbps<input v-model.number="form.up_mbps" type="number" min="0" max="100000" aria-label="HY2 服务端发出 Mbps"><small>VPS → 客户端，0 不设置协商上限。</small></label>
  <label>VPS 接收上限 down_mbps<input v-model.number="form.down_mbps" type="number" min="0" max="100000" aria-label="HY2 服务端接收 Mbps"><small>客户端 → VPS，0 不设置协商上限。</small></label>
  <label>Salamander 混淆密码<input v-model="form.hy2_obfs_password" type="password" autocomplete="off" :placeholder="editing?'留空保留已有密码':'留空关闭混淆'"></label>
  <label v-if="editing" class="check-label"><input v-model="form.clear_hy2_obfs" type="checkbox">关闭已有混淆</label>
 </div><p class="muted">服务端数值是 Brutal 的协商上限，不是所有用户共享的全局限速，也不会限制 BBR。自用可保持 0，仅在客户端指定实测带宽。密码会同步到客户端，混淆并非提速开关。</p></details>
 <p class="muted">Mihomo / sing-box 完整订阅会同步客户端参数；纯 URI 格式不能可靠携带全部调优选项，请在对应客户端手动设置。TCP BBR 系统设置不等同于 QUIC 的 BBR；不自动修改系统缓冲区和连接窗口，避免额外内存占用。</p>
</fieldset>
<fieldset v-if="form.kind==='tuic'" class="protocol-tuning"><legend>TUIC v5 · 专属设置</legend><div class="fields">
 <label>两端拥塞控制<select v-model="form.congestion" aria-label="TUIC 拥塞控制"><option value="">内核默认 cubic</option><option>cubic</option><option>bbr</option><option>new_reno</option></select></label>
 <label>客户端 UDP 转发<select v-model="form.tuic_udp_relay_mode" aria-label="TUIC UDP 转发模式"><option value="">客户端默认 native</option><option value="native">native · QUIC 数据报</option><option value="quic">quic · QUIC 流</option></select></label>
 <label>客户端心跳（秒）<input v-model.number="form.tuic_heartbeat_seconds" type="number" min="0" max="300" aria-label="TUIC 心跳秒数"><small>0 保留客户端默认，通常为 10 秒。</small></label>
</div><p class="muted">TUIC 没有 HY2 的 up_mbps / down_mbps 参数；心跳和转发方式不会自动提升线路带宽。QUIC + TLS / h3，保持原有 0-RTT 关闭策略。客户端专属值写入完整订阅。</p></fieldset>
<fieldset v-if="form.kind==='anytls'" class="protocol-tuning"><legend>AnyTLS · 客户端会话复用</legend><div class="fields">
 <label>空闲检查间隔（秒）<input v-model.number="form.anytls_idle_check_seconds" type="number" min="0" max="3600" aria-label="AnyTLS 空闲检查间隔"></label>
 <label>空闲会话超时（秒）<input v-model.number="form.anytls_idle_timeout_seconds" type="number" min="0" max="86400" aria-label="AnyTLS 空闲会话超时"></label>
 <label>最少保留空闲会话<input v-model.number="form.anytls_min_idle_sessions" type="number" min="0" max="32" aria-label="AnyTLS 最少空闲会话"></label>
</div><p class="muted">全部为 0 保留内核默认：检查 / 超时通常为 30 秒，保留 0 个。增加空闲会话可减少建连等待，也会占用连接与内存。只写入 Mihomo / sing-box 客户端，不更改服务端 padding。</p></fieldset>
<fieldset v-if="form.kind==='hysteria'" class="protocol-tuning"><legend>Hysteria v1 · 必填带宽</legend><div class="fields"><label>up_mbps（Mbps，必填）<input v-model.number="form.up_mbps" type="number" min="1" max="100000" required></label><label>down_mbps（Mbps，必填）<input v-model.number="form.down_mbps" type="number" min="1" max="100000" required></label><label>混淆密码<input v-model="form.hysteria_obfs_password" type="password" autocomplete="off" placeholder="留空保留 / 新建禁用"></label><label v-if="editing" class="check-label"><input v-model="form.clear_hysteria_obfs" type="checkbox">关闭已有混淆</label></div><p class="muted">v1 两个带宽值必填，现有适配共用于两端；请按实际线路设定。HY2 支持自动 BBR 与两端独立设置。批量创建不再默填 100 Mbps。</p></fieldset>
 <div v-if="form.kind==='shadowsocks'" class="fields"><label>加密方式<select v-model="form.cipher"><option value="">2022-blake3-aes-256-gcm（默认）</option><option value="2022-blake3-aes-128-gcm">2022-blake3-aes-128-gcm</option><option value="2022-blake3-chacha20-poly1305">2022-blake3-chacha20-poly1305</option><option value="aes-128-gcm">aes-128-gcm</option><option value="aes-256-gcm">aes-256-gcm</option><option value="chacha20-ietf-poly1305">chacha20-ietf-poly1305</option></select></label></div>
 <div v-if="form.kind==='snell'" class="fields"><label>Snell 模式<select v-model.number="form.snell_version" @change="form.snell_obfs='';form.snell_host='';form.snell_mode=''"><option :value="0">v4 兼容（默认）</option><option :value="5">v4 兼容 · 自定义混淆</option><option :value="6">v6 · sing-box 客户端</option></select></label><template v-if="form.snell_version!==6"><label>混淆<select v-model="form.snell_obfs" @change="form.snell_host=''"><option value="">无</option><option value="http">HTTP</option></select></label><label v-if="form.snell_obfs==='http'">混淆 Host<input v-model="form.snell_host" placeholder="bing.com（默认）"></label></template><label v-else>流量整形<select v-model="form.snell_mode"><option value="">默认加密整形</option><option value="unshaped">仅加密，不整形</option></select></label></div>

</template>
<style scoped>.protocol-tuning{padding:18px;margin:18px 0;border:1px solid var(--line);border-radius:12px;background:var(--panel-soft)}legend{padding:0 8px;color:var(--accent);font-size:13px}h4{margin:20px 0 12px}small{display:block;margin-top:7px;color:var(--muted);font-weight:400;line-height:1.6}p{line-height:1.7}details{margin-top:18px}@media(max-width:600px){.protocol-tuning{padding:12px}}</style>

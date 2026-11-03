// @vitest-environment happy-dom
import {it,expect} from 'vitest'
import {mount} from '@vue/test-utils'
import {reactive} from 'vue'
import ProtocolTuning from './ProtocolTuning.vue'
import {protocolTuningDefaults} from './protocolTuning'
it('separates server and client bandwidth and clears stale fixed rates when selecting BBR',async()=>{
 const form=reactive({...protocolTuningDefaults('hysteria2'),kind:'hysteria2',up_mbps:900,down_mbps:200})
 const w=mount(ProtocolTuning,{props:{form}})
 expect(form.hy2_client_mode).toBe('bbr');expect(w.text()).toContain('VPS 发出上限')
 await w.get('[aria-label="HY2 客户端带宽模式"]').setValue('brutal')
 await w.get('[aria-label="HY2 客户端上传 Mbps"]').setValue('40');await w.get('[aria-label="HY2 客户端下载 Mbps"]').setValue('500')
 expect(form.up_mbps).toBe(900);expect(form.down_mbps).toBe(200);expect(form.hy2_client_down_mbps).toBe(500)
 await w.get('[aria-label="HY2 客户端带宽模式"]').setValue('bbr');expect(form.hy2_client_up_mbps).toBe(0);expect(form.hy2_client_down_mbps).toBe(0);expect(form.up_mbps).toBe(900);w.unmount()
})
it('retains legacy edit behavior and exposes bounded TUIC and AnyTLS client options',()=>{
 const w=mount(ProtocolTuning,{props:{form:{...protocolTuningDefaults('hysteria2'),kind:'hysteria2',hy2_client_mode:''},editing:true}})
 expect(w.text()).toContain('保留旧节点的下发行为');w.unmount()
 for(const [kind,label] of [['tuic','TUIC 心跳秒数'],['anytls','AnyTLS 最少空闲会话']]){
  const v=mount(ProtocolTuning,{props:{form:{...protocolTuningDefaults(kind),kind}}});expect(v.find(`[aria-label="${label}"]`).exists()).toBe(true);v.unmount()
 }
 expect(protocolTuningDefaults('hysteria').up_mbps).toBe(0)
})

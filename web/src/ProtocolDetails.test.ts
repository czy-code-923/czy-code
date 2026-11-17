// @vitest-environment happy-dom
import {it,expect} from 'vitest'
import {mount} from '@vue/test-utils'
import {reactive} from 'vue'
import ProtocolDetails from './ProtocolDetails.vue'
import {transportDefaults,clearIncompatibleTransportOptions,recommendedTransportOptions} from './transportOptions'

it('edits TLS fingerprints and only exposes options that the protocol can use',async()=>{
 const form=reactive({kind:'anytls',engine:'sing-box',transport:'tcp',tls_mode:'tls',transport_options:transportDefaults()})
 const w=mount(ProtocolDetails,{props:{form}})
 await w.get('[aria-label="TLS 浏览器指纹"]').setValue('firefox')
 expect(form.transport_options.tls_fingerprint).toBe('firefox')
 expect(w.find('[aria-label="XHTTP 模式"]').exists()).toBe(false)
 form.kind='hysteria2';form.transport='quic';clearIncompatibleTransportOptions(form);await w.vm.$nextTick()
 expect(w.find('[aria-label="TLS 浏览器指纹"]').exists()).toBe(false)
 expect(form.transport_options.tls_fingerprint).toBe('');expect(w.text()).toContain('QUIC')
 form.kind='naive';await w.vm.$nextTick();expect(w.text()).toContain('Chromium');w.unmount()
})
it('keeps XHTTP mode editable and clears an incompatible streaming-only flag',async()=>{
 const form=reactive({kind:'vmess',engine:'xray',transport:'xhttp',tls_mode:'tls',transport_options:{...transportDefaults(),xhttp_mode:'stream-up',xhttp_no_grpc_header:true}})
 const w=mount(ProtocolDetails,{props:{form}})
 await w.get('[aria-label="XHTTP 模式"]').setValue('packet-up')
 expect(form.transport_options.xhttp_no_grpc_header).toBe(false)
 await w.get('[aria-label="XHTTP 填充字节"]').setValue('100-1000')
 expect(form.transport_options.xhttp_padding_bytes).toBe('100-1000')
 form.transport='ws';clearIncompatibleTransportOptions(form);await w.vm.$nextTick()
 expect(form.transport_options.xhttp_mode).toBe('');expect(w.find('[aria-label="XHTTP 模式"]').exists()).toBe(false)
 expect(form.transport_options.tls_fingerprint).toBe('chrome');w.unmount()
})
it('selects protocol recommendations while preserving existing nodes and JSON imports',async()=>{
 const form=reactive({kind:'vmess',engine:'xray',transport:'xhttp',tls_mode:'tls',transport_options:{...transportDefaults(),xhttp_mode:'stream-up',xhttp_padding_bytes:'200-400',tls_fingerprint:'firefox'}})
 const w=mount(ProtocolDetails,{props:{form,editing:true}})
 expect(form.transport_options.xhttp_mode).toBe('stream-up');expect(form.transport_options.tls_fingerprint).toBe('firefox')
 await w.get('button').trigger('click');expect(form.transport_options.xhttp_mode).toBe('auto');expect(form.transport_options.tls_fingerprint).toBe('chrome')
 const imported=reactive({...form,transport_options:{...transportDefaults(),xhttp_mode:'stream-one',xhttp_padding_bytes:'50-100'}})
 await w.setProps({form:imported});expect(imported.transport_options.xhttp_mode).toBe('stream-one')
 imported.transport='grpc';await w.vm.$nextTick();expect(imported.transport_options.grpc_multi_mode).toBe(false);expect(imported.transport_options.xhttp_mode).toBe('')
 expect(recommendedTransportOptions({kind:'vmess',engine:'xray',transport:'kcp',tls_mode:'none'})).toMatchObject({kcp_header:'none',kcp_mtu:1350,kcp_tti:50,kcp_uplink_capacity:5,kcp_downlink_capacity:20,kcp_congestion:false,tls_fingerprint:''})
 w.unmount()
})

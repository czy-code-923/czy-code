// @vitest-environment happy-dom
import {expect,it} from 'vitest'
import {mount} from '@vue/test-utils'
import ProtocolLayers from './ProtocolLayers.vue'
it('separates native SS listener networks from framing and preserves the selection',async()=>{
 const form={kind:'shadowsocks',engine:'sing-box',transport:'tcp',tls_mode:'none',listen_network:''}
 const wrapper=mount(ProtocolLayers,{props:{form,template:{layers:{'sing-box':[{transport:'tcp',security_modes:['none']}]},network_note:'SS 支持 TCP / UDP'}}})
 expect(wrapper.text()).toContain('不是仅 TCP')
 await wrapper.get('[aria-label="Shadowsocks 原生监听网络"]').setValue('udp')
 expect(form.listen_network).toBe('udp');expect(form.transport).toBe('tcp');expect(form.tls_mode).toBe('none')
 wrapper.unmount()
})

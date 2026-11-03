// @vitest-environment happy-dom
import {it,expect} from 'vitest'
import {mount} from '@vue/test-utils'
import PortHopping from './PortHopping.vue'
it('enables and clears explicit hopping options only for supported protocols',async()=>{
 const form={kind:'hysteria2',engine:'sing-box',hop_ports:'',hop_interval_seconds:0}
 const w=mount(PortHopping,{props:{form}})
 await w.get('[aria-label="启用端口跳跃"]').setValue(true)
 expect(form.hop_ports).toBe('30000-30100');expect(form.hop_interval_seconds).toBe(30)
 await w.get('[aria-label="跳跃端口范围"]').setValue('31000-31100,31200')
 await w.get('[aria-label="跳跃间隔"]').setValue(20)
 expect(form.hop_ports).toBe('31000-31100,31200');expect(form.hop_interval_seconds).toBe(20)
 await w.get('[aria-label="启用端口跳跃"]').setValue(false)
 expect(form.hop_ports).toBe('');expect(form.hop_interval_seconds).toBe(0)
 await w.setProps({form:{kind:'tuic',engine:'sing-box'}});expect(w.find('fieldset').exists()).toBe(false);w.unmount()
})

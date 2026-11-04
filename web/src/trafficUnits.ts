import {inject,ref,type InjectionKey,type Ref} from 'vue'

export type TrafficUnit='GB'|'GiB'|'auto'
export const trafficUnitKey:InjectionKey<Ref<TrafficUnit>>=Symbol('traffic-unit')
export const normalizeTrafficUnit=(v:unknown):TrafficUnit=>v==='GiB'||v==='auto'?v:'GB'
export const quotaUnitSize=(unit:string)=>unit==='GiB'?1073741824:1e9
export function quotaDraft(bytes:unknown,unit:unknown){const quota_unit=unit==='GiB'?'GiB':'GB',source=Number(bytes??0),amount=source/quotaUnitSize(quota_unit);return {quota_unit,quota_amount:amount,quota_amount_source:amount,quota_bytes_source:source}}
export function quotaToBytes(d:Record<string,any>){return Number(d.quota_amount)===d.quota_amount_source?d.quota_bytes_source:Math.round(Number(d.quota_amount??0)*quotaUnitSize(d.quota_unit))}
export function changeQuotaUnit(d:Record<string,any>,unit:string){Object.assign(d,quotaDraft(quotaToBytes(d),unit))}
export function trafficBytes(value:unknown,unit:TrafficUnit='GB'){
 const parsed=Number(value??0),n=Number.isFinite(parsed)?Math.max(0,parsed):0
 if(unit==='auto'){
  for(const [name,size] of [['PB',1e15],['TB',1e12],['GB',1e9],['MB',1e6],['KB',1e3]] as const)if(n>=size)return (n/size).toFixed(2)+' '+name
  return n+' B'
 }
 const amount=n/quotaUnitSize(unit)
 return (n>0&&amount<.01?amount.toFixed(9).replace(/0+$/,'').replace(/\.$/,''):amount.toFixed(2))+' '+unit
}
export function useTrafficFormat(){const unit=inject(trafficUnitKey,ref<TrafficUnit>('GB'));return {unit,bytes:(n:unknown)=>trafficBytes(n,unit.value)}}

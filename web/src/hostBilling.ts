export const hostBytes=(v:unknown)=>{const n=Number(v??0);for(const [unit,size] of [['TiB',1099511627776],['GiB',1073741824],['MiB',1048576],['KiB',1024]] as const){if(n>=size)return (n/size).toFixed(2)+' '+unit}return n+' B'}
export const hostUnit=(unit:unknown):'GB'|'GiB'=>unit==='GB'?'GB':'GiB'
export const hostUnitSize=(unit:unknown)=>hostUnit(unit)==='GB'?1e9:1073741824
export const hostTrafficBytes=(v:unknown,unit:unknown)=>`${(Number(v??0)/hostUnitSize(unit)).toFixed(2)} ${hostUnit(unit)}`
export const hostModes:Record<string,string>={both:'双向 RX + TX',rx:'仅入站 RX',tx:'仅出站 TX',max:'周期累计 RX / TX 取大'}
export function hostDraft(t:Record<string,any>={}){return {version:t.version??1,mode:t.mode??'both',reset_day:t.reset_day??new Date().getDate(),auto_reset:t.auto_reset??true,interfaces:[...(t.interfaces??[])],unit:hostUnit(t.display_unit),quota:Number(t.quota_bytes??0)/hostUnitSize(t.display_unit),initial_used:0}}
export function hostPayload(d:Record<string,any>,fresh=false){const unit=hostUnitSize(d.unit);return {version:d.version,quota_bytes:Math.round(Number(d.quota)*unit),display_unit:hostUnit(d.unit),mode:d.mode,reset_day:Number(d.reset_day),auto_reset:!!d.auto_reset,interfaces:d.interfaces,...(fresh?{initial_used_bytes:Math.round(Number(d.initial_used??0)*unit)}:{})}}

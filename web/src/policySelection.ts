export type PolicyNode=Record<string,any>
export const isNodeSelector=(ref:string)=>ref==='ALL'||['family:','protocol:','host:','node:','endpoint:'].some(prefix=>ref.startsWith(prefix))
export function matchesEndpoint(ref:string,node:PolicyNode){
 return ref===node.reference||ref==='node:'+node.id||ref==='ALL'||ref==='family:'+node.family||ref==='protocol:'+node.kind||ref==='host:'+node.host_id
}
export function selectedEndpoints(refs:string[],nodes:PolicyNode[]){
 return Array.from(new Set(nodes.filter(node=>!node.delete_requested_at&&!['frps','frpc'].includes(node.kind)&&node.reference&&refs.some(ref=>matchesEndpoint(ref,node))).map(node=>String(node.reference))))
}
// Keep the original default: selecting an address must not move DIRECT first.
export function replaceNodeSelectors(refs:string[],selected:string[]){
 const first=refs.findIndex(isNodeSelector)
 const offset=first<0?0:refs.slice(0,first).filter(ref=>!isNodeSelector(ref)).length
 const result=refs.filter(ref=>!isNodeSelector(ref))
 result.splice(offset,0,...Array.from(new Set(selected)))
 return result
}
export function selectionMode(refs:string[]){
 const dynamic=refs.filter(ref=>isNodeSelector(ref)&&!ref.startsWith('endpoint:')&&!ref.startsWith('node:'))
 if(!dynamic.length)return 'manual'
 if(dynamic.length===1&&!refs.some(ref=>ref.startsWith('endpoint:')||ref.startsWith('node:'))){
  if(dynamic[0]==='ALL')return 'all'
  if(dynamic[0]==='family:ipv4')return 'ipv4'
  if(dynamic[0]==='family:ipv6')return 'ipv6'
 }
 return 'custom'
}
export function toggleEndpoint(refs:string[],nodes:PolicyNode[],endpoint:string,checked:boolean){
 const selected=selectedEndpoints(refs,nodes)
 // Retain temporarily unavailable stable endpoints during an unrelated edit.
 for(const ref of refs)if(ref.startsWith('endpoint:')&&!selected.includes(ref))selected.push(ref)
 const next=checked?Array.from(new Set([...selected,endpoint])):selected.filter(ref=>ref!==endpoint)
 return replaceNodeSelectors(refs,next)
}

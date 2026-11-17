export const transportDefaults=()=>({tls_fingerprint:'',xhttp_mode:'',xhttp_padding_bytes:'',xhttp_no_grpc_header:false,grpc_multi_mode:false,kcp_seed:'',kcp_header:'',kcp_mtu:0,kcp_tti:0,kcp_uplink_capacity:0,kcp_downlink_capacity:0,kcp_congestion:false})
export function recommendedTransportOptions(form:Record<string,any>){
 const o=transportDefaults()
 if(form.tls_mode==='tls'&&['vless','vmess','trojan','anytls','http'].includes(form.kind))o.tls_fingerprint='chrome'
 if(form.engine==='xray'&&form.transport==='xhttp')Object.assign(o,{xhttp_mode:'auto',xhttp_padding_bytes:'100-1000'})
 if(form.engine==='xray'&&form.transport==='kcp')Object.assign(o,{kcp_header:'none',kcp_mtu:1350,kcp_tti:50,kcp_uplink_capacity:5,kcp_downlink_capacity:20})
 return o
}
export function applyTransportRecommendations(form:Record<string,any>){form.transport_options=recommendedTransportOptions(form)}
export function clearIncompatibleTransportOptions(form:Record<string,any>){
 const o=form.transport_options
 if(!o)return
 if(form.tls_mode!=='tls'||!['vless','vmess','trojan','anytls','http'].includes(form.kind))o.tls_fingerprint=''
 if(form.engine!=='xray'||form.transport!=='xhttp'){o.xhttp_mode='';o.xhttp_padding_bytes='';o.xhttp_no_grpc_header=false}
 if(form.engine!=='xray'||form.transport!=='grpc')o.grpc_multi_mode=false
 if(form.engine!=='xray'||form.transport!=='kcp')Object.assign(o,{kcp_seed:'',kcp_header:'',kcp_mtu:0,kcp_tti:0,kcp_uplink_capacity:0,kcp_downlink_capacity:0,kcp_congestion:false})
}

export const transportDefaults=()=>({tls_fingerprint:'',xhttp_mode:'',xhttp_padding_bytes:'',xhttp_no_grpc_header:false,grpc_multi_mode:false,kcp_seed:'',kcp_header:'',kcp_mtu:0,kcp_tti:0,kcp_uplink_capacity:0,kcp_downlink_capacity:0,kcp_congestion:false})
export function clearIncompatibleTransportOptions(form:Record<string,any>){
 const o=form.transport_options
 if(!o)return
 if(form.tls_mode!=='tls'||!['vless','vmess','trojan','anytls','http'].includes(form.kind))o.tls_fingerprint=''
 if(form.engine!=='xray'||form.transport!=='xhttp'){o.xhttp_mode='';o.xhttp_padding_bytes='';o.xhttp_no_grpc_header=false}
 if(form.engine!=='xray'||form.transport!=='grpc')o.grpc_multi_mode=false
 if(form.engine!=='xray'||form.transport!=='kcp')Object.assign(o,{kcp_seed:'',kcp_header:'',kcp_mtu:0,kcp_tti:0,kcp_uplink_capacity:0,kcp_downlink_capacity:0,kcp_congestion:false})
}

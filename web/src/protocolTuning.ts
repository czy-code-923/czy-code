export const clientTuningKeys=['hy2_client_mode','hy2_client_up_mbps','hy2_client_down_mbps','hy2_bbr_profile','tuic_udp_relay_mode','tuic_heartbeat_seconds','anytls_idle_check_seconds','anytls_idle_timeout_seconds','anytls_min_idle_sessions'] as const
export function protocolTuningDefaults(kind:string){return {
 hy2_client_mode:kind==='hysteria2'?'bbr':'',hy2_client_up_mbps:0,hy2_client_down_mbps:0,hy2_bbr_profile:'',
 tuic_udp_relay_mode:'',tuic_heartbeat_seconds:0,anytls_idle_check_seconds:0,anytls_idle_timeout_seconds:0,anytls_min_idle_sessions:0,
 cipher:'',snell_version:0,snell_obfs:'',snell_host:'',snell_mode:'',congestion:'',hysteria_obfs_password:'',hy2_obfs_password:'',up_mbps:0,down_mbps:0,
 clear_hy2_obfs:false,clear_hysteria_obfs:false
}}

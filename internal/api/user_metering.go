package api

// Shared-key entry counters are deliberately separate from personal bills.
// A subscription download or its IP cannot identify a proxy connection's user.
const userMeteringColumns = `
(SELECT count(DISTINCT l.id) FROM live_services l WHERE l.tenant_id=u.tenant_id AND l.managed_users AND l.delete_requested_at IS NULL AND EXISTS(SELECT 1 FROM user_group_services gs JOIN user_group_members gm USING(tenant_id,group_id) WHERE gs.tenant_id=l.tenant_id AND gs.service_id=l.id AND gm.user_id=u.id)) metered_nodes,
(SELECT COALESCE(jsonb_agg(x),'[]') FROM (SELECT l.id,l.name,l.kind,COALESCE(sum(c.uplink),0) upload_bytes,COALESCE(sum(c.downlink),0) download_bytes FROM live_services l LEFT JOIN traffic_cursors c ON (c.tenant_id,c.host_id,c.subject_kind,c.subject_id)=(l.tenant_id,l.host_id,'service',l.id) WHERE l.tenant_id=u.tenant_id AND NOT l.managed_users AND l.kind IN ('shadowsocks','snell') AND l.delete_requested_at IS NULL AND EXISTS(SELECT 1 FROM user_group_services gs JOIN user_group_members gm USING(tenant_id,group_id) WHERE gs.tenant_id=l.tenant_id AND gs.service_id=l.id AND gm.user_id=u.id) GROUP BY l.id,l.name,l.kind) x) shared_entries`

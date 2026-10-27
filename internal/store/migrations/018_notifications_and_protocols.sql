ALTER TABLE live_services DROP CONSTRAINT live_services_kind_check;
ALTER TABLE live_services ADD CONSTRAINT live_services_kind_check CHECK(kind IN ('shadowsocks','frps','frpc','vless','hysteria2','tuic','trojan','snell','vmess','anytls','http','socks','mixed','hysteria','naive'));
ALTER TABLE firewall_policies ADD COLUMN deleted_at timestamptz;
CREATE TABLE notification_settings (
 tenant_id text PRIMARY KEY REFERENCES tenants(id),version integer NOT NULL DEFAULT 1,
 settings jsonb NOT NULL,sealed bytea NOT NULL,offset_id bigint NOT NULL DEFAULT 0,
 next_report_at timestamptz NOT NULL DEFAULT now(),next_alert_at timestamptz NOT NULL DEFAULT now(),
 last_poll_at timestamptz,last_send_at timestamptz,last_error text NOT NULL DEFAULT '',updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE notification_state (
 tenant_id text NOT NULL REFERENCES tenants(id),resource text NOT NULL,state text NOT NULL,updated_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(tenant_id,resource)
);
CREATE TABLE notification_log (
 tenant_id text NOT NULL REFERENCES tenants(id),id bigserial PRIMARY KEY,event text NOT NULL,
 state text NOT NULL,detail text NOT NULL DEFAULT '',created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notification_log_recent ON notification_log(tenant_id,created_at DESC);

ALTER TABLE client_profiles DROP CONSTRAINT client_profiles_format_check;
ALTER TABLE client_profiles ADD CONSTRAINT client_profiles_format_check CHECK(format IN ('sing-box','mihomo','clash-verge','stash','hiddify','nekobox','sfa','xray','v2rayng','v2rayn','shadowrocket','v2rayng-uri','nekobox-uri','base64','naive-proxy'));

ALTER TABLE client_profiles DROP CONSTRAINT IF EXISTS client_profiles_format_check;
ALTER TABLE client_profiles ADD CONSTRAINT client_profiles_format_check CHECK (format IN ('sing-box','mihomo','clash-verge','stash','hiddify','nekobox','sfa','xray','v2rayng','v2rayn','shadowrocket','shadowrocket-config','v2rayng-uri','nekobox-uri','base64','naive-proxy'));
CREATE TABLE client_policy_bootstrap (tenant_id text PRIMARY KEY REFERENCES tenants(id),initialized_at timestamptz NOT NULL DEFAULT now());

ALTER TABLE site_settings ADD COLUMN ip_origin text NOT NULL DEFAULT 'http://127.0.0.1:5173';
ALTER TABLE site_settings ADD COLUMN share_domain text NOT NULL DEFAULT '';
ALTER TABLE site_settings ADD COLUMN share_https boolean NOT NULL DEFAULT true;
ALTER TABLE hosts ADD COLUMN retire_id text;
ALTER TABLE hosts ADD COLUMN retire_state text NOT NULL DEFAULT '' CHECK(retire_state IN ('','requested','cleaning','complete'));
ALTER TABLE hosts ADD COLUMN retire_requested_at timestamptz;
ALTER TABLE hosts ADD COLUMN retire_result text NOT NULL DEFAULT '';
ALTER TABLE hosts ADD COLUMN cleanup_capable boolean NOT NULL DEFAULT false;
CREATE TABLE client_policies (
 tenant_id text NOT NULL REFERENCES tenants(id),id text NOT NULL,name text NOT NULL,
 user_id text,client_id text NOT NULL DEFAULT 'all',source text NOT NULL,bindings jsonb NOT NULL DEFAULT '{}',
 native_options jsonb NOT NULL DEFAULT '{}',version integer NOT NULL DEFAULT 1,
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),FOREIGN KEY(tenant_id,user_id) REFERENCES proxy_users(tenant_id,id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX client_policies_scope ON client_policies(tenant_id,COALESCE(user_id,''),client_id);
CREATE TABLE host_retirement_receipts (
 tenant_id text NOT NULL,host_id text NOT NULL,retire_id text NOT NULL,
 result text NOT NULL,completed_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(tenant_id,host_id)
);

ALTER TABLE client_profiles DROP CONSTRAINT IF EXISTS client_profiles_format_check;
ALTER TABLE client_profiles ADD CONSTRAINT client_profiles_format_check CHECK (format IN ('sing-box','mihomo','clash-verge','stash','hiddify','nekobox','sfa','xray','v2rayng','v2rayn','shadowrocket','v2rayng-uri','nekobox-uri','base64'));

ALTER TABLE host_retirement_receipts ADD COLUMN agent_hash text NOT NULL DEFAULT '';

ALTER TABLE proxy_users ADD COLUMN reset_day integer NOT NULL DEFAULT 0 CHECK(reset_day BETWEEN 0 AND 31);

ALTER TABLE site_settings ADD COLUMN share_gateway_domain text NOT NULL DEFAULT '';
ALTER TABLE site_settings ADD COLUMN share_gateway_state text NOT NULL DEFAULT 'inherited';
ALTER TABLE site_settings ADD COLUMN share_gateway_checked_at timestamptz;

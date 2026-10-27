ALTER TABLE proxy_users DROP CONSTRAINT proxy_users_reset_strategy_check;
ALTER TABLE proxy_users ADD CONSTRAINT proxy_users_reset_strategy_check CHECK(reset_strategy IN ('never','daily','monthly','rolling_monthly'));
ALTER TABLE proxy_users ADD COLUMN reset_anchor_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE proxy_users ADD COLUMN next_reset_at timestamptz;
UPDATE proxy_users SET reset_anchor_at=created_at;
CREATE INDEX proxy_users_next_reset ON proxy_users(tenant_id,next_reset_at) WHERE reset_strategy='rolling_monthly';
ALTER TABLE client_profiles ADD COLUMN extra_config jsonb NOT NULL DEFAULT '{}';
ALTER TABLE client_profiles ADD COLUMN extra_source text NOT NULL DEFAULT '';
ALTER TABLE client_profiles ADD COLUMN version integer NOT NULL DEFAULT 1;
ALTER TABLE site_settings ADD COLUMN title text NOT NULL DEFAULT '星枢';
ALTER TABLE site_settings ADD COLUMN api_origins jsonb NOT NULL DEFAULT '[]';
ALTER TABLE subscriptions ADD COLUMN version integer NOT NULL DEFAULT 1;
CREATE TABLE api_keys (
 tenant_id text NOT NULL,id text NOT NULL,name text NOT NULL,token_hash text NOT NULL,
 scopes text[] NOT NULL,expires_at timestamptz NOT NULL,revoked_at timestamptz,
 last_used_at timestamptz,created_at timestamptz NOT NULL DEFAULT now(),version integer NOT NULL DEFAULT 1,
 rate_limit integer NOT NULL DEFAULT 120 CHECK(rate_limit BETWEEN 1 AND 600),
 PRIMARY KEY(tenant_id,id),UNIQUE(token_hash)
);
CREATE TABLE deployment_batches (
 tenant_id text NOT NULL,request_key text NOT NULL,request_hash text NOT NULL,result jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,request_key)
);
CREATE TABLE maintenance_clock (tenant_id text PRIMARY KEY,last_run_at timestamptz NOT NULL);

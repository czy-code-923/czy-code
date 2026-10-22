CREATE TABLE site_settings (tenant_id text PRIMARY KEY REFERENCES tenants(id), domain text NOT NULL, https boolean NOT NULL DEFAULT true, updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE live_services (
 tenant_id text NOT NULL, id text NOT NULL, host_id text NOT NULL, name text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('shadowsocks','frps','frpc')), address text NOT NULL, port int NOT NULL CHECK(port BETWEEN 1024 AND 65535),
 parent_id text, next_id text, sealed bytea NOT NULL, enabled boolean NOT NULL DEFAULT false,
 version int NOT NULL DEFAULT 1, reported_version int NOT NULL DEFAULT 0,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','running','stopped','failed')),
 reported_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id), FOREIGN KEY(tenant_id,host_id) REFERENCES hosts(tenant_id,id),
 FOREIGN KEY(tenant_id,parent_id) REFERENCES live_services(tenant_id,id),
 FOREIGN KEY(tenant_id,next_id) REFERENCES live_services(tenant_id,id)
);
CREATE UNIQUE INDEX live_listen_port ON live_services(tenant_id,host_id,port) WHERE kind <> 'frpc';
CREATE TABLE live_ports (tenant_id text NOT NULL, host_id text NOT NULL, port int NOT NULL, service_id text NOT NULL, PRIMARY KEY(tenant_id,host_id,port), FOREIGN KEY(tenant_id,service_id) REFERENCES live_services(tenant_id,id));
CREATE TABLE subscriptions (
 tenant_id text NOT NULL REFERENCES tenants(id), id text NOT NULL, name text NOT NULL, token_hash text NOT NULL UNIQUE,
 expires_at timestamptz NOT NULL, revoked_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(tenant_id,id)
);
CREATE TABLE subscription_services (tenant_id text NOT NULL, subscription_id text NOT NULL, service_id text NOT NULL,
 PRIMARY KEY(tenant_id,subscription_id,service_id), FOREIGN KEY(tenant_id,subscription_id) REFERENCES subscriptions(tenant_id,id), FOREIGN KEY(tenant_id,service_id) REFERENCES live_services(tenant_id,id));
CREATE INDEX subscriptions_expiry ON subscriptions(expires_at) WHERE revoked_at IS NULL;

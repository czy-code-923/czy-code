ALTER TABLE proxy_users ADD COLUMN device_limit_mode text NOT NULL DEFAULT 'compatible' CHECK(device_limit_mode IN ('compatible','hwid'));
CREATE TABLE subscription_clients (
 tenant_id text NOT NULL,id text NOT NULL,user_id text NOT NULL,fingerprint text NOT NULL,
 name text NOT NULL,client_id text NOT NULL,user_agent text NOT NULL,source_ip inet NOT NULL,
 identity_kind text NOT NULL CHECK(identity_kind IN ('client_reported_hwid','source_and_client')),
 enabled boolean NOT NULL DEFAULT true,version integer NOT NULL DEFAULT 1,
 device_os text NOT NULL DEFAULT '',os_version text NOT NULL DEFAULT '',device_model text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),last_subscription_at timestamptz NOT NULL DEFAULT now(),last_connected_at timestamptz,
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,user_id,fingerprint),
 FOREIGN KEY(tenant_id,user_id) REFERENCES proxy_users(tenant_id,id) ON DELETE CASCADE
);
CREATE INDEX subscription_clients_parent ON subscription_clients(tenant_id,user_id,last_subscription_at);
UPDATE live_services SET version=version+1,state='pending' WHERE managed_users;

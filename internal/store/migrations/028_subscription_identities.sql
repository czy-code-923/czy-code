ALTER TABLE proxy_users ADD COLUMN legacy_credentials boolean NOT NULL DEFAULT true;
ALTER TABLE subscriptions ADD COLUMN independent_identity boolean NOT NULL DEFAULT false;
CREATE TABLE subscription_identities (
 tenant_id text NOT NULL, id text NOT NULL, subscription_id text NOT NULL,
 user_id text NOT NULL, device_id text, sealed bytea NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,id),
 FOREIGN KEY (tenant_id,subscription_id) REFERENCES subscriptions(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY (tenant_id,user_id) REFERENCES proxy_users(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY (tenant_id,device_id) REFERENCES user_devices(tenant_id,id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX subscription_identity_compat ON subscription_identities(tenant_id,subscription_id) WHERE device_id IS NULL;
CREATE UNIQUE INDEX subscription_identity_device ON subscription_identities(tenant_id,subscription_id,device_id) WHERE device_id IS NOT NULL;

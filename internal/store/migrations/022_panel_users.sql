ALTER TABLE proxy_users ADD COLUMN version integer NOT NULL DEFAULT 1;
ALTER TABLE proxy_users ADD COLUMN external_id text;
CREATE UNIQUE INDEX proxy_users_external_id ON proxy_users(tenant_id,external_id) WHERE external_id IS NOT NULL;
CREATE TABLE user_create_requests (
 tenant_id text NOT NULL REFERENCES tenants(id),
 actor text NOT NULL,
 request_key text NOT NULL,
 request_hash text NOT NULL,
 user_id text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,actor,request_key)
);
CREATE INDEX user_create_requests_expiry ON user_create_requests(created_at);

ALTER TABLE proxy_users ADD COLUMN commercial_only boolean NOT NULL DEFAULT false;
ALTER TABLE proxy_users ADD COLUMN commercial_protocols jsonb NOT NULL DEFAULT '[]';
CREATE TABLE commerce_entitlements(tenant_id text NOT NULL,order_id text NOT NULL,external_id text NOT NULL,request_hash text NOT NULL,response jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(tenant_id,order_id));

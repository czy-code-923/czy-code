CREATE TABLE IF NOT EXISTS tenants (id text PRIMARY KEY, name text NOT NULL);
INSERT INTO tenants VALUES ('self','Personal workspace') ON CONFLICT DO NOTHING;
CREATE TABLE hosts (
 tenant_id text NOT NULL REFERENCES tenants(id), id text NOT NULL, name text NOT NULL,
 provider text NOT NULL, address text NOT NULL, agent_hash text NOT NULL,
 last_seen timestamptz, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id)
);
CREATE TABLE nodes (
 tenant_id text NOT NULL REFERENCES tenants(id), id text NOT NULL, name text NOT NULL,
 engine text NOT NULL CHECK(engine IN ('sing-box','frp')),
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(tenant_id,id)
);
CREATE TABLE instances (
 tenant_id text NOT NULL, id text NOT NULL, name text NOT NULL,
 host_id text NOT NULL, node_id text NOT NULL, listen_port integer NOT NULL CHECK(listen_port BETWEEN 1024 AND 65535),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','simulated')),
 applied_config_id text, created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(tenant_id,id),
 UNIQUE(tenant_id,host_id,listen_port), UNIQUE(tenant_id,host_id,node_id),
 FOREIGN KEY(tenant_id,host_id) REFERENCES hosts(tenant_id,id),
 FOREIGN KEY(tenant_id,node_id) REFERENCES nodes(tenant_id,id)
);
CREATE TABLE tunnels (
 tenant_id text NOT NULL, id text NOT NULL, name text NOT NULL, instance_id text NOT NULL,
 local_host text NOT NULL CHECK(local_host IN ('127.0.0.1','::1')), local_port integer NOT NULL CHECK(local_port BETWEEN 1 AND 65535),
 remote_port integer NOT NULL CHECK(remote_port BETWEEN 1024 AND 65535),
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,instance_id,remote_port),
 FOREIGN KEY(tenant_id,instance_id) REFERENCES instances(tenant_id,id)
);
CREATE TABLE configs (
 tenant_id text NOT NULL, id text NOT NULL, instance_id text NOT NULL, version integer NOT NULL,
 spec jsonb NOT NULL, checksum text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,instance_id,version), UNIQUE(tenant_id,instance_id,id),
 FOREIGN KEY(tenant_id,instance_id) REFERENCES instances(tenant_id,id)
);
ALTER TABLE instances ADD CONSTRAINT applied_config_fk FOREIGN KEY(tenant_id,id,applied_config_id) REFERENCES configs(tenant_id,instance_id,id);
CREATE TABLE tasks (
 tenant_id text NOT NULL, id text NOT NULL, instance_id text NOT NULL, config_id text NOT NULL,
 request_key text NOT NULL, state text NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','running','succeeded','failed')),
 attempts integer NOT NULL DEFAULT 0, lease_token text, lease_until timestamptz,
 result text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,request_key),
 FOREIGN KEY(tenant_id,instance_id,config_id) REFERENCES configs(tenant_id,instance_id,id)
);
CREATE UNIQUE INDEX one_active_task ON tasks(tenant_id,instance_id) WHERE state IN ('queued','running');
CREATE INDEX task_queue ON tasks(tenant_id,created_at) WHERE state IN ('queued','running');
CREATE TABLE audit (
 sequence bigserial PRIMARY KEY, tenant_id text NOT NULL REFERENCES tenants(id), actor text NOT NULL,
 action text NOT NULL, resource_id text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);

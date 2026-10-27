CREATE TABLE IF NOT EXISTS engine_updates (
 tenant_id text NOT NULL REFERENCES tenants(id),
 id text NOT NULL,
 host_id text NOT NULL,
 engine text NOT NULL CHECK(engine IN ('sing-box','xray')),
 version text NOT NULL,
 state text NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','downloading','validating','succeeded','failed')),
 error text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,host_id) REFERENCES hosts(tenant_id,id)
);
CREATE UNIQUE INDEX IF NOT EXISTS one_active_engine_update ON engine_updates(tenant_id,host_id) WHERE state IN ('queued','downloading','validating');

CREATE TABLE route_plans (
 tenant_id text NOT NULL REFERENCES tenants(id), id text NOT NULL, name text NOT NULL,
 hops jsonb NOT NULL CHECK(jsonb_typeof(hops)='array' AND jsonb_array_length(hops) BETWEEN 2 AND 8),
 state text NOT NULL DEFAULT 'planned' CHECK(state='planned'),
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(tenant_id,id)
);

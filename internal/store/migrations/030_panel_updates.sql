CREATE TABLE panel_updater (
 tenant_id text PRIMARY KEY, layout text NOT NULL CHECK(layout IN ('legacy','quickstart')),
 platform text NOT NULL CHECK(platform IN ('linux-amd64','linux-arm64')),
 last_seen timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE panel_updates (
 tenant_id text NOT NULL,id text NOT NULL,from_version text NOT NULL,target_version text NOT NULL,
 state text NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','downloading','verifying','backing_up','applying','checking','succeeded','rolled_back','failed')),
 detail text NOT NULL DEFAULT '',created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id)
);
CREATE UNIQUE INDEX one_panel_update ON panel_updates(tenant_id) WHERE state NOT IN ('succeeded','rolled_back','failed');

ALTER TABLE hosts ADD COLUMN agent_version text NOT NULL DEFAULT '';
ALTER TABLE hosts ADD COLUMN platform text NOT NULL DEFAULT '';
ALTER TABLE hosts ADD COLUMN reported_addresses jsonb NOT NULL DEFAULT '[]';

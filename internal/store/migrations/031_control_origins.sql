ALTER TABLE site_settings ADD COLUMN agent_origins jsonb NOT NULL DEFAULT '[]';
ALTER TABLE hosts ADD COLUMN control_origin text NOT NULL DEFAULT '';
ALTER TABLE panel_updater ADD COLUMN database_resources jsonb NOT NULL DEFAULT '{}';

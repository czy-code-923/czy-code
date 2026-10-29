ALTER TABLE site_settings ADD COLUMN nginx_version text NOT NULL DEFAULT '';
ALTER TABLE site_settings ADD COLUMN nginx_source text NOT NULL DEFAULT '';
ALTER TABLE site_settings ADD COLUMN nginx_checked_at timestamptz;

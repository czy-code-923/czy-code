ALTER TABLE site_settings ADD COLUMN gateway_domain text;
ALTER TABLE site_settings ADD COLUMN gateway_https boolean;
ALTER TABLE site_settings ADD COLUMN gateway_state text;
ALTER TABLE site_settings ADD COLUMN gateway_checked_at timestamptz;

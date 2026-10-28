ALTER TABLE user_devices ADD COLUMN registration_source text NOT NULL DEFAULT 'manual' CHECK(registration_source IN ('manual','subscription_hwid'));
ALTER TABLE user_devices ADD COLUMN hwid_hash text;
ALTER TABLE user_devices ADD COLUMN device_os text NOT NULL DEFAULT '';
ALTER TABLE user_devices ADD COLUMN os_version text NOT NULL DEFAULT '';
ALTER TABLE user_devices ADD COLUMN device_model text NOT NULL DEFAULT '';
CREATE UNIQUE INDEX user_devices_hwid_unique ON user_devices(tenant_id,user_id,hwid_hash) WHERE hwid_hash IS NOT NULL;

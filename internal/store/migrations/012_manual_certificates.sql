ALTER TABLE managed_certificates DROP CONSTRAINT managed_certificates_provider_check;
ALTER TABLE managed_certificates ADD CONSTRAINT managed_certificates_provider_check CHECK(provider IN ('http-01','cloudflare','manual'));

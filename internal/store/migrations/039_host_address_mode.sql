ALTER TABLE hosts ADD COLUMN address_mode text NOT NULL DEFAULT 'auto' CHECK (address_mode IN ('auto','manual'));
-- Preserve existing independent inventory/NAT addresses and domains. Matching
-- interface IPs and outbound-only registrations follow subsequent heartbeats.
UPDATE hosts SET address_mode='manual'
WHERE address<>'outbound-agent' AND NOT (reported_addresses ? address);

-- Revocation now removes the key record; only the action audit remains, with no key material.
DELETE FROM api_keys WHERE revoked_at IS NOT NULL;

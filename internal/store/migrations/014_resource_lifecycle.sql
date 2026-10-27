ALTER TABLE hosts ADD COLUMN deleted_at timestamptz;
ALTER TABLE live_services ADD COLUMN delete_requested_at timestamptz;
ALTER TABLE subscriptions ADD COLUMN sealed_token bytea;

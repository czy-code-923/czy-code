ALTER TABLE notification_settings
 ADD COLUMN last_poll_error text NOT NULL DEFAULT '',
 ADD COLUMN last_send_error text NOT NULL DEFAULT '',
 ADD COLUMN last_command_at timestamptz,
 ADD COLUMN last_command text NOT NULL DEFAULT '',
 ADD COLUMN last_ignored_reason text NOT NULL DEFAULT '',
 ADD COLUMN last_ignored_at timestamptz,
 ADD COLUMN bot_username text NOT NULL DEFAULT '',
 ADD COLUMN telegram_clock_skew_seconds integer NOT NULL DEFAULT 0;

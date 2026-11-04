ALTER TABLE tenants ADD COLUMN traffic_display_unit text NOT NULL DEFAULT 'GB' CHECK (traffic_display_unit IN ('GB','GiB','auto'));

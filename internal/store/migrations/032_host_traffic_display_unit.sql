ALTER TABLE host_traffic ADD COLUMN display_unit text NOT NULL DEFAULT 'GiB' CHECK (display_unit IN ('GB','GiB'));

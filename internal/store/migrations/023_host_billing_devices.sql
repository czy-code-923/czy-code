ALTER TABLE proxy_users ADD COLUMN billing_mode text NOT NULL DEFAULT 'both' CHECK(billing_mode IN ('both','uplink','downlink','max'));
ALTER TABLE proxy_users ADD COLUMN multiplier_milli integer NOT NULL DEFAULT 1000 CHECK(multiplier_milli BETWEEN 1 AND 100000);
ALTER TABLE proxy_users ADD COLUMN billed_upload_bytes bigint NOT NULL DEFAULT 0 CHECK(billed_upload_bytes>=0);
ALTER TABLE proxy_users ADD COLUMN billed_download_bytes bigint NOT NULL DEFAULT 0 CHECK(billed_download_bytes>=0);
UPDATE proxy_users SET billed_upload_bytes=upload_bytes,billed_download_bytes=download_bytes;
ALTER TABLE proxy_users ADD COLUMN max_devices integer NOT NULL DEFAULT 0 CHECK(max_devices BETWEEN 0 AND 20);
ALTER TABLE user_groups ADD COLUMN max_devices integer NOT NULL DEFAULT 0 CHECK(max_devices BETWEEN 0 AND 20);

CREATE TABLE host_traffic (
 tenant_id text NOT NULL,host_id text NOT NULL,version integer NOT NULL DEFAULT 1,
 quota_bytes bigint NOT NULL DEFAULT 0 CHECK(quota_bytes BETWEEN 0 AND 9000000000000000),
 mode text NOT NULL DEFAULT 'both' CHECK(mode IN ('both','rx','tx','max')),
 reset_day integer NOT NULL DEFAULT 1 CHECK(reset_day BETWEEN 1 AND 31),
 auto_reset boolean NOT NULL DEFAULT true,next_reset_at timestamptz,
 interfaces jsonb NOT NULL DEFAULT '[]',
 period_rx bigint NOT NULL DEFAULT 0 CHECK(period_rx>=0),period_tx bigint NOT NULL DEFAULT 0 CHECK(period_tx>=0),
 adjustment_bytes bigint NOT NULL DEFAULT 0 CHECK(adjustment_bytes BETWEEN 0 AND 9000000000000000),
 used_bytes bigint GENERATED ALWAYS AS (adjustment_bytes+CASE mode WHEN 'rx' THEN period_rx WHEN 'tx' THEN period_tx WHEN 'max' THEN GREATEST(period_rx,period_tx) ELSE period_rx+period_tx END) STORED,
 lifetime_rx bigint NOT NULL DEFAULT 0,lifetime_tx bigint NOT NULL DEFAULT 0,
 epoch text NOT NULL DEFAULT '',epoch_started_at timestamptz,started_at timestamptz,last_sample_at timestamptz,
 last_reset_at timestamptz NOT NULL DEFAULT now(),quality text NOT NULL DEFAULT 'waiting',gap_count bigint NOT NULL DEFAULT 0,agent_gaps bigint NOT NULL DEFAULT 0,acknowledged_gaps bigint NOT NULL DEFAULT 0,calibrated_at timestamptz,
 rx_rate bigint NOT NULL DEFAULT 0,tx_rate bigint NOT NULL DEFAULT 0,updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,host_id),FOREIGN KEY(tenant_id,host_id) REFERENCES hosts(tenant_id,id) ON DELETE CASCADE
);
INSERT INTO host_traffic(tenant_id,host_id) SELECT tenant_id,id FROM hosts;
UPDATE host_traffic SET next_reset_at=(date_trunc('month',now() AT TIME ZONE 'Asia/Shanghai')+interval '1 month') AT TIME ZONE 'Asia/Shanghai';
CREATE TABLE host_interface_cursors (
 tenant_id text NOT NULL,host_id text NOT NULL,name text NOT NULL,
 rx bigint NOT NULL,tx bigint NOT NULL,is_uplink boolean NOT NULL,epoch text NOT NULL,
 sampled_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,host_id,name),FOREIGN KEY(tenant_id,host_id) REFERENCES hosts(tenant_id,id) ON DELETE CASCADE
);

CREATE TABLE user_devices (
 tenant_id text NOT NULL,id text NOT NULL,user_id text NOT NULL,name text NOT NULL,client_id text NOT NULL,
 enabled boolean NOT NULL DEFAULT true,version integer NOT NULL DEFAULT 1,sealed bytea NOT NULL,
 all_nodes boolean NOT NULL DEFAULT true,service_ids jsonb NOT NULL DEFAULT '[]',
 created_at timestamptz NOT NULL DEFAULT now(),last_subscription_at timestamptz,last_connected_at timestamptz,
 user_agent text NOT NULL DEFAULT '',last_source_ip inet,
 PRIMARY KEY(tenant_id,id),FOREIGN KEY(tenant_id,user_id) REFERENCES proxy_users(tenant_id,id) ON DELETE CASCADE,
 UNIQUE(tenant_id,user_id,name)
);
CREATE INDEX user_devices_parent ON user_devices(tenant_id,user_id,enabled);
ALTER TABLE subscriptions ADD COLUMN device_id text;
ALTER TABLE subscriptions ADD CONSTRAINT subscriptions_device_fk FOREIGN KEY(tenant_id,device_id) REFERENCES user_devices(tenant_id,id) ON DELETE CASCADE;
CREATE UNIQUE INDEX subscriptions_device_unique ON subscriptions(tenant_id,device_id) WHERE device_id IS NOT NULL;

ALTER TABLE access_observations ADD COLUMN closed boolean NOT NULL DEFAULT false;
ALTER TABLE access_observations ADD COLUMN device_id text NOT NULL DEFAULT '';
ALTER TABLE access_observations ADD COLUMN uplink bigint CHECK(uplink>=0);
ALTER TABLE access_observations ADD COLUMN downlink bigint CHECK(downlink>=0);
ALTER TABLE access_observations ADD COLUMN last_seen_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX access_observations_user_sort ON access_observations(tenant_id,user_id,observed_at,event_id);
CREATE INDEX access_observations_bytes_sort ON access_observations(tenant_id,((uplink+downlink)),observed_at,event_id);

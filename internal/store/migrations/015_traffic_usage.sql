ALTER TABLE proxy_users ADD COLUMN quota_bytes bigint NOT NULL DEFAULT 0 CHECK(quota_bytes BETWEEN 0 AND 9000000000000000);
ALTER TABLE proxy_users ADD COLUMN used_bytes bigint NOT NULL DEFAULT 0 CHECK(used_bytes>=0);
ALTER TABLE proxy_users ADD COLUMN lifetime_bytes bigint NOT NULL DEFAULT 0 CHECK(lifetime_bytes>=0);
ALTER TABLE proxy_users ADD COLUMN upload_bytes bigint NOT NULL DEFAULT 0 CHECK(upload_bytes>=0);
ALTER TABLE proxy_users ADD COLUMN download_bytes bigint NOT NULL DEFAULT 0 CHECK(download_bytes>=0);
ALTER TABLE proxy_users ADD COLUMN reset_strategy text NOT NULL DEFAULT 'never' CHECK(reset_strategy IN ('never','daily','monthly'));
ALTER TABLE proxy_users ADD COLUMN last_reset_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE proxy_users ADD COLUMN last_active_at timestamptz;
CREATE TABLE traffic_cursors (
 tenant_id text NOT NULL,host_id text NOT NULL,counter_id text NOT NULL,subject_kind text NOT NULL CHECK(subject_kind IN ('user','service')),subject_id text NOT NULL,
 uplink bigint NOT NULL CHECK(uplink>=0),downlink bigint NOT NULL CHECK(downlink>=0),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,host_id,counter_id),FOREIGN KEY(tenant_id,host_id) REFERENCES hosts(tenant_id,id)
);
CREATE TABLE traffic_daily (
 tenant_id text NOT NULL,day date NOT NULL,host_id text NOT NULL,subject_kind text NOT NULL,subject_id text NOT NULL,uplink bigint NOT NULL DEFAULT 0,downlink bigint NOT NULL DEFAULT 0,
 PRIMARY KEY(tenant_id,day,host_id,subject_kind,subject_id),FOREIGN KEY(tenant_id,host_id) REFERENCES hosts(tenant_id,id)
);
CREATE TABLE traffic_subjects (
 tenant_id text NOT NULL,host_id text NOT NULL,subject_kind text NOT NULL,subject_id text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,host_id,subject_kind,subject_id),FOREIGN KEY(tenant_id,host_id) REFERENCES hosts(tenant_id,id)
);
CREATE TABLE access_observations (
 tenant_id text NOT NULL,host_id text NOT NULL,event_id text NOT NULL,user_id text NOT NULL DEFAULT '',service_id text NOT NULL,
 source_ip inet NOT NULL,destination text NOT NULL,destination_port int NOT NULL CHECK(destination_port BETWEEN 1 AND 65535),network text NOT NULL CHECK(network IN ('tcp','udp')),
 observed_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(tenant_id,host_id,event_id),FOREIGN KEY(tenant_id,host_id) REFERENCES hosts(tenant_id,id)
);
CREATE INDEX access_observations_recent ON access_observations(tenant_id,observed_at DESC);
CREATE INDEX traffic_cursors_age ON traffic_cursors(tenant_id,updated_at);
CREATE TABLE telemetry_status (
 tenant_id text NOT NULL,host_id text NOT NULL,engine text NOT NULL,state text NOT NULL,error text NOT NULL DEFAULT '',reported_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,host_id,engine),FOREIGN KEY(tenant_id,host_id) REFERENCES hosts(tenant_id,id)
);

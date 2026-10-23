ALTER TABLE live_services ADD COLUMN managed_users boolean NOT NULL DEFAULT false;
CREATE TABLE host_kernels (
 tenant_id text NOT NULL,host_id text NOT NULL,engine text NOT NULL CHECK(engine IN ('sing-box','xray','frps','frpc')),
 available boolean NOT NULL,version text NOT NULL,checked_at timestamptz NOT NULL DEFAULT now(),desired_checksum text NOT NULL DEFAULT '',applied_checksum text NOT NULL DEFAULT '',state text NOT NULL DEFAULT 'pending',reported_at timestamptz,
 PRIMARY KEY(tenant_id,host_id,engine),FOREIGN KEY(tenant_id,host_id) REFERENCES hosts(tenant_id,id) ON DELETE CASCADE
);
CREATE TABLE kernel_revisions (
 tenant_id text NOT NULL,host_id text NOT NULL,engine text NOT NULL,checksum text NOT NULL,sealed bytea NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,host_id,engine,checksum),FOREIGN KEY(tenant_id,host_id) REFERENCES hosts(tenant_id,id) ON DELETE CASCADE
);
CREATE TABLE proxy_users (
 tenant_id text NOT NULL REFERENCES tenants(id),id text NOT NULL,name text NOT NULL,enabled boolean NOT NULL DEFAULT true,expires_at timestamptz NOT NULL,sealed bytea NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,name)
);
CREATE TABLE user_groups (
 tenant_id text NOT NULL REFERENCES tenants(id),id text NOT NULL,name text NOT NULL,version int NOT NULL DEFAULT 1,PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,name)
);
CREATE TABLE user_group_members (
 tenant_id text NOT NULL,group_id text NOT NULL,user_id text NOT NULL,PRIMARY KEY(tenant_id,group_id,user_id),
 FOREIGN KEY(tenant_id,group_id) REFERENCES user_groups(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,user_id) REFERENCES proxy_users(tenant_id,id) ON DELETE CASCADE
);
CREATE TABLE user_group_services (
 tenant_id text NOT NULL,group_id text NOT NULL,service_id text NOT NULL,PRIMARY KEY(tenant_id,group_id,service_id),
 FOREIGN KEY(tenant_id,group_id) REFERENCES user_groups(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,service_id) REFERENCES live_services(tenant_id,id)
);
ALTER TABLE subscriptions ADD COLUMN user_id text;
ALTER TABLE subscriptions ADD CONSTRAINT subscription_user_fk FOREIGN KEY(tenant_id,user_id) REFERENCES proxy_users(tenant_id,id);
ALTER TABLE subscriptions ADD COLUMN profile_id text;
CREATE TABLE client_profiles (
 tenant_id text NOT NULL REFERENCES tenants(id),id text NOT NULL,name text NOT NULL,format text NOT NULL CHECK(format IN ('sing-box','mihomo','shadowrocket','xray')),local_port int NOT NULL CHECK(local_port BETWEEN 1024 AND 65535),selection text NOT NULL CHECK(selection IN ('select','urltest')),test_url text NOT NULL DEFAULT 'https://www.gstatic.com/generate_204',PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,name)
);
ALTER TABLE subscriptions ADD CONSTRAINT subscription_profile_fk FOREIGN KEY(tenant_id,profile_id) REFERENCES client_profiles(tenant_id,id);

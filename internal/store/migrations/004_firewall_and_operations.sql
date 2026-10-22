CREATE TABLE firewall_policies (
 tenant_id text NOT NULL, id text NOT NULL, host_id text NOT NULL, name text NOT NULL,
 version integer NOT NULL, spec jsonb NOT NULL, checksum text NOT NULL,
 archived_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,host_id,version),
 FOREIGN KEY(tenant_id,host_id) REFERENCES hosts(tenant_id,id)
);
ALTER TABLE tasks DROP CONSTRAINT tasks_state_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_state_check CHECK(state IN ('queued','running','succeeded','failed','cancelled'));
CREATE INDEX expired_task_lease ON tasks(tenant_id,lease_until) WHERE state='running';
CREATE INDEX hosts_recent ON hosts(tenant_id,created_at DESC,id);
CREATE INDEX nodes_recent ON nodes(tenant_id,created_at DESC,id);
CREATE INDEX instances_recent ON instances(tenant_id,created_at DESC,id);
CREATE INDEX configs_recent ON configs(tenant_id,created_at DESC,id);
CREATE INDEX audit_recent ON audit(tenant_id,created_at DESC,sequence);
CREATE INDEX tasks_recent ON tasks(tenant_id,created_at DESC,id);
CREATE INDEX firewall_recent ON firewall_policies(tenant_id,created_at DESC,id);

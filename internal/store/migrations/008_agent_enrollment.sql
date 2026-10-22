CREATE TABLE agent_enrollments (
 tenant_id text NOT NULL REFERENCES tenants(id),
 host_id text NOT NULL,
 token_hash text PRIMARY KEY,
 expires_at timestamptz NOT NULL,
 consumed_at timestamptz,
 revoked_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY (tenant_id,host_id) REFERENCES hosts(tenant_id,id) ON DELETE CASCADE
);
CREATE INDEX agent_enrollment_host ON agent_enrollments(tenant_id,host_id,created_at DESC);

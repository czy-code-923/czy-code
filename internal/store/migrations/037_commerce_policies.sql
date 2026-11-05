CREATE TABLE commerce_account_policies (
 tenant_id text NOT NULL,
 external_id text NOT NULL,
 source_id text NOT NULL,
 revision bigint NOT NULL CHECK(revision>0),
 request_hash text NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,external_id)
);

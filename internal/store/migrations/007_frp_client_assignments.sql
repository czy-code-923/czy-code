CREATE TABLE frp_assignments (
 tenant_id text NOT NULL, parent_id text NOT NULL, remote_port integer NOT NULL, client_id text NOT NULL,
 PRIMARY KEY(tenant_id,parent_id,remote_port),
 FOREIGN KEY(tenant_id,parent_id) REFERENCES live_services(tenant_id,id),
 FOREIGN KEY(tenant_id,client_id) REFERENCES live_services(tenant_id,id)
);

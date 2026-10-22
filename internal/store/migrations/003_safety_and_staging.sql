-- Fail the upgrade on an existing cross-resource port collision; never silently reassign ports.
CREATE TABLE port_reservations (
 tenant_id text NOT NULL, host_id text NOT NULL, port integer NOT NULL CHECK(port BETWEEN 1024 AND 65535),
 instance_id text NOT NULL, tunnel_id text,
 PRIMARY KEY(tenant_id,host_id,port),
 FOREIGN KEY(tenant_id,host_id) REFERENCES hosts(tenant_id,id),
 FOREIGN KEY(tenant_id,instance_id) REFERENCES instances(tenant_id,id),
 FOREIGN KEY(tenant_id,tunnel_id) REFERENCES tunnels(tenant_id,id)
);
INSERT INTO port_reservations SELECT tenant_id,host_id,listen_port,id,NULL FROM instances;
INSERT INTO port_reservations SELECT t.tenant_id,i.host_id,t.remote_port,i.id,t.id FROM tunnels t JOIN instances i ON (i.tenant_id,i.id)=(t.tenant_id,t.instance_id);
CREATE TABLE route_hops (
 tenant_id text NOT NULL, route_id text NOT NULL, position integer NOT NULL CHECK(position BETWEEN 1 AND 8), instance_id text NOT NULL,
 PRIMARY KEY(tenant_id,route_id,position), UNIQUE(tenant_id,route_id,instance_id),
 FOREIGN KEY(tenant_id,route_id) REFERENCES route_plans(tenant_id,id),
 FOREIGN KEY(tenant_id,instance_id) REFERENCES instances(tenant_id,id)
);
INSERT INTO route_hops SELECT p.tenant_id,p.id,h.ordinality,h.value FROM route_plans p CROSS JOIN LATERAL jsonb_array_elements_text(p.hops) WITH ORDINALITY h(value,ordinality);
ALTER TABLE hosts ADD COLUMN agent_mode text NOT NULL DEFAULT 'simulation' CHECK(agent_mode IN ('simulation','staging'));
ALTER TABLE instances DROP CONSTRAINT instances_state_check;
ALTER TABLE instances ADD CONSTRAINT instances_state_check CHECK(state IN ('pending','simulated','staged'));
ALTER TABLE instances ADD COLUMN staged_config_id text;
ALTER TABLE instances ADD FOREIGN KEY(tenant_id,id,staged_config_id) REFERENCES configs(tenant_id,instance_id,id);
ALTER TABLE tasks ADD COLUMN mode text NOT NULL DEFAULT 'simulation' CHECK(mode IN ('simulation','staging'));
ALTER TABLE tasks ADD COLUMN operation text NOT NULL DEFAULT 'deploy' CHECK(operation IN ('deploy','rollback'));
ALTER TABLE tasks ADD COLUMN artifact_checksum text NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN completed_lease text;

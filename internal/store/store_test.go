package store

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"os"
	"testing"
	"time"
)

func TestUpgradeRejectsExistingPortCollision(t *testing.T) {
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, raw)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	schema := fmt.Sprintf("upgrade_%d", time.Now().UnixNano())
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	u, e := url.Parse(raw)
	if e != nil {
		t.Fatal(e)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	old, e := pgxpool.New(ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	defer old.Close()
	if _, e = old.Exec(ctx, "CREATE TABLE schema_migrations(version text PRIMARY KEY,applied_at timestamptz NOT NULL DEFAULT now())"); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"001_init.sql", "002_routes.sql"} {
		b, _ := migrations.ReadFile("migrations/" + name)
		if _, e = old.Exec(ctx, string(b)); e != nil {
			t.Fatal(e)
		}
		if _, e = old.Exec(ctx, "INSERT INTO schema_migrations(version) VALUES($1)", name); e != nil {
			t.Fatal(e)
		}
	}
	_, e = old.Exec(ctx, `INSERT INTO hosts(tenant_id,id,name,provider,address,agent_hash) VALUES('self','host','h','p','inventory.invalid','hash');
 INSERT INTO nodes(tenant_id,id,name,engine) VALUES('self','proxy','p','sing-box'),('self','frp','f','frp');
 INSERT INTO instances(tenant_id,id,name,host_id,node_id,listen_port) VALUES('self','proxy','p','host','proxy',2080),('self','frp','f','host','frp',7000);
 INSERT INTO tunnels(tenant_id,id,name,instance_id,local_host,local_port,remote_port) VALUES('self','collision','c','frp','127.0.0.1',8080,2080)`)
	if e != nil {
		t.Fatal(e)
	}
	if p, e := Open(ctx, u.String()); e == nil {
		p.Close()
		t.Fatal("collision upgrade succeeded")
	}
	var count int
	if e = old.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); e != nil || count != 2 {
		t.Fatal("partial migration committed", e, count)
	}
	var table *string
	if e = old.QueryRow(ctx, "SELECT to_regclass('port_reservations')::text").Scan(&table); e != nil || table != nil {
		t.Fatal("partial table survived", e)
	}
	if _, e = old.Exec(ctx, "UPDATE tunnels SET remote_port=18080 WHERE id='collision'"); e != nil {
		t.Fatal(e)
	}
	upgraded, e := Open(ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	defer upgraded.Close()
	if e = upgraded.QueryRow(ctx, "SELECT count(*) FROM port_reservations").Scan(&count); e != nil || count != 3 {
		t.Fatal("backfill missing", count, e)
	}
}

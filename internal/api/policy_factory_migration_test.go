package api

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"testing"
	"vps-control/internal/clientpolicy"
)

//go:embed testdata/legacy-default-v016.yaml
var legacyDefaultFixture string

func TestLegacyFactoryMigrationPreservesCustomPolicy(t *testing.T) {
	p, e := clientpolicy.Parse(legacyDefaultFixture)
	if e != nil {
		t.Fatal(e)
	}
	source, e := marshalPolicyYAML(p)
	if e != nil {
		t.Fatal(e)
	}
	if fmt.Sprintf("%x", sha256.Sum256([]byte(source))) != legacyDefaultSourceSHA {
		t.Fatal("legacy fixture mismatch")
	}
	s := testControlDB(t)
	ctx := context.Background()
	id := ID()
	_, e = s.DB.Exec(ctx, `INSERT INTO client_policies(tenant_id,id,name,client_id,source) VALUES('self',$1,'Mihomo Max 默认模板','all',$2)`, id, source)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.EnsureDefaultClientPolicy(ctx); e != nil {
		t.Fatal(e)
	}
	var saved string
	var version int
	s.DB.QueryRow(ctx, `SELECT source,version FROM client_policies WHERE id=$1`, id).Scan(&saved, &version)
	if saved == source || version != 2 {
		t.Fatal("factory migration missing")
	}
	if e = s.EnsureDefaultClientPolicy(ctx); e != nil {
		t.Fatal(e)
	}
	s.DB.QueryRow(ctx, `SELECT version FROM client_policies WHERE id=$1`, id).Scan(&version)
	if version != 2 {
		t.Fatal("migration repeated")
	}
	custom := source + "\n# owner custom comment\n"
	s.DB.Exec(ctx, `UPDATE client_policies SET source=$2 WHERE id=$1`, id, custom)
	if e = s.EnsureDefaultClientPolicy(ctx); e != nil {
		t.Fatal(e)
	}
	s.DB.QueryRow(ctx, `SELECT source FROM client_policies WHERE id=$1`, id).Scan(&saved)
	if saved != custom {
		t.Fatal("custom policy replaced")
	}
}

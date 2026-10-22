package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"vps-control/internal/store"
)

func TestAuthenticationAndStrictJSON(t *testing.T) {
	s := (&Server{AdminToken: strings.Repeat("a", 32)}).Handler()
	r := httptest.NewRequest("GET", "/api/v1/hosts", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest("POST", "/api/v1/hosts", strings.NewReader(`{"name":"a","shell":"id"}`))
	r.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 32))
	s.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest("POST", "/api/v1/hosts", strings.NewReader(`{} {}`))
	r.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 32))
	s.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestIntegration(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dbURL)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	schema := "test_" + ID()
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") // generated fixed alphabet only
	u, e := url.Parse(dbURL)
	if e != nil {
		t.Fatal(e)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, e := store.Open(ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	// Re-running migrations is a no-op.
	again, e := store.Open(ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	again.Close()
	token := strings.Repeat("a", 32)
	srv := httptest.NewServer((&Server{DB: db, AdminToken: token}).Handler())
	defer srv.Close()
	request := func(method, path string, body any, agent map[string]any, key string) (int, []byte) {
		t.Helper()
		b, _ := json.Marshal(body)
		r, _ := http.NewRequest(method, srv.URL+"/api/v1/"+path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		if agent != nil {
			r.Header.Set("X-Host-ID", agent["id"].(string))
			r.Header.Set("Authorization", "Bearer "+agent["agent_token"].(string))
		}
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		res, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		var raw json.RawMessage
		if res.StatusCode != 204 {
			if e = json.NewDecoder(res.Body).Decode(&raw); e != nil {
				t.Fatal(e)
			}
		}
		return res.StatusCode, raw
	}
	post := func(path string, body any, want int, agent map[string]any, key string) map[string]any {
		t.Helper()
		code, raw := request("POST", path, body, agent, key)
		if code != want {
			t.Fatalf("%s status=%d want=%d: %s", path, code, want, raw)
		}
		var v map[string]any
		if len(raw) > 0 {
			json.Unmarshal(raw, &v)
		}
		return v
	}
	list := func(kind string) []map[string]any {
		t.Helper()
		code, raw := request("GET", kind, nil, nil, "")
		if code != 200 {
			t.Fatalf("%s: %d %s", kind, code, raw)
		}
		var v []map[string]any
		json.Unmarshal(raw, &v)
		return v
	}
	host := post("hosts", map[string]any{"name": "GreenCloud demo", "provider": "GreenCloud", "address": "inventory.invalid"}, 201, nil, "")
	other := post("hosts", map[string]any{"name": "Other", "provider": "DMIT", "address": "inventory.invalid"}, 201, nil, "")
	if _, ok := list("hosts")[0]["agent_hash"]; ok {
		t.Fatal("credential hash exposed")
	}
	post("agent/claim", nil, 401, map[string]any{"id": host["id"], "agent_token": token}, "")
	node := post("nodes", map[string]any{"name": "proxy", "engine": "sing-box"}, 201, nil, "")
	post("nodes", map[string]any{"name": "unsupported", "engine": "xray"}, 400, nil, "")
	post("hosts", map[string]any{"name": "bad", "provider": "x", "address": "x", "tenant_id": "foreign"}, 400, nil, "")
	makeInstance := func(hostID string, port int) map[string]any {
		return post("instances", map[string]any{"name": "proxy", "host_id": hostID, "node_id": node["id"], "listen_port": port}, 201, nil, "")
	}
	instance := makeInstance(host["id"].(string), 2080)
	otherInstance := makeInstance(other["id"].(string), 2080)
	post("instances", map[string]any{"name": "collision", "host_id": host["id"], "node_id": node["id"], "listen_port": 2080}, 409, nil, "")
	post("instances", map[string]any{"name": "missing", "host_id": "missing", "node_id": node["id"], "listen_port": 2081}, 409, nil, "")
	path := "instances/" + instance["id"].(string)
	cfg := post(path+"/configs", map[string]any{"outbound": "block"}, 201, nil, "")
	cfg2 := post(path+"/configs", map[string]any{"outbound": "direct"}, 201, nil, "")
	if cfg2["version"].(float64) != 2 {
		t.Fatal(cfg2)
	}
	spec, _ := json.Marshal(cfg["spec"])
	if Hash(string(spec)) != cfg["checksum"] {
		t.Fatal("checksum mismatch")
	}
	post("tunnels", map[string]any{"name": "wrong engine", "instance_id": instance["id"], "local_port": 80, "remote_port": 18080}, 400, nil, "")
	post("routes", map[string]any{"name": "cycle", "hops": []any{instance["id"], instance["id"]}}, 400, nil, "")
	job := post(path+"/deploy", map[string]any{"config_id": cfg["id"]}, 202, nil, "request-0001")
	replay := post(path+"/deploy", map[string]any{"config_id": cfg["id"]}, 200, nil, "request-0001")
	if job["id"] != replay["id"] {
		t.Fatal("idempotency failed")
	}
	post(path+"/deploy", map[string]any{"config_id": cfg2["id"]}, 409, nil, "request-0001")
	post(path+"/deploy", map[string]any{"config_id": cfg2["id"]}, 409, nil, "request-0002")
	post("agent/claim", nil, 204, other, "")
	post("agent/heartbeat", nil, 200, host, "")
	// Concurrent consumers must claim a task only once.
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	bodies := make(chan []byte, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, raw := request("POST", "agent/claim", nil, host, "")
			codes <- code
			if code == 200 {
				bodies <- raw
			}
		}()
	}
	wg.Wait()
	close(codes)
	claims := 0
	for code := range codes {
		if code == 200 {
			claims++
		} else if code != 204 {
			t.Fatal(code)
		}
	}
	if claims != 1 {
		t.Fatalf("claims=%d", claims)
	}
	var claim map[string]any
	json.Unmarshal(<-bodies, &claim)
	completePath := "agent/tasks/" + job["id"].(string) + "/complete"
	post(completePath, map[string]any{"lease_token": claim["lease_token"], "success": true}, 409, other, "")
	// Expire and reclaim; old lease cannot complete.
	if _, e = db.Exec(ctx, "UPDATE tasks SET lease_until=now()-interval '1 second' WHERE id=$1", job["id"]); e != nil {
		t.Fatal(e)
	}
	fresh := post("agent/claim", nil, 200, host, "")
	post(completePath, map[string]any{"lease_token": claim["lease_token"], "success": true}, 409, host, "")
	post(completePath, map[string]any{"lease_token": fresh["lease_token"], "success": true}, 200, host, "")
	post(completePath, map[string]any{"lease_token": fresh["lease_token"], "success": true}, 200, host, "")
	post(completePath, map[string]any{"lease_token": fresh["lease_token"], "success": false}, 409, host, "")
	if list("instances")[0]["state"] != "simulated" && list("instances")[1]["state"] != "simulated" {
		t.Fatal("missing simulated state")
	}
	// Exhaust leases and release the per-instance active-task constraint.
	exhausted := post(path+"/deploy", map[string]any{"config_id": cfg2["id"]}, 202, nil, "request-0003")
	post("agent/claim", nil, 200, host, "")
	if _, e = db.Exec(ctx, "UPDATE tasks SET attempts=3,lease_until=now()-interval '1 second' WHERE id=$1", exhausted["id"]); e != nil {
		t.Fatal(e)
	}
	post("agent/claim", nil, 204, host, "")
	var state string
	db.QueryRow(ctx, "SELECT state FROM tasks WHERE id=$1", exhausted["id"]).Scan(&state)
	if state != "failed" {
		t.Fatal(state)
	}
	frp := post("nodes", map[string]any{"name": "frp", "engine": "frp"}, 201, nil, "")
	fi := post("instances", map[string]any{"name": "frp", "host_id": host["id"], "node_id": frp["id"], "listen_port": 7000}, 201, nil, "")
	post("tunnels", map[string]any{"name": "web", "instance_id": fi["id"], "local_host": "127.0.0.1", "local_port": 8080, "remote_port": 18080}, 201, nil, "")
	post("tunnels", map[string]any{"name": "unsafe target", "instance_id": fi["id"], "local_host": "169.254.169.254", "local_port": 80, "remote_port": 18081}, 409, nil, "")
	fc := post("instances/"+fi["id"].(string)+"/configs", map[string]any{"outbound": "block"}, 201, nil, "")
	fs, _ := json.Marshal(fc["spec"])
	if Hash(string(fs)) != fc["checksum"] {
		t.Fatal("FRP checksum mismatch")
	}
	if len(fc["spec"].(map[string]any)["tunnels"].([]any)) != 1 {
		t.Fatal(fc)
	}
	post("routes", map[string]any{"name": "invalid route", "hops": []any{instance["id"], fi["id"]}}, 400, nil, "")
	post("routes", map[string]any{"name": "planned route", "hops": []any{instance["id"], otherInstance["id"]}}, 201, nil, "")
	post("nodes", map[string]any{"name": "unused field", "engine": "sing-box", "address": "ignored before"}, 400, nil, "")
	// Port reservations span instance listeners and every FRP server on the host.
	post("tunnels", map[string]any{"name": "listener collision", "instance_id": fi["id"], "local_port": 8080, "remote_port": 2080}, 409, nil, "")
	frp2 := post("nodes", map[string]any{"name": "second frp", "engine": "frp"}, 201, nil, "")
	post("instances", map[string]any{"name": "remote collision", "host_id": host["id"], "node_id": frp2["id"], "listen_port": 18080}, 409, nil, "")
	fi2 := post("instances", map[string]any{"name": "second frp", "host_id": host["id"], "node_id": frp2["id"], "listen_port": 7001}, 201, nil, "")
	post("tunnels", map[string]any{"name": "other frp collision", "instance_id": fi2["id"], "local_port": 8080, "remote_port": 18080}, 409, nil, "")
	code, preview := request("GET", "configs/"+fc["id"].(string)+"/preview", nil, nil, "")
	if code != 200 || !bytes.Contains(preview, []byte("agent-derived-secret")) {
		t.Fatalf("redacted preview: %d %s", code, preview)
	}
	// A simulated Agent cannot consume staging work. Completion requires native evidence.
	stage := post(path+"/deploy", map[string]any{"config_id": cfg["id"], "mode": "staging"}, 202, nil, "stage-0001")
	post("agent/claim", nil, 204, host, "")
	post("agent/heartbeat", map[string]any{"mode": "staging"}, 200, host, "")
	stageClaim := post("agent/claim", nil, 200, host, "")
	if stageClaim["id"] != stage["id"] || stageClaim["instance_id"] != instance["id"] {
		t.Fatal(stageClaim)
	}
	stagePath := "agent/tasks/" + stage["id"].(string)
	post(stagePath+"/renew", map[string]any{"lease_token": "stale"}, 409, host, "")
	post(stagePath+"/renew", map[string]any{"lease_token": stageClaim["lease_token"]}, 200, host, "")
	post(stagePath+"/complete", map[string]any{"lease_token": stageClaim["lease_token"], "success": true}, 400, host, "")
	post(stagePath+"/complete", map[string]any{"lease_token": stageClaim["lease_token"], "success": true, "artifact_checksum": strings.Repeat("a", 64)}, 200, host, "")
	post(path+"/rollback", map[string]any{"config_id": cfg["id"], "mode": "staging"}, 409, nil, "rollback-noop")
	post(path+"/rollback", map[string]any{"config_id": cfg2["id"], "mode": "staging"}, 409, nil, "rollback-new")
	post(path+"/deploy", map[string]any{"config_id": cfg2["id"], "mode": "staging"}, 202, nil, "stage-0002")
	stageClaim = post("agent/claim", nil, 200, host, "")
	post("agent/tasks/"+stageClaim["id"].(string)+"/complete", map[string]any{"lease_token": stageClaim["lease_token"], "success": true, "artifact_checksum": strings.Repeat("b", 64)}, 200, host, "")
	post(path+"/rollback", map[string]any{"config_id": cfg["id"], "mode": "staging"}, 202, nil, "rollback-0001")
	stageClaim = post("agent/claim", nil, 200, host, "")
	if stageClaim["operation"] != "rollback" {
		t.Fatal(stageClaim)
	}
	post("agent/tasks/"+stageClaim["id"].(string)+"/complete", map[string]any{"lease_token": stageClaim["lease_token"], "success": true, "artifact_checksum": strings.Repeat("a", 64)}, 200, host, "")
	var staged string
	if e = db.QueryRow(ctx, "SELECT staged_config_id FROM instances WHERE id=$1", instance["id"]).Scan(&staged); e != nil || staged != cfg["id"] {
		t.Fatal(staged, e)
	}
	// Reaping does not require any host heartbeat or claim.
	post(path+"/deploy", map[string]any{"config_id": cfg["id"]}, 202, nil, "offline-reap")
	stageClaim = post("agent/claim", nil, 200, host, "")
	if _, e = db.Exec(ctx, "UPDATE tasks SET attempts=3,lease_until=now()-interval '1 second' WHERE id=$1", stageClaim["id"]); e != nil {
		t.Fatal(e)
	}
	if e = (&Server{DB: db}).Reap(ctx); e != nil {
		t.Fatal(e)
	}
	if e = db.QueryRow(ctx, "SELECT state FROM tasks WHERE id=$1", stageClaim["id"]).Scan(&state); e != nil || state != "failed" {
		t.Fatal(state, e)
	}
	// Enqueue racing with completion must never deadlock or return an internal error.
	post(path+"/deploy", map[string]any{"config_id": cfg["id"]}, 202, nil, "lock-order-0001")
	lockedClaim := post("agent/claim", nil, 200, host, "")
	completionCode := make(chan int, 1)
	enqueueCode := make(chan int, 1)
	go func() {
		code, _ := request("POST", "agent/tasks/"+lockedClaim["id"].(string)+"/complete", map[string]any{"lease_token": lockedClaim["lease_token"], "success": true}, host, "")
		completionCode <- code
	}()
	go func() {
		code, _ := request("POST", path+"/deploy", map[string]any{"config_id": cfg2["id"]}, nil, "lock-order-0002")
		enqueueCode <- code
	}()
	if code := <-completionCode; code != 200 {
		t.Fatal("completion race", code)
	}
	if code := <-enqueueCode; code != 202 && code != 409 {
		t.Fatal("enqueue race", code)
	}
	// Another tenant's rows cannot leak through self-scoped list endpoints.
	db.Exec(ctx, "INSERT INTO tenants VALUES('foreign','Other tenant')")
	db.Exec(ctx, "INSERT INTO nodes(tenant_id,id,name,engine) VALUES('foreign','foreign-node','hidden','frp')")
	for _, n := range list("nodes") {
		if n["id"] == "foreign-node" {
			t.Fatal("tenant leak")
		}
	}
	if len(list("audit")) < 15 {
		t.Fatal("audit missing")
	}
	for _, a := range list("audit") {
		if strings.Contains(fmt.Sprint(a), host["agent_token"].(string)) {
			t.Fatal("token in audit")
		}
	}
	post("hosts/"+host["id"].(string)+"/rotate-agent-token", nil, 200, nil, "")
	// Firewall versions are validated, immutable and never applied by the control plane.
	firewallPath := "hosts/" + host["id"].(string) + "/firewalls"
	policy := map[string]any{"name": "restricted ingress", "ssh_port": 22, "admin_cidrs": []string{"192.0.2.0/24"}, "default_action": "drop", "rules": []any{map[string]any{"name": "proxy", "protocol": "tcp", "port": 2080, "source": "0.0.0.0/0", "action": "accept"}}}
	fw := post(firewallPath, policy, 201, nil, "")
	fw2 := post(firewallPath, policy, 201, nil, "")
	if fw2["version"].(float64) != 2 {
		t.Fatal("firewall version", fw2)
	}
	code, fwPreview := request("GET", "firewalls/"+fw["id"].(string)+"/preview", nil, nil, "")
	if code != 200 || !bytes.Contains(fwPreview, []byte("harbor-managed-v1")) {
		t.Fatal(code, string(fwPreview))
	}
	post("firewalls/"+fw["id"].(string)+"/archive", map[string]bool{"archived": true}, 200, nil, "")
	policy["admin_cidrs"] = []string{"0.0.0.0/0"}
	post(firewallPath, policy, 400, nil, "")
	code, _ = request("GET", "tasks?limit=0", nil, nil, "")
	if code != 400 {
		t.Fatal("invalid pagination accepted")
	}
	// A bounded single snapshot excludes secret/config bodies, has true counts, and supports 304.
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/snapshot", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	var snap map[string]any
	if e = json.NewDecoder(res.Body).Decode(&snap); e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	tag := res.Header.Get("ETag")
	if tag == "" {
		t.Fatal("missing snapshot ETag")
	}
	collections := snap["collections"].(map[string]any)
	if _, ok := collections["configs"].([]any)[0].(map[string]any)["spec"]; ok {
		t.Fatal("heavy config body in snapshot")
	}
	if snap["summary"].(map[string]any)["hosts"].(float64) != 2 {
		t.Fatal("incorrect total counts")
	}
	req.Header.Set("If-None-Match", tag)
	res, e = http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 304 {
		t.Fatal("unchanged snapshot not 304", res.StatusCode)
	}
	// Cancel pending work atomically; a running/completed task cannot be cancelled.
	cancelInstance := post("instances", map[string]any{"name": "cancel target", "host_id": other["id"], "node_id": frp2["id"], "listen_port": 29000}, 201, nil, "")
	cancelCfg := post("instances/"+cancelInstance["id"].(string)+"/configs", map[string]any{"outbound": "block"}, 201, nil, "")
	cancelJob := post("instances/"+cancelInstance["id"].(string)+"/deploy", map[string]any{"config_id": cancelCfg["id"]}, 202, nil, "cancel-0001")
	post("tasks/"+cancelJob["id"].(string)+"/cancel", map[string]any{}, 200, nil, "")
	post("tasks/"+cancelJob["id"].(string)+"/cancel", map[string]any{}, 409, nil, "")
	post("agent/heartbeat", nil, 401, host, "")
	status, body := request("PATCH", "nodes/"+node["id"].(string), map[string]string{"name": "Renamed proxy"}, nil, "")
	if status != 200 {
		t.Fatal(status, string(body))
	}
	status, _ = request("PATCH", "nodes/"+node["id"].(string), map[string]string{"engine": "frp"}, nil, "")
	if status != 409 {
		t.Fatal("referenced engine changed", status)
	}
	status, _ = request("DELETE", "instances/"+instance["id"].(string), nil, nil, "")
	if status != 409 {
		t.Fatal("history destroyed", status)
	}
	status, _ = request("DELETE", "hosts/"+host["id"].(string), nil, nil, "")
	if status != 409 {
		t.Fatal("referenced host removed", status)
	}
	orphan := post("nodes", map[string]any{"name": "unused", "engine": "sing-box"}, 201, nil, "")
	status, _ = request("DELETE", "nodes/"+orphan["id"].(string), nil, nil, "")
	if status != 200 {
		t.Fatal("unused inventory not removable", status)
	}
	t.Log("Verified persistence, migrations, authorization, tenant scope, relation/port constraints, FRP snapshots, idempotency, concurrent claims, retry exhaustion, stale leases, token rotation and audit")
}

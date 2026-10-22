package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"vps-control/internal/store"
)

func TestEnrollmentIntegration(t *testing.T) {
	rawURL := os.Getenv("TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, rawURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	schema := "enroll_" + ID()
	if _, e = pool.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	u, _ := url.Parse(rawURL)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, e := store.Open(ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	dir := t.TempDir()
	file := filepath.Join(dir, "linux-amd64.zip")
	os.WriteFile(file, []byte("test-package"), 0600)
	sum, _ := packageDigest(file)
	manifest, _ := json.Marshal(map[string]agentPackage{"linux-amd64": {sum, 12}})
	os.WriteFile(filepath.Join(dir, "manifest.json"), manifest, 0600)
	s := &Server{DB: db, AdminToken: strings.Repeat("a", 32), MasterKey: bytes.Repeat([]byte{1}, 32), AgentReleaseDir: dir}
	handler := s.Handler()
	call := func(method, path string, body any, auth bool) (int, map[string]any) {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/api/v1/"+path, bytes.NewReader(b))
		if auth {
			r.Header.Set("Authorization", "Bearer "+s.AdminToken)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		var result map[string]any
		json.Unmarshal(w.Body.Bytes(), &result)
		return w.Code, result
	}
	want := func(method, path string, body any, status int) map[string]any {
		t.Helper()
		code, b := call(method, path, body, true)
		if code != status {
			t.Fatalf("%s %s: %d %#v", method, path, code, b)
		}
		return b
	}
	h := want("POST", "hosts", map[string]string{"name": "NAT host"}, 201)
	id := h["id"].(string)
	want("PUT", "site", map[string]any{"domain": "vps.example.com", "https": true}, 200)
	path := "hosts/" + id + "/enrollment"
	if code, _ := call("POST", path, nil, false); code != 401 {
		t.Fatal("unauthenticated enrollment")
	}
	issue := func() string {
		t.Helper()
		b := want("POST", path, nil, 201)
		cmd := b["command"].(string)
		if strings.Contains(cmd, s.AdminToken) || strings.Contains(cmd, h["agent_token"].(string)) {
			t.Fatal("long-term token disclosed")
		}
		tickets := regexp.MustCompile(`[a-f0-9]{64}`).FindAllString(cmd, -1)
		if len(tickets) != 1 {
			t.Fatal("ticket command malformed")
		}
		return tickets[0]
	}
	first := issue()
	second := issue()
	if code, _ := call("POST", "agent/enroll", map[string]string{"ticket": first}, false); code != 401 {
		t.Fatal("regenerated old ticket accepted")
	}
	var mu sync.Mutex
	success := 0
	var claimed map[string]any
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, result := call("POST", "agent/enroll", map[string]string{"ticket": second}, false)
			mu.Lock()
			defer mu.Unlock()
			if code == 200 {
				success++
				claimed = result
			} else if code != 401 {
				t.Errorf("concurrent claim %d", code)
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("single-use success count: %d", success)
	}
	r := httptest.NewRequest("POST", "/api/v1/agent/heartbeat", strings.NewReader(`{"mode":"simulation"}`))
	r.Header.Set("X-Host-ID", id)
	r.Header.Set("Authorization", "Bearer "+claimed["agent_token"].(string))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("issued Agent credential rejected", w.Code)
	}
	if want("GET", path, nil, 200)["state"] != "connected" {
		t.Fatal("heartbeat not reflected")
	}
	third := issue()
	if want("GET",path,nil,200)["state"]!="waiting" {t.Fatal("reconnect command hidden by old heartbeat")}
	var currentHash string;if e=db.QueryRow(ctx,"SELECT agent_hash FROM hosts WHERE tenant_id=$1 AND id=$2",tenant,id).Scan(&currentHash);e!=nil||currentHash!=Hash(claimed["agent_token"].(string)){t.Fatal("generating ticket prematurely revoked active Agent")}
	want("POST", path+"/revoke", nil, 200)
	if code, _ := call("POST", "agent/enroll", map[string]string{"ticket": third}, false); code != 401 {
		t.Fatal("revoked ticket accepted")
	}
	fourth := issue()
	if _, e = db.Exec(ctx, "UPDATE agent_enrollments SET expires_at=now()-interval '1 second' WHERE token_hash=$1", Hash(fourth)); e != nil {
		t.Fatal(e)
	}
	if code, _ := call("POST", "agent/enroll", map[string]string{"ticket": fourth}, false); code != 401 {
		t.Fatal("expired ticket accepted")
	}
	var stored string
	db.QueryRow(ctx, "SELECT token_hash FROM agent_enrollments WHERE token_hash=$1", Hash(fourth)).Scan(&stored)
	if stored == fourth {
		t.Fatal("plaintext ticket persisted")
	}
	want("GET", "agent/releases/linux-amd64", nil, 200)
	if code, _ := call("GET", "agent/releases/windows-amd64", nil, false); code != 400 {
		t.Fatal("unexpected platform allowed")
	}
}

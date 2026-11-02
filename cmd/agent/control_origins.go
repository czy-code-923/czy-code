package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
	"vps-control/internal/controlorigin"
)

type controlConnection struct {
	mu                          sync.RWMutex
	probe                       sync.Mutex
	client                      *http.Client
	base, host, token, stateDir string
	origins                     []string
}

func newControlConnection(client *http.Client, base, host, token, stateDir string) *controlConnection {
	c := &controlConnection{client: client, base: base, host: host, token: token, stateDir: stateDir, origins: []string{base}}
	// Persisted addresses contain no secrets. Every recovered address must still
	// prove the identity before receiving a token; a modified file cannot redirect it.
	if root, err := os.OpenRoot(stateDir); err == nil {
		defer root.Close()
		if f, e := root.Open("control-origins.json"); e == nil {
			defer f.Close()
			var saved struct {
				Host    string   `json:"host"`
				Origins []string `json:"origins"`
			}
			if json.NewDecoder(io.LimitReader(f, 4096)).Decode(&saved) == nil && saved.Host == host {
				for _, raw := range saved.Origins {
					if u, e := controlorigin.Normalize(raw, false); e == nil && len(c.origins) < 5 && u != base {
						c.origins = append(c.origins, u)
					}
				}
			}
		}
	}
	return c
}

func (c *controlConnection) origin() string { c.mu.RLock(); defer c.mu.RUnlock(); return c.base }

func (c *controlConnection) verifies(ctx context.Context, origin string) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return false
	}
	nonce := hex.EncodeToString(random[:])
	body, _ := json.Marshal(map[string]string{"host_id": c.host, "nonce": nonce, "origin": origin})
	req, err := http.NewRequestWithContext(ctx, "POST", origin+"/api/v1/agent/verify-origin", bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.client.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	var result struct {
		Proof string `json:"proof"`
	}
	return res.StatusCode == 200 && json.NewDecoder(io.LimitReader(res.Body, 2048)).Decode(&result) == nil && hmac.Equal([]byte(result.Proof), []byte(controlorigin.Proof(controlorigin.TokenHash(c.token), nonce, origin)))
}

func (c *controlConnection) remember() {
	if c.stateDir == "" {
		return
	}
	root, err := os.OpenRoot(c.stateDir)
	if err != nil {
		return
	}
	defer root.Close()
	c.mu.RLock()
	body, _ := json.Marshal(map[string]any{"host": c.host, "origins": append([]string{c.base}, c.origins...)})
	c.mu.RUnlock()
	_ = root.Remove("control-origins.new") // Private, non-secret staging file after an interrupted write.
	f, err := root.OpenFile("control-origins.new", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	_, err = f.Write(body)
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil && ce == nil {
		err = root.Rename("control-origins.new", "control-origins.json")
	}
	if err != nil || ce != nil {
		_ = root.Remove("control-origins.new")
	}
}

// Update is called only with an authenticated heartbeat response. Failed probes
// retain the working origin. Old origins remain recovery fallbacks for this process.
func (c *controlConnection) update(ctx context.Context, candidates []string) {
	if len(candidates) == 0 || len(candidates) > 4 || !c.probe.TryLock() {
		return
	}
	defer c.probe.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	valid := []string{}
	for _, raw := range candidates {
		u, e := controlorigin.Normalize(raw, false)
		if e != nil {
			return
		}
		valid = append(valid, u)
	}
	c.mu.Lock()
	old := append([]string{c.base}, c.origins...)
	c.origins = append([]string{}, valid...)
	for _, u := range old {
		found := false
		for _, v := range c.origins {
			if u == v {
				found = true
			}
		}
		if !found && len(c.origins) < 5 {
			c.origins = append(c.origins, u)
		}
	}
	c.mu.Unlock()
	for _, u := range valid {
		if u == c.origin() {
			c.remember()
			return
		}
		if c.verifies(ctx, u) {
			c.mu.Lock()
			c.base = u
			c.mu.Unlock()
			c.client.CloseIdleConnections()
			c.remember()
			return
		}
	}
	c.remember()
}

func (c *controlConnection) recover(ctx context.Context, failed string) {
	if !c.probe.TryLock() {
		return
	}
	defer c.probe.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c.mu.RLock()
	origins := append([]string{}, c.origins...)
	c.mu.RUnlock()
	for _, u := range origins {
		if u != failed && c.verifies(ctx, u) {
			c.mu.Lock()
			c.base = u
			c.mu.Unlock()
			c.client.CloseIdleConnections()
			c.remember()
			return
		}
	}
}

func (c *controlConnection) call(ctx context.Context, path string, body any) ([]byte, int, error) {
	base := c.origin()
	data, status, err := controlCaller(c.client, base, c.host, c.token)(ctx, path, body)
	if err != nil && ctx.Err() == nil {
		c.recover(ctx, base)
	}
	return data, status, err // Never replay an ambiguous mutating operation.
}

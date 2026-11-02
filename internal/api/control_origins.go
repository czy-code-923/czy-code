package api

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"sync"
	"time"
	"vps-control/internal/controlorigin"
)

var originProofLimit struct {
	sync.Mutex
	window time.Time
	count  int
}

// A fresh nonce prevents replay; the Agent verifies this proof without sending
// its bearer credential to the proposed new origin. agent_hash is never exposed.
func (s *Server) verifyControlOrigin(w http.ResponseWriter, r *http.Request) error {
	originProofLimit.Lock()
	if time.Since(originProofLimit.window) > time.Second {
		originProofLimit.window = time.Now()
		originProofLimit.count = 0
	}
	originProofLimit.count++
	limited := originProofLimit.count > 60
	originProofLimit.Unlock()
	if limited {
		return problem{429, "origin verification busy"}
	}
	var b struct {
		Host   string `json:"host_id"`
		Nonce  string `json:"nonce"`
		Origin string `json:"origin"`
	}
	if e := decodeBounded(w, r, &b, 1024); e != nil {
		return e
	}
	if len(b.Host) != 32 || !ticketPattern.MatchString(b.Nonce) {
		return bad("invalid origin challenge")
	}
	origin, err := controlorigin.Normalize(b.Origin, false)
	if err != nil {
		return bad("invalid origin challenge address")
	}
	allowed, err := s.agentOrigins(r)
	if err != nil {
		return err
	}
	// Bind the proof to an explicitly configured origin to prevent challenge relay.
	permitted := false
	for _, candidate := range allowed {
		if candidate == origin {
			permitted = true
		}
	}
	if current, e := s.managementOrigin(r); e == nil && current == origin {
		permitted = true
	}
	if !permitted {
		return problem{403, "origin not configured on this control server"}
	}
	var hash string
	if e := s.DB.QueryRow(r.Context(), `SELECT agent_hash FROM hosts WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL AND retire_state=''`, tenant, b.Host).Scan(&hash); e != nil {
		return e
	}
	send(w, 200, map[string]string{"proof": controlorigin.Proof(hash, b.Nonce, origin)})
	return nil
}

func (s *Server) agentOrigins(r *http.Request) ([]string, error) {
	var raw json.RawMessage
	if e := s.DB.QueryRow(r.Context(), `SELECT agent_origins FROM site_settings WHERE tenant_id=$1`, tenant).Scan(&raw); e != nil {
		if e == pgx.ErrNoRows {
			return []string{}, nil
		}
		return nil, e
	}
	origins := []string{}
	if e := json.Unmarshal(raw, &origins); e != nil {
		return nil, e
	}
	if len(origins) == 0 {
		if origin, e := s.managementOrigin(r); e == nil {
			if u, e := controlorigin.Normalize(origin, false); e == nil {
				origins = append(origins, u)
			}
		}
	}
	return origins, nil
}

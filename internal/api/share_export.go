package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/skip2/go-qrcode"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"vps-control/internal/live"
)

func (s *Server) exportURL(r *http.Request) (string, error) {
	var sealed []byte
	if err := s.DB.QueryRow(r.Context(), `SELECT sealed_token FROM subscriptions WHERE tenant_id=$1 AND id=$2 AND revoked_at IS NULL AND expires_at>now()`, tenant, r.PathValue("id")).Scan(&sealed); err != nil {
		return "", err
	}
	token, err := live.Open(s.MasterKey, sealed)
	if err != nil {
		return "", err
	}
	origin, err := s.publicOrigin(r)
	if err != nil {
		return "", err
	}
	q := r.URL.Query()
	values := url.Values{}
	for _, key := range []string{"client", "format", "policy"} {
		if v := q.Get(key); v != "" && v != "auto" {
			values.Set(key, v)
		}
	}
	if values.Get("client") != "" {
		if _, ok := clientByID(values.Get("client")); !ok {
			return "", bad("unsupported client")
		}
	}
	result := origin + "/s/" + string(token)
	if len(values) > 0 {
		result += "?" + values.Encode()
	}
	return result, nil
}
func (s *Server) exportRecorder(r *http.Request, client string) (*httptest.ResponseRecorder, error) {
	raw, err := s.exportURL(r)
	if err != nil {
		return nil, err
	}
	if client != "" {
		u, _ := url.Parse(raw)
		q := u.Query()
		q.Set("client", client)
		q.Set("policy", "nodes-only")
		u.RawQuery = q.Encode()
		raw = u.String()
	}
	// Generic HWID subscriptions do not represent an already deployed identity.
	var strict bool
	if err = s.DB.QueryRow(r.Context(), `SELECT COALESCE((SELECT u.device_limit_mode='hwid' AND `+deviceLimitSQL+`>0 FROM proxy_users u WHERE u.tenant_id=s.tenant_id AND u.id=s.user_id),false) AND s.device_id IS NULL FROM subscriptions s WHERE s.tenant_id=$1 AND s.id=$2`, tenant, r.PathValue("id")).Scan(&strict); err != nil {
		return nil, err
	}
	if strict {
		return nil, problem{409, "请先由 HWID 客户端注册设备，再使用设备专属订阅；API 不会创建虚假设备"}
	}
	req := httptest.NewRequest("GET", raw, nil).WithContext(context.WithValue(r.Context(), subscriptionInspectionKey{}, true))
	u, _ := url.Parse(raw)
	req.SetPathValue("token", strings.TrimPrefix(u.Path, "/s/"))
	req.Header.Set("User-Agent", r.UserAgent())
	recorder := httptest.NewRecorder()
	if err = s.serveSubscription(recorder, req); err != nil {
		return nil, err
	}
	return recorder, nil
}

type sharedNode struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URI  string `json:"uri"`
}

func (s *Server) exportNodes(r *http.Request) ([]sharedNode, error) {
	recorder, err := s.exportRecorder(r, "nekobox-uri")
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(recorder.Body.String()))
	if err != nil {
		return nil, err
	}
	nodes := []sharedNode{}
	for _, line := range strings.Fields(string(raw)) {
		name := ""
		u, e := url.Parse(line)
		if e != nil {
			continue
		}
		name = u.Fragment
		if u.Scheme == "vmess" {
			if b, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "vmess://")); e == nil {
				var obj map[string]any
				json.Unmarshal(b, &obj)
				name = text(obj["ps"])
			}
		}
		nodes = append(nodes, sharedNode{Hash(line)[:16], name, line})
	}
	return nodes, nil
}
func (s *Server) shareExport(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	raw, err := s.exportURL(r)
	if err != nil {
		return err
	}
	view := r.PathValue("export")
	switch view {
	case "share":
		send(w, 200, map[string]any{"url": raw, "qr_url": "/api/v1/subscriptions/" + r.PathValue("id") + "/qr?" + r.URL.RawQuery, "qr_payload": raw, "client": r.URL.Query().Get("client"), "private": true})
		return nil
	case "content":
		recorder, err := s.exportRecorder(r, "")
		if err != nil {
			return err
		}
		for k, v := range recorder.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(recorder.Code)
		_, err = w.Write(recorder.Body.Bytes())
		return err
	case "nodes":
		nodes, err := s.exportNodes(r)
		if err != nil {
			return err
		}
		send(w, 200, nodes)
		return nil
	case "qr":
		if r.URL.Query().Get("kind") == "node" {
			nodes, err := s.exportNodes(r)
			if err != nil {
				return err
			}
			found := false
			for _, node := range nodes {
				if node.ID == r.URL.Query().Get("node_id") {
					raw = node.URI
					found = true
					break
				}
			}
			if !found {
				return problem{404, "node unavailable"}
			}
		}
		png, err := qrcode.Encode(raw, qrcode.Medium, 320)
		if err != nil {
			return bad("QR payload too long")
		}
		w.Header().Set("Content-Type", "image/png")
		_, err = w.Write(png)
		return err
	}
	return problem{404, "unknown export"}
}

// agent-cleanup is a local privileged janitor. Its root-owned manifest contains
// fixed installation paths; the controller can supply only an uninstall ID.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type manifest struct {
	Cleaned bool     `json:"cleaned"`
	Env     string   `json:"env"`
	Marker  string   `json:"marker"`
	Paths   []string `json:"paths"`
}

func safePath(value string) bool {
	clean := filepath.Clean(value)
	if clean != value || !filepath.IsAbs(value) {
		return false
	}
	allowed := []string{"/opt/harbor-agent", "/etc/harbor-agent", "/var/lib/harbor-agent", "/var/lib/harbor/live", "/var/lib/harbor/agent", "/etc/harbor/agent.env", "/opt/harbor/bin/harbor-agent", "/opt/harbor/harbor-agent", "/opt/harbor/engines"}
	if runtime.GOOS == "windows" {
		allowed = []string{filepath.Join(os.Getenv("ProgramData"), "StarhubAgent")}
	}
	for _, a := range allowed {
		if clean == a || strings.HasPrefix(clean, a+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
func removeOwned(path string) error {
	if !safePath(path) {
		return errors.New("path outside Agent installation")
	}
	for p := filepath.Dir(path); p != filepath.Dir(p); p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e == nil && st.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink parent refused")
		}
	}
	st, e := os.Lstat(path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return os.Remove(path)
	}
	// RemoveAll does not traverse symlink entries. Every top-level path is local and allowlisted.
	return os.RemoveAll(path)
}
func run() error {
	config := "/etc/harbor-agent-cleanup.json"
	if runtime.GOOS == "windows" {
		config = filepath.Join(os.Getenv("ProgramData"), "StarhubCleanup", "manifest.json")
	}
	raw, e := os.ReadFile(config)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if e = trustedManifest(config); e != nil {
		return e
	}
	var m manifest
	if e = json.Unmarshal(raw, &m); e != nil {
		return e
	}
	if !safePath(m.Env) || !safePath(m.Marker) || len(m.Paths) > 24 {
		return errors.New("invalid local cleanup manifest")
	}
	for _, p := range m.Paths {
		if !safePath(p) || p == m.Env || strings.HasPrefix(m.Env, p+string(filepath.Separator)) || p == m.Marker || strings.HasPrefix(m.Marker, p+string(filepath.Separator)) {
			return errors.New("invalid removal path")
		}
	}
	raw, e = os.ReadFile(m.Marker)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil || len(raw) > 512 {
		return errors.New("invalid marker")
	}
	var marker struct {
		ID string `json:"retire_id"`
	}
	if json.Unmarshal(raw, &marker) != nil || len(marker.ID) != 32 || strings.Trim(marker.ID, "0123456789abcdef") != "" {
		return errors.New("invalid uninstall ID")
	}
	env, e := os.ReadFile(m.Env)
	if e != nil {
		return e
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(env), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			values[k] = v
		}
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	call := func(action string) error {
		b, _ := json.Marshal(marker)
		req, e := http.NewRequest("POST", strings.TrimRight(values["CONTROL_URL"], "/")+"/api/v1/agent/retirement/"+action, bytes.NewReader(b))
		if e != nil {
			return e
		}
		req.Header.Set("Authorization", "Bearer "+values["AGENT_TOKEN"])
		req.Header.Set("X-Host-ID", values["HOST_ID"])
		req.Header.Set("Content-Type", "application/json")
		res, e := client.Do(req)
		if e != nil {
			return errors.New("cleanup control unavailable")
		}
		defer res.Body.Close()
		io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		if res.StatusCode != 200 {
			return fmt.Errorf("cleanup acknowledgement HTTP %d", res.StatusCode)
		}
		return nil
	}
	if !m.Cleaned {
		if e = call("begin"); e != nil {
			return e
		}
		if runtime.GOOS == "windows" {
			if e = exec.Command("schtasks.exe", "/End", "/TN", "StarhubAgent").Run(); e != nil {
				return e
			}
			time.Sleep(time.Second)
			if e = exec.Command("schtasks.exe", "/Delete", "/TN", "StarhubAgent", "/F").Run(); e != nil {
				return e
			}
		} else {
			if e = exec.Command("/usr/bin/systemctl", "disable", "--now", "harbor-agent.service").Run(); e != nil {
				return e
			}
		}
		for _, p := range m.Paths {
			if e = removeOwned(p); e != nil {
				return e
			}
		}
		m.Cleaned = true
		state, _ := json.Marshal(m)
		tmp := config + ".tmp"
		if e = os.WriteFile(tmp, state, 0600); e != nil {
			return e
		}
		if e = os.Rename(tmp, config); e != nil {
			return e
		}
	}
	// Keep the marker and credential until the controller confirms the purge.
	if e = call("ack"); e != nil {
		return e
	}
	if e = os.Remove(m.Env); e != nil && !os.IsNotExist(e) {
		return e
	}
	os.Remove(m.Marker)
	os.Remove(filepath.Dir(m.Env))
	os.Remove(filepath.Dir(m.Marker))
	if runtime.GOOS != "windows" {
		os.Remove("/etc/systemd/system/harbor-agent.service")
		exec.Command("/usr/bin/systemctl", "daemon-reload").Run()
	}
	os.Remove(config)
	if runtime.GOOS != "windows" {
		exec.Command("/usr/bin/systemctl", "disable", "--now", "harbor-agent-cleanup.timer").Run()
		for _, p := range []string{"/etc/systemd/system/harbor-agent-cleanup.service", "/etc/systemd/system/harbor-agent-cleanup.timer", "/usr/local/libexec/harbor-agent-cleanup"} {
			os.Remove(p)
		}
		exec.Command("/usr/bin/systemctl", "daemon-reload").Run()
	}
	fmt.Println("Agent installation removed; control plane and unrelated services retained")
	return nil
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}

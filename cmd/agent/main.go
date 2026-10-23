// Agent defaults to simulation. Opt-in staging validates and writes private config files only.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
	"vps-control/internal/executor"
	"vps-control/internal/manifest"
)

func run(ctx context.Context) error {
	base := strings.TrimRight(os.Getenv("CONTROL_URL"), "/")
	if base == "" {
		base = "http://127.0.0.1:8080"
	}
	u, e := url.Parse(base)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return errors.New("invalid CONTROL_URL; use an origin without path")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1" || os.Getenv("ALLOW_COMPOSE_HTTP") == "1" && u.Hostname() == "api")) {
		return errors.New("HTTPS required except loopback or explicit Compose development network")
	}
	host, token := os.Getenv("HOST_ID"), os.Getenv("AGENT_TOKEN")
	if !manifest.ValidID(host) || len(token) < 32 {
		return errors.New("HOST_ID and AGENT_TOKEN required")
	}
	mode := os.Getenv("AGENT_MODE")
	if mode == "" {
		mode = "simulation"
	}
	if mode != "simulation" && mode != "staging" {
		return errors.New("invalid AGENT_MODE")
	}
	var stager *executor.Stager
	if mode == "staging" {
		binaries := map[string]executor.Binary{}
		for engine, prefix := range map[string]string{"sing-box": "SING_BOX", "frps": "FRPS", "frpc": "FRPC"} {
			binaries[engine] = executor.Binary{Path: os.Getenv(prefix + "_BINARY"), SHA256: os.Getenv(prefix + "_SHA256")}
		}
		stager, e = executor.Open(os.Getenv("STATE_DIR"), os.Getenv("ENGINE_SECRET"), executor.NativeValidator{Binaries: binaries})
		if e != nil {
			return e
		}
		defer stager.Close()
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	call := func(ctx context.Context, path string, body any) ([]byte, int, error) {
		b, _ := json.Marshal(body)
		req, e := http.NewRequestWithContext(ctx, "POST", base+"/api/v1/agent/"+path, bytes.NewReader(b))
		if e != nil {
			return nil, 0, e
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Host-ID", host)
		req.Header.Set("Content-Type", "application/json")
		res, e := client.Do(req)
		if e != nil {
			return nil, 0, errors.New("control connection failed")
		}
		defer res.Body.Close()
		data, e := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
		if len(data) > 2<<20 {
			return nil, res.StatusCode, errors.New("control response too large")
		}
		if res.StatusCode >= 300 {
			return nil, res.StatusCode, fmt.Errorf("control HTTP %d", res.StatusCode)
		}
		return data, res.StatusCode, e
	}
	if os.Getenv("ENABLE_LIVE") == "1" {
		liveCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		liveErrors := make(chan error, 1)
		go func() {
			err := runLive(liveCtx, call)
			if err != nil {
				log.Print("live runner initialization failed: ", err)
			}
			liveErrors <- err
		}()
		defer func() {
			cancel()
			if err := <-liveErrors; err != nil {
				log.Print("live runner stopped: ", err)
			}
		}()
	}
	idle := 0
	nextHeartbeat := time.Time{}
	for {
		delay := time.Second
		e = nil
		if time.Now().After(nextHeartbeat) {
			addresses := []string{}
			if items, err := net.InterfaceAddrs(); err == nil {
				for _, item := range items {
					prefix, err := netip.ParsePrefix(item.String())
					if err != nil {
						continue
					}
					a := prefix.Addr()
					if a.IsUnspecified() || a.IsLoopback() || a.IsMulticast() || a.IsLinkLocalUnicast() {
						continue
					}
					addresses = append(addresses, a.String())
					if len(addresses) == 16 {
						break
					}
				}
			}
			_, _, e = call(ctx, "heartbeat", map[string]any{"mode": mode, "version": "0.7.0", "platform": runtime.GOOS + "/" + runtime.GOARCH, "addresses": addresses})
			if e == nil {
				nextHeartbeat = time.Now().Add(30 * time.Second)
			}
		}
		if e != nil {
			log.Print(e)
			idle++
			delay = idleDelay(idle, 60*time.Second)
		} else {
			data, status, err := call(ctx, "claim", nil)
			if err != nil {
				idle++
				delay = idleDelay(idle, 60*time.Second)
				log.Print(err)
			} else if status == 204 {
				idle++
				delay = idleDelay(idle, 15*time.Second)
			} else if status == 200 {
				idle = 0
				var task executor.Task
				if json.Unmarshal(data, &task) != nil || !manifest.ValidID(task.ID) || !manifest.ValidID(task.Lease) {
					log.Print("invalid task envelope")
				} else {
					_, validationError := manifest.Verify(task.Spec, task.Checksum)
					sum := ""
					ok := validationError == nil
					switch task.Mode {
					case "simulation":
					case "staging":
						if stager == nil {
							ok = false
						} else if ok {
							sum, err = stager.Stage(ctx, task, func(guard context.Context) error {
								_, _, err := call(guard, "tasks/"+task.ID+"/renew", map[string]string{"lease_token": task.Lease})
								return err
							})
							ok = err == nil
						}
					default:
						ok = false
					}
					body := map[string]any{"lease_token": task.Lease, "success": ok, "artifact_checksum": sum}
					// Completion is idempotent: safely retry an ambiguous lost acknowledgement.
					_, _, e = call(ctx, "tasks/"+task.ID+"/complete", body)
					if e != nil {
						_, _, e = call(ctx, "tasks/"+task.ID+"/complete", body)
					}
					if e != nil {
						log.Print(e)
					} else {
						log.Printf("task %s completed mode=%s success=%t (no services started)", task.ID, task.Mode, ok)
					}
				}
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
func idleDelay(n int, cap time.Duration) time.Duration {
	delay := 2 * time.Second
	for i := 1; i < n && delay < cap; i++ {
		delay *= 2
	}
	if delay > cap {
		return cap
	}
	return delay
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if e := run(ctx); e != nil {
		log.Fatal(e)
	}
}

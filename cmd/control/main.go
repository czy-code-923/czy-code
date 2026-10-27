package main

import (
	"context"
	"encoding/hex"
	"log"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
	"vps-control/internal/api"
	"vps-control/internal/live"
	"vps-control/internal/store"
)

func main() {
	token := os.Getenv("ADMIN_TOKEN")
	if len(token) < 32 {
		log.Fatal("ADMIN_TOKEN must contain at least 32 bytes")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	db, e := store.Open(startup, os.Getenv("DATABASE_URL"))
	if e != nil {
		log.Fatal(e)
	}
	defer db.Close()
	if origin := os.Getenv("PUBLIC_ORIGIN"); origin != "" {
		if !live.Origin(origin) {
			log.Fatal("invalid PUBLIC_ORIGIN")
		}
		u, _ := url.Parse(origin)
		domain := ""
		if _, e := netip.ParseAddr(u.Hostname()); e != nil {
			if u.Port() != "" {
				log.Fatal("domain origins with custom ports require a reverse proxy")
			}
			domain = u.Hostname()
		}
		if _, e = db.Exec(startup, `INSERT INTO site_settings(tenant_id,domain,https,ip_origin) VALUES('self',$1,$2,$3) ON CONFLICT DO NOTHING`, domain, u.Scheme == "https", origin); e != nil {
			log.Fatal("cannot initialize public origin")
		}
	}

	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	key, err := hex.DecodeString(os.Getenv("MASTER_KEY"))
	if os.Getenv("MASTER_KEY") != "" && (err != nil || len(key) != 32) {
		log.Fatal("MASTER_KEY must be 64 hexadecimal characters")
	}
	control := &api.Server{DB: db, AdminToken: token, MasterKey: key, AgentReleaseDir: os.Getenv("AGENT_RELEASE_DIR")}
	if e := control.EnsureDefaultClientPolicy(startup); e != nil {
		log.Fatal("cannot initialize client policy template: ", e)
	}
	go control.RunNotifications(ctx)
	shareAddr := os.Getenv("SHARE_ADDR")
	if shareAddr == "" {
		shareAddr = "127.0.0.1:8091"
	}
	shareSrv := &http.Server{Addr: shareAddr, Handler: control.ShareHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shareSrv.Shutdown(c)
	}()
	go func() {
		if e := shareSrv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			log.Print("subscription listener failed")
			stop()
		}
	}()
	if webDir := os.Getenv("WEB_DIR"); webDir != "" {
		go func() {
			if e := webServer(ctx, webDir); e != nil {
				log.Print("native web listener failed: ", e)
				stop()
			}
		}()
	}
	go func() {
		tick := time.NewTicker(5 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				reapCtx, c := context.WithTimeout(ctx, 4*time.Second)
				e := control.Reap(reapCtx)
				c()
				if e != nil && ctx.Err() == nil {
					log.Print("task lease maintenance failed")
				}
			}
		}
	}()
	srv := &http.Server{Addr: addr, Handler: control.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() {
		<-ctx.Done()
		shutdown, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		srv.Shutdown(shutdown)
	}()
	log.Printf("control plane listening on %s (managed service activation requires opt-in Agent)", addr)
	if e = srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		log.Fatal(e)
	}
}

package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func webServer(ctx context.Context, dir string) error {
	if !filepath.IsAbs(dir) {
		return errors.New("WEB_DIR must be absolute")
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		return e
	}
	defer root.Close()
	if _, e = root.Stat("index.html"); e != nil {
		return errors.New("WEB_DIR must contain built index.html")
	}
	target, e := localListenerURL("HTTP_ADDR", "127.0.0.1:8080")
	if e != nil {
		return e
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	baseDirector := proxy.Director
	proxy.Director = func(r *http.Request) {
		// This is a same-origin UI gateway. Other origins still reach the API's
		// explicit CORS allowlist; no new cross-origin permission is created.
		if sameWebOrigin(r) {
			r.Header.Del("Origin")
		}
		baseDirector(r)
	}
	files := http.FileServerFS(root.FS())
	mux := http.NewServeMux()
	mux.Handle("/api/", proxy)
	shareTarget, e := localListenerURL("SHARE_ADDR", "127.0.0.1:8091")
	if e != nil {
		return e
	}
	mux.Handle("/s/", shareProxy(shareTarget))
	mux.Handle("/healthz", proxy)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "method not allowed", 405)
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, e := root.Stat(p); e != nil {
			copy := r.Clone(r.Context())
			u := *r.URL
			u.Path = "/"
			copy.URL = &u
			files.ServeHTTP(w, copy)
			return
		}
		files.ServeHTTP(w, r)
	})
	addr := os.Getenv("WEB_ADDR")
	if addr == "" {
		addr = "127.0.0.1:5173"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil || net.ParseIP(host) == nil {
		return errors.New("WEB_ADDR requires IP:port")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1024 || n > 65535 || n == 8080 || n == 8091 {
		return errors.New("invalid WEB_ADDR port")
	}
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	e = srv.ListenAndServe()
	if e == http.ErrServerClosed {
		return nil
	}
	return e
}

func sameWebOrigin(r *http.Request) bool {
	u, err := url.Parse(r.Header.Get("Origin"))
	if err != nil || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return u.Scheme == scheme && strings.EqualFold(u.Host, r.Host)
}

func localListenerURL(key, fallback string) (*url.URL, error) {
	addr := os.Getenv(key)
	if addr == "" {
		addr = fallback
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, errors.New("invalid " + key + " listener")
	}
	ip := net.ParseIP(host)
	if host == "" || ip != nil && ip.IsUnspecified() {
		host = "127.0.0.1"
	} else if ip == nil {
		return nil, errors.New(key + " requires IP:port")
	}
	return &url.URL{Scheme: "http", Host: net.JoinHostPort(host, port)}, nil
}

// Preserve only the origin asserted by a local, trusted gateway. Public peers
// cannot forge another client's subscription slot with X-Real-IP.
func shareProxy(target *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		peer := net.ParseIP(host)
		asserted := net.ParseIP(r.Header.Get("X-Real-IP"))
		origin := peer
		if peer != nil && peer.IsLoopback() && asserted != nil && !asserted.IsUnspecified() {
			origin = asserted
		}
		r.Header.Del("X-Real-IP")
		if origin != nil {
			r.Header.Set("X-Real-IP", origin.String())
		}
		director(r)
	}
	return proxy
}

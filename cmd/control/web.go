package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
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
	target, _ := url.Parse("http://127.0.0.1:8080")
	proxy := httputil.NewSingleHostReverseProxy(target)
	files := http.FileServerFS(root.FS())
	mux := http.NewServeMux()
	mux.Handle("/api/", proxy)
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
	srv := &http.Server{Addr: "127.0.0.1:5173", Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
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

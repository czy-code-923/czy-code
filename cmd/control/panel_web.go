package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Recovery serves only the existing web UI and authenticated API proxy. It does
// not initialize a database, issue credentials or expose a raw control listener.
func runPanelWeb() {
	flags := flag.NewFlagSet("panel-web", flag.ExitOnError)
	addr := flags.String("addr", "127.0.0.1:18081", "Web proxy bind address")
	dir := flags.String("dir", "", "Existing absolute web directory")
	ttl := flags.Duration("ttl", 30*time.Minute, "Time until automatic shutdown; 0 explicitly means persistent")
	flags.Parse(os.Args[2:])
	if *ttl < 0 || *ttl > 24*time.Hour {
		log.Fatal("ttl must be 0..24h")
	}
	os.Setenv("WEB_ADDR", *addr)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *ttl > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *ttl)
		defer cancel()
	}
	if err := webServer(ctx, *dir); err != nil {
		log.Fatal(err)
	}
}

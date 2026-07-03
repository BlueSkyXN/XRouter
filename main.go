package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	configPath := flag.String("config", getenvDefault("XROUTER_CONFIG", "config.example.json"), "path to XRouter JSON config")
	showVersion := flag.Bool("version", false, "print version information and exit")
	flag.Parse()
	if *showVersion {
		printVersion()
		return
	}
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	srv := NewServer(cfg)
	httpServer := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           srv.routes(),
		ReadHeaderTimeout: time.Duration(cfg.Server.ReadHeaderTimeoutMS) * time.Millisecond,
		IdleTimeout:       90 * time.Second,
	}
	log.Printf("xrouter listening on %s", cfg.Server.Listen)
	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.ListenAndServe()
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	case sig := <-stop:
		log.Printf("xrouter shutting down after %s", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Fatal(err)
		}
	}
}

func getenvDefault(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

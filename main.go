package main

import (
	"flag"
	"log"
	"net/http"
	"os"
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
	}
	log.Printf("xrouter listening on %s", cfg.Server.Listen)
	if err := httpServer.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func getenvDefault(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

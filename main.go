package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
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
	warnIfUnauthenticatedProviderCredentials(cfg, configuredAPIKeys(cfg.Auth))
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

func warnIfUnauthenticatedProviderCredentials(cfg Config, apiKeys map[string]struct{}) {
	if len(apiKeys) > 0 {
		return
	}
	providers := unauthenticatedProviderCredentialNames(cfg)
	if len(providers) == 0 {
		return
	}
	apiKeysEnv := strings.TrimSpace(cfg.Auth.APIKeysEnv)
	if apiKeysEnv == "" {
		apiKeysEnv = "auth.api_keys_env"
	}
	log.Printf("WARNING: provider credentials are loaded for %s but no XRouter API keys are configured; shared or public deployments should set auth.api_keys or %s", strings.Join(providers, ","), apiKeysEnv)
}

func unauthenticatedProviderCredentialNames(cfg Config) []string {
	providers := make([]string, 0, len(cfg.Providers))
	for name, provider := range cfg.Providers {
		if providerHasLoadedAPIKey(provider) {
			providers = append(providers, name)
		}
	}
	sort.Strings(providers)
	return providers
}

func providerHasLoadedAPIKey(provider ProviderConfig) bool {
	if strings.TrimSpace(provider.APIKey) != "" {
		return true
	}
	if strings.TrimSpace(provider.APIKeyEnv) == "" {
		return false
	}
	return strings.TrimSpace(os.Getenv(provider.APIKeyEnv)) != ""
}

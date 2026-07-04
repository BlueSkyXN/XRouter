package main

import (
	"fmt"
	"testing"
	"time"
)

func TestStickyStoreEvictsWhenBounded(t *testing.T) {
	store := NewStickyStore()
	store.max = 2
	store.Set("a", "ta", time.Minute)
	store.Set("b", "tb", time.Minute)
	store.Set("c", "tc", time.Minute)
	if len(store.items) != 2 {
		t.Fatalf("expected sticky store to stay bounded at 2 items, got %d", len(store.items))
	}
	if _, ok := store.Get("a"); ok {
		t.Fatal("expected oldest sticky item to be evicted")
	}
}

func TestPrefixCacheEvictsInBatchesWhenBounded(t *testing.T) {
	store := NewPrefixCacheStore(PrefixCacheConfig{MaxEntries: 5})
	target := TargetConfig{Provider: "p"}
	for i := 0; i < 8; i++ {
		store.Touch(fmt.Sprintf("key-%d", i), "target", target, 0)
	}
	if store.count > 5 {
		t.Fatalf("expected prefix cache count <= 5, got %d", store.count)
	}
	total := 0
	for _, entries := range store.Snapshot() {
		total += len(entries)
	}
	if total > 5 {
		t.Fatalf("expected snapshot size <= 5, got %d", total)
	}
}

func TestPrefixCacheUpdateFromUsageCanBeDisabled(t *testing.T) {
	disabled := false
	cfg := Config{
		PrefixCache: PrefixCacheConfig{Enabled: true, MinPrefixChars: 1, PrefixChars: 64, TTLSeconds: 3600, UpdateFromUsage: &disabled},
		Providers:   map[string]ProviderConfig{"p": {BaseURL: "http://example.invalid/v1", Supports: []string{"chat"}}},
		Targets:     map[string]TargetConfig{"t": {Provider: "p", Model: "m"}},
	}
	cfg.applyDefaults()
	s := NewServer(cfg)
	body := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "cacheable prefix"}}}
	decision := RouteDecision{RouteName: "r", Route: RouteConfig{Type: "direct", Target: "t"}}
	res := UpstreamResult{
		TargetName: "t",
		Target:     cfg.Targets["t"],
		Status:     200,
		Body:       []byte(`{"usage":{"prompt_tokens_details":{"cached_tokens":123}}}`),
	}

	s.updatePrefixCache(body, decision, APIChat, res)

	if got := s.prefixBK.Snapshot(); len(got) != 0 {
		t.Fatalf("expected no prefix-cache entries when update_from_usage=false, got %+v", got)
	}
}

func TestPrefixCacheReplacesPlaceholderHashSalt(t *testing.T) {
	store := NewPrefixCacheStore(PrefixCacheConfig{HashSalt: "replace-me-per-deployment"})
	if store.cfg.HashSalt == "" || store.cfg.HashSalt == "replace-me-per-deployment" {
		t.Fatalf("expected runtime salt to replace placeholder, got %q", store.cfg.HashSalt)
	}

	explicit := NewPrefixCacheStore(PrefixCacheConfig{HashSalt: "deployment-secret"})
	if explicit.cfg.HashSalt != "deployment-secret" {
		t.Fatalf("expected explicit hash salt to be preserved, got %q", explicit.cfg.HashSalt)
	}
}

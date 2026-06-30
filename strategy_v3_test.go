package main

import (
	"net/http/httptest"
	"testing"
)

func testStrategyServer() *Server {
	cfg := Config{
		RequestOverrides: RequestOverrideConfig{Enabled: true},
		PrefixCache:      PrefixCacheConfig{Enabled: true, MaxEntries: 64, TTLSeconds: 3600, PrefixChars: 512, MinPrefixChars: 1, RecencyHalfLifeSec: 600},
		Providers:        map[string]ProviderConfig{"openai": {BaseURL: "http://127.0.0.1", Supports: []string{"chat", "responses"}}},
		Targets: map[string]TargetConfig{
			"cheap": {Provider: "openai", Model: "cheap-model", Quality: 0.35, CostIn: 0.1, CostOut: 0.1, LatencyMS: 200, Reliability: 0.98, CacheSupportScore: 1, Capabilities: CapabilityConfig{Tools: true, JSON: true, Responses: true}, Tags: []string{"cheap"}},
			"smart": {Provider: "openai", Model: "smart-model", Quality: 0.95, CostIn: 2, CostOut: 4, LatencyMS: 1500, Reliability: 0.96, CacheSupportScore: 0.6, Capabilities: CapabilityConfig{Tools: true, JSON: true, Responses: true}, Tags: []string{"code", "reasoning"}},
		},
		Routes: map[string]RouteConfig{
			"xrouter/auto":       {Kind: "smart_router", Candidates: []string{"cheap", "smart"}, Objective: "quality", PrefixCache: PrefixCacheRouteConfig{Weight: 1.5}, Weights: SmartWeights{Quality: 0.25, Cost: 0.05, Latency: 0.05, Reliability: 0.05, Capability: 0.05, Cache: 1.5}},
			"xrouter/code/*":     {Kind: "smart_router", MatchPrefixes: []string{"xrouter/code/"}, Candidates: []string{"smart", "cheap"}, KeywordRules: []KeywordRule{{Any: []string{"rust", "debug"}, Tags: []string{"code"}, Boost: 0.5}}},
			"xrouter/code/exact": {Kind: "direct_alias", Target: "cheap"},
			"xrouter/mov/synth":  {Kind: "mov", Flow: "parallel_synthesize_v1", References: []string{"cheap"}, Aggregator: "smart"},
		},
	}
	cfg.applyDefaults()
	return NewServer(cfg)
}

func TestModelIDDispatchExactBeatsPrefix(t *testing.T) {
	s := testStrategyServer()
	body := map[string]any{"model": "xrouter/code/exact", "messages": []any{map[string]any{"role": "user", "content": "debug rust"}}}
	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	decision, err := s.resolve("xrouter/code/exact", body, APIChat, r)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Route.Type != "direct" || len(decision.TargetNames) == 0 || decision.TargetNames[0] != "cheap" {
		t.Fatalf("expected exact direct target cheap, got type=%s targets=%v", decision.Route.Type, decision.TargetNames)
	}
}

func TestPrefixRouteDispatch(t *testing.T) {
	s := testStrategyServer()
	body := map[string]any{"model": "xrouter/code/rust", "messages": []any{map[string]any{"role": "user", "content": "debug rust lifetime"}}}
	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	decision, err := s.resolve("xrouter/code/rust", body, APIChat, r)
	if err != nil {
		t.Fatal(err)
	}
	if decision.RouteName != "xrouter/code/*" || decision.Route.Type != "auto" {
		t.Fatalf("expected prefix smart route, got route=%s type=%s", decision.RouteName, decision.Route.Type)
	}
}

func TestPrefixCacheBookkeepingCanChangeSmartOrder(t *testing.T) {
	s := testStrategyServer()
	body := map[string]any{"model": "xrouter/auto", "messages": []any{map[string]any{"role": "user", "content": "same long prefix for cache affinity"}}}
	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	route := s.cfg.Routes["xrouter/auto"]
	controls := s.controlsFromRequest(r, body)
	key, ok := s.prefixBK.PrefixKey(body, APIChat, route, controls)
	if !ok {
		t.Fatal("expected prefix key")
	}
	s.prefixBK.Touch(key, "cheap", s.cfg.Targets["cheap"], 4096)
	ordered := s.routeCandidates(route, body, APIChat, "", r)
	if len(ordered) == 0 || ordered[0] != "cheap" {
		t.Fatalf("expected cache-affine cheap first, got %v", ordered)
	}
}

func TestSmartRouterRejectsWhenHardFiltersRemoveAllCandidates(t *testing.T) {
	s := testStrategyServer()
	cheap := s.cfg.Targets["cheap"]
	cheap.Capabilities.Tools = false
	s.cfg.Targets["cheap"] = cheap
	s.cfg.Routes["xrouter/tools"] = RouteConfig{Type: "auto", Kind: "auto", Candidates: []string{"cheap"}}
	body := map[string]any{
		"model":    "xrouter/tools",
		"messages": []any{map[string]any{"role": "user", "content": "call a tool"}},
		"tools": []any{
			map[string]any{"type": "function", "function": map[string]any{"name": "lookup", "parameters": map[string]any{"type": "object"}}},
		},
	}
	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	if _, err := s.resolve("xrouter/tools", body, APIChat, r); err == nil {
		t.Fatal("expected smart router to fail when no candidate satisfies hard filters")
	}
}

func TestDirectAliasDoesNotRewritePrompt(t *testing.T) {
	provider := ProviderConfig{Supports: []string{"chat"}}
	target := TargetConfig{Provider: "openai", Model: "upstream-model"}
	body := map[string]any{"model": "public-model", "messages": []any{map[string]any{"role": "user", "content": "hello"}}, "xrouter": map[string]any{"dry_run": true}}
	up := prepareBodyForTarget(body, target, provider, APIChat)
	if up["model"] != "upstream-model" {
		t.Fatalf("model not mapped: %v", up["model"])
	}
	if _, ok := up["xrouter"]; ok {
		t.Fatalf("xrouter extension leaked upstream")
	}
	msgs := up["messages"].([]any)
	if msgs[0].(map[string]any)["content"] != "hello" {
		t.Fatalf("messages were rewritten: %#v", msgs)
	}
}

func TestMoVRouteMaterializes(t *testing.T) {
	s := testStrategyServer()
	body := map[string]any{"model": "xrouter/mov/synth", "messages": []any{map[string]any{"role": "user", "content": "explain"}}}
	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	decision, err := s.resolve("xrouter/mov/synth", body, APIChat, r)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Route.Type != "mov" || decision.Route.Flow != "parallel_synthesize_v1" {
		t.Fatalf("expected mov synth, got type=%s flow=%s", decision.Route.Type, decision.Route.Flow)
	}
}

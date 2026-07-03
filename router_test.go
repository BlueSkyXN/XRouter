package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testServerForRouting() *Server {
	cfg := Config{
		RequestOverrides: RequestOverrideConfig{Enabled: true, MaxShadowTargets: 4, MaxListenerTargets: 4},
		Providers: map[string]ProviderConfig{
			"p": {BaseURL: "http://example.invalid/v1", Supports: []string{"chat", "responses"}},
		},
		Targets: map[string]TargetConfig{
			"cheap": {Provider: "p", Model: "cheap", Quality: 0.4, LatencyMS: 100, Reliability: 0.9, Capabilities: CapabilityConfig{Tools: true, JSON: true, Vision: true, Responses: true}},
			"smart": {Provider: "p", Model: "smart", Quality: 0.9, LatencyMS: 200, Reliability: 0.9, Capabilities: CapabilityConfig{Tools: true, JSON: true, Vision: true, Responses: true}},
		},
		Routes: map[string]RouteConfig{
			"xrouter/auto-single": {Type: "auto", Objective: "cost", MultiModel: "never", Candidates: []string{"cheap", "smart"}},
			"xrouter/auto":        {Type: "auto", Objective: "balanced", MultiModel: "auto", MoARoute: "xrouter/moa", MoAComplexityThreshold: 0.1, Candidates: []string{"cheap", "smart"}},
			"xrouter/moa":         {Type: "moa", References: []string{"cheap"}, Aggregator: "smart", AllowPartial: true},
		},
	}
	cfg.applyDefaults()
	return NewServer(cfg)
}

func TestAutoSingleDoesNotUseMoA(t *testing.T) {
	s := testServerForRouting()
	body := map[string]any{"model": "xrouter/auto-single", "messages": []any{map[string]any{"role": "user", "content": "hello"}}}
	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	decision, err := s.resolve("xrouter/auto-single", body, APIChat, r)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Route.Type != "auto" {
		t.Fatalf("expected auto single route, got %q", decision.Route.Type)
	}
	if len(decision.TargetNames) == 0 || decision.TargetNames[0] == "smart" && decision.RouteName == "xrouter/moa" {
		t.Fatalf("unexpected target decision: %+v", decision)
	}
}

func TestAutoEscalatesToMoAWhenComplex(t *testing.T) {
	s := testServerForRouting()
	body := map[string]any{"model": "xrouter/auto", "messages": []any{map[string]any{"role": "user", "content": "```go\nfunc main(){}\n``` debug architecture distributed concurrency"}}}
	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	decision, err := s.resolve("xrouter/auto", body, APIChat, r)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Route.Type != "moa" || decision.RouteName != "xrouter/moa" {
		t.Fatalf("expected MoA escalation, got type=%q route=%q", decision.Route.Type, decision.RouteName)
	}
}

func TestRequestBypassTargetOverride(t *testing.T) {
	s := testServerForRouting()
	body := map[string]any{
		"model":    "xrouter/auto",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
		"xrouter":  map[string]any{"mode": "bypass", "target": "cheap"},
	}
	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	decision, err := s.resolve("xrouter/auto", body, APIChat, r)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Route.Type != "direct" || len(decision.TargetNames) != 1 || decision.TargetNames[0] != "cheap" {
		t.Fatalf("expected direct cheap bypass, got %+v", decision)
	}
}

func TestUnknownModelPolicyRejectDoesNotPassthrough(t *testing.T) {
	cfg := Config{
		Providers: map[string]ProviderConfig{
			"openai":     {BaseURL: "http://example.invalid/v1", Supports: []string{"chat", "responses"}},
			"openrouter": {BaseURL: "http://example.invalid/api/v1", Supports: []string{"chat"}},
		},
	}
	cfg.applyDefaults()
	s := NewServer(cfg)
	body := map[string]any{"model": "definitely-unknown-model", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	if _, err := s.resolve("definitely-unknown-model", body, APIChat, r); err == nil {
		t.Fatal("expected reject policy to reject unknown model")
	}
	if _, err := s.resolve("anthropic/not-configured", body, APIChat, r); err == nil {
		t.Fatal("expected reject policy to reject slash model without explicit passthrough")
	}
}

func TestUnknownModelPolicyPassthroughUsesExplicitProvider(t *testing.T) {
	base := Config{
		Providers: map[string]ProviderConfig{
			"openai":     {BaseURL: "http://example.invalid/v1", Supports: []string{"chat", "responses"}},
			"openrouter": {BaseURL: "http://example.invalid/api/v1", Supports: []string{"chat"}},
		},
	}
	body := map[string]any{"model": "anthropic/claude-3-haiku", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)

	openaiCfg := base
	openaiCfg.Routing.UnknownModelPolicy = "passthrough_openai"
	openaiCfg.applyDefaults()
	openaiServer := NewServer(openaiCfg)
	openaiDecision, err := openaiServer.resolve("anthropic/claude-3-haiku", body, APIChat, r)
	if err != nil {
		t.Fatal(err)
	}
	openaiTarget, ok := openaiServer.targetByName(openaiDecision.TargetNames[0])
	if !ok || openaiTarget.Provider != "openai" {
		t.Fatalf("expected explicit OpenAI passthrough, got target=%+v ok=%v", openaiTarget, ok)
	}

	openrouterCfg := base
	openrouterCfg.Routing.UnknownModelPolicy = "passthrough_openrouter"
	openrouterCfg.applyDefaults()
	openrouterServer := NewServer(openrouterCfg)
	openrouterDecision, err := openrouterServer.resolve("plain-model-name", body, APIChat, r)
	if err != nil {
		t.Fatal(err)
	}
	openrouterTarget, ok := openrouterServer.targetByName(openrouterDecision.TargetNames[0])
	if !ok || openrouterTarget.Provider != "openrouter" {
		t.Fatalf("expected explicit OpenRouter passthrough, got target=%+v ok=%v", openrouterTarget, ok)
	}
}

func TestStreamingNonRetryableUpstreamErrorIsNotAppended(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer upstream.Close()

	cfg := Config{
		Providers: map[string]ProviderConfig{
			"p": {BaseURL: upstream.URL, Supports: []string{"chat"}},
		},
		Targets: map[string]TargetConfig{
			"bad": {Provider: "p", Model: "bad-model", Capabilities: CapabilityConfig{Tools: true, JSON: true}},
		},
		Routes: map[string]RouteConfig{
			"xrouter/bad": {Type: "direct", Target: "bad"},
		},
	}
	cfg.applyDefaults()
	s := NewServer(cfg)
	body := map[string]any{"model": "xrouter/bad", "stream": true, "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	decision, err := s.resolve("xrouter/bad", body, APIChat, req)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.handleStreamWithFallback(rec, req, body, decision, APIChat, "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected upstream status 401, got %d with body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != `{"error":"bad key"}` {
		t.Fatalf("expected only upstream body, got %q", got)
	}
	if strings.Contains(rec.Body.String(), "upstream_error") {
		t.Fatalf("xrouter error was appended to stream response: %s", rec.Body.String())
	}
}

func TestStreamingNetworkErrorFallsBackToNextTarget(t *testing.T) {
	badUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	badURL := badUpstream.URL
	badUpstream.Close()

	goodCalls := 0
	goodUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		goodCalls++
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: ok\n\n"))
	}))
	defer goodUpstream.Close()

	cfg := Config{
		Providers: map[string]ProviderConfig{
			"bad":  {BaseURL: badURL, Supports: []string{"chat"}},
			"good": {BaseURL: goodUpstream.URL, Supports: []string{"chat"}},
		},
		Targets: map[string]TargetConfig{
			"bad-target":  {Provider: "bad", Model: "bad-model"},
			"good-target": {Provider: "good", Model: "good-model"},
		},
		Routes: map[string]RouteConfig{
			"xrouter/fallback-stream": {Type: "direct", Target: "bad-target", Fallbacks: []string{"good-target"}},
		},
	}
	cfg.applyDefaults()
	s := NewServer(cfg)
	body := map[string]any{"model": "xrouter/fallback-stream", "stream": true, "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	decision, err := s.resolve("xrouter/fallback-stream", body, APIChat, req)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.handleStreamWithFallback(rec, req, body, decision, APIChat, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected fallback stream to succeed, got %d with body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("x-xrouter-target"); got != "good-target" {
		t.Fatalf("expected fallback target good-target, got %q", got)
	}
	if got := rec.Body.String(); got != "data: ok\n\n" {
		t.Fatalf("expected successful stream body, got %q", got)
	}
	if goodCalls != 1 {
		t.Fatalf("expected one fallback upstream call, got %d", goodCalls)
	}
}

func TestStreamingClientDoesNotTimeoutAfterHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(75 * time.Millisecond)
		_, _ = w.Write([]byte("data: slow\n\n"))
	}))
	defer upstream.Close()

	cfg := Config{
		Server: ServerConfig{RequestTimeoutMS: 25},
		Providers: map[string]ProviderConfig{
			"p": {BaseURL: upstream.URL, Supports: []string{"chat"}},
		},
		Targets: map[string]TargetConfig{
			"slow": {Provider: "p", Model: "slow-model"},
		},
		Routes: map[string]RouteConfig{
			"xrouter/slow-stream": {Type: "direct", Target: "slow"},
		},
	}
	cfg.applyDefaults()
	s := NewServer(cfg)
	body := map[string]any{"model": "xrouter/slow-stream", "stream": true, "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	decision, err := s.resolve("xrouter/slow-stream", body, APIChat, req)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.handleStreamWithFallback(rec, req, body, decision, APIChat, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected slow stream to keep running after headers, got %d with body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "data: slow\n\n" {
		t.Fatalf("expected delayed stream body, got %q", got)
	}
}

func TestRequestBodyLimitRejectsOversizedJSON(t *testing.T) {
	cfg := Config{
		Server: ServerConfig{MaxRequestBodyBytes: 16},
		Providers: map[string]ProviderConfig{
			"p": {BaseURL: "http://example.invalid/v1", Supports: []string{"chat"}},
		},
		Targets: map[string]TargetConfig{
			"t": {Provider: "p", Model: "m"},
		},
		Routes: map[string]RouteConfig{
			"xrouter/t": {Type: "direct", Target: "t"},
		},
	}
	cfg.applyDefaults()
	s := NewServer(cfg)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"xrouter/t","messages":[{"role":"user","content":"this is too large"}]}`))
	rec := httptest.NewRecorder()
	s.handleChat(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected oversized body to be rejected as bad request, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "request body too large") {
		t.Fatalf("expected body size error, got %s", rec.Body.String())
	}
}

func TestMetricsUsesConfiguredAPIKeys(t *testing.T) {
	cfg := Config{Auth: AuthConfig{APIKeys: []string{"secret"}}}
	cfg.applyDefaults()
	s := NewServer(cfg)
	handler := s.routes()

	unauthorizedReq := httptest.NewRequest("GET", "/metrics", nil)
	unauthorizedRec := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedRec, unauthorizedReq)
	if unauthorizedRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated metrics to return 401, got %d", unauthorizedRec.Code)
	}

	authorizedReq := httptest.NewRequest("GET", "/metrics", nil)
	authorizedReq.Header.Set("Authorization", "Bearer secret")
	authorizedRec := httptest.NewRecorder()
	handler.ServeHTTP(authorizedRec, authorizedReq)
	if authorizedRec.Code != http.StatusOK {
		t.Fatalf("expected authenticated metrics to return 200, got %d: %s", authorizedRec.Code, authorizedRec.Body.String())
	}
}

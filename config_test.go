package main

import "testing"

func TestLoadConfigAcceptsExample(t *testing.T) {
	if _, err := LoadConfig("config.example.json"); err != nil {
		t.Fatal(err)
	}
}

func TestConfigValidationRejectsMissingRouteTarget(t *testing.T) {
	cfg := Config{
		Providers: map[string]ProviderConfig{
			"p": {BaseURL: "http://example.invalid/v1", Supports: []string{"chat"}},
		},
		Routes: map[string]RouteConfig{
			"xrouter/missing": {Type: "direct", Target: "does-not-exist"},
		},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected missing route target to fail validation")
	}
}

func TestConfigValidationAllowsExplicitPassthroughTargetRefs(t *testing.T) {
	cfg := Config{
		Routing: RoutingConfig{UnknownModelPolicy: "passthrough_openrouter"},
		Providers: map[string]ProviderConfig{
			"openrouter": {BaseURL: "http://example.invalid/api/v1", Supports: []string{"chat"}},
		},
		Routes: map[string]RouteConfig{
			"anthropic/claude": {Type: "direct", Target: "anthropic/claude"},
		},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestConfigValidationRejectsPassthroughRouteWithoutProviderPolicy(t *testing.T) {
	cfg := Config{
		Providers: map[string]ProviderConfig{
			"p": {BaseURL: "http://example.invalid/v1", Supports: []string{"chat"}},
		},
		Routes: map[string]RouteConfig{
			"provider/model": {Type: "passthrough"},
		},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected passthrough route without target or passthrough provider policy to fail validation")
	}
}

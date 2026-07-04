package main

import "testing"

func TestLoadConfigAcceptsExample(t *testing.T) {
	if _, err := LoadConfig("config.example.json"); err != nil {
		t.Fatal(err)
	}
}

func TestExampleConfigKeepsSensitiveSurfacesClosed(t *testing.T) {
	cfg, err := LoadConfig("config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Debug {
		t.Fatal("expected config.example.json to keep server.debug=false")
	}
	if cfg.RequestOverrides.AllowProviderKeyOverride {
		t.Fatal("expected config.example.json to keep allow_provider_key_override=false")
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

func TestConfigValidationRejectsImplicitPassthroughTypos(t *testing.T) {
	cfg := Config{
		Routing: RoutingConfig{UnknownModelPolicy: "passthrough_openrouter"},
		Providers: map[string]ProviderConfig{
			"openrouter": {BaseURL: "http://example.invalid/api/v1", Supports: []string{"chat"}},
		},
		Routes: map[string]RouteConfig{
			"xrouter/typo": {Type: "direct", Target: "opnai-smart"},
		},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected non provider/model missing target to fail even with passthrough policy")
	}
}

func TestConfigValidationRejectsUnknownListenerMode(t *testing.T) {
	cfg := Config{
		Providers: map[string]ProviderConfig{
			"p": {BaseURL: "http://example.invalid/v1", Supports: []string{"chat"}},
		},
		Targets: map[string]TargetConfig{
			"t": {Provider: "p", Model: "m"},
		},
		Routes: map[string]RouteConfig{
			"xrouter/listener": {Type: "direct", Target: "t", SerialListeners: []ListenerConfig{{Target: "t", Mode: "async"}}},
		},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected unsupported listener mode to fail validation")
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

func TestConfigValidationRejectsReservedMoVStages(t *testing.T) {
	cfg := Config{
		Providers: map[string]ProviderConfig{
			"p": {BaseURL: "http://example.invalid/v1", Supports: []string{"chat"}},
		},
		Targets: map[string]TargetConfig{
			"t": {Provider: "p", Model: "m"},
		},
		Routes: map[string]RouteConfig{
			"xrouter/mov/staged": {
				Type:   "mov",
				Flow:   "parallel_synthesize_v1",
				Stages: []MoVStage{{Name: "draft", Targets: []string{"t"}}},
			},
		},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected reserved mov stages to fail validation")
	}
}

func TestConfigValidationRejectsRequiredKeywordRuleWithoutSelector(t *testing.T) {
	cfg := Config{
		Providers: map[string]ProviderConfig{
			"p": {BaseURL: "http://example.invalid/v1", Supports: []string{"chat"}},
		},
		Targets: map[string]TargetConfig{
			"t": {Provider: "p", Model: "m"},
		},
		Routes: map[string]RouteConfig{
			"xrouter/auto": {
				Type:         "auto",
				Candidates:   []string{"t"},
				KeywordRules: []KeywordRule{{Any: []string{"debug"}, Require: true}},
			},
		},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected require=true keyword rule without targets or tags to fail validation")
	}
}

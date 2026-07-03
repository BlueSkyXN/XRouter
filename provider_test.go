package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProviderKeyOverrideRequiresRequestOverridesEnabled(t *testing.T) {
	var auths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))
	defer upstream.Close()

	cfg := Config{
		RequestOverrides: RequestOverrideConfig{Enabled: false, AllowProviderKeyOverride: true},
		Providers: map[string]ProviderConfig{
			"p": {BaseURL: upstream.URL, APIKey: "server-key", Supports: []string{"chat"}},
		},
		Targets: map[string]TargetConfig{
			"t": {Provider: "p", Model: "upstream-model"},
		},
	}
	cfg.applyDefaults()
	s := NewServer(cfg)

	body := map[string]any{"model": "xrouter/t", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	headerReq := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	headerReq.Header.Set("x-xrouter-provider-key-p", "header-key")
	res := s.callTargetBytes(headerReq.Context(), "t", APIChat, body, headerReq)
	if res.Err != nil {
		t.Fatal(res.Err)
	}

	bodyReq := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	bodyWithKey := cloneJSONMap(body)
	bodyWithKey["xrouter"] = map[string]any{"provider_api_keys": map[string]any{"p": "body-key"}}
	res = s.callTargetBytes(bodyReq.Context(), "t", APIChat, bodyWithKey, bodyReq)
	if res.Err != nil {
		t.Fatal(res.Err)
	}

	if len(auths) != 2 {
		t.Fatalf("expected 2 upstream calls, got %d", len(auths))
	}
	for i, got := range auths {
		if got != "Bearer server-key" {
			t.Fatalf("call %d used override despite request_overrides.enabled=false: %q", i, got)
		}
	}
}

func TestProviderKeyOverrideAllowsHeaderAndBodyWhenEnabled(t *testing.T) {
	var auths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))
	defer upstream.Close()

	cfg := Config{
		RequestOverrides: RequestOverrideConfig{Enabled: true, AllowProviderKeyOverride: true},
		Providers: map[string]ProviderConfig{
			"p": {BaseURL: upstream.URL, APIKey: "server-key", Supports: []string{"chat"}},
		},
		Targets: map[string]TargetConfig{
			"t": {Provider: "p", Model: "upstream-model"},
		},
	}
	cfg.applyDefaults()
	s := NewServer(cfg)

	body := map[string]any{"model": "xrouter/t", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	headerReq := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	headerReq.Header.Set("x-xrouter-provider-key-p", "header-key")
	res := s.callTargetBytes(headerReq.Context(), "t", APIChat, body, headerReq)
	if res.Err != nil {
		t.Fatal(res.Err)
	}

	bodyReq := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	bodyWithKey := cloneJSONMap(body)
	bodyWithKey["xrouter"] = map[string]any{"provider_api_keys": map[string]any{"p": "body-key"}}
	res = s.callTargetBytes(bodyReq.Context(), "t", APIChat, bodyWithKey, bodyReq)
	if res.Err != nil {
		t.Fatal(res.Err)
	}

	if len(auths) != 2 {
		t.Fatalf("expected 2 upstream calls, got %d", len(auths))
	}
	if auths[0] != "Bearer header-key" {
		t.Fatalf("expected header override, got %q", auths[0])
	}
	if auths[1] != "Bearer body-key" {
		t.Fatalf("expected body override, got %q", auths[1])
	}
}

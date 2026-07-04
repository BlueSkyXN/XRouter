package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestUpstreamResponseBodyLimitReturnsReadError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"too":"large"}`))
	}))
	defer upstream.Close()

	cfg := Config{
		Server: ServerConfig{MaxUpstreamBodyBytes: 4},
		Providers: map[string]ProviderConfig{
			"p": {BaseURL: upstream.URL, Supports: []string{"chat"}},
		},
		Targets: map[string]TargetConfig{
			"t": {Provider: "p", Model: "m"},
		},
	}
	cfg.applyDefaults()
	s := NewServer(cfg)
	body := map[string]any{"model": "xrouter/t", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	res := s.callTargetBytes(httptest.NewRequest("POST", "/", nil).Context(), "t", APIChat, body, nil)
	if res.Err == nil || res.Status != 0 {
		t.Fatalf("expected size-limit read error with status 0, got status=%d err=%v", res.Status, res.Err)
	}
}

func TestReadLimitedResponseBodyAllowsExactLimit(t *testing.T) {
	data, err := readLimitedResponseBody(strings.NewReader("1234"), 4)
	if err != nil || string(data) != "1234" {
		t.Fatalf("expected exact limit to pass, got %q err=%v", string(data), err)
	}
	if _, err := readLimitedResponseBody(strings.NewReader("12345"), 4); err == nil {
		t.Fatal("expected over-limit body to fail")
	}
}

func TestPrepareBodyTranslatesOnlyInternalOpenAIReasoningEffort(t *testing.T) {
	body := map[string]any{
		"model":     "xrouter/race",
		"reasoning": map[string]any{"effort": "high"},
		"messages":  []any{map[string]any{"role": "user", "content": "hi"}},
	}
	up := prepareBodyForTarget(body, TargetConfig{Provider: "openai", Model: "gpt"}, ProviderConfig{}, APIChat)
	if _, ok := up["reasoning"].(map[string]any); !ok || up["reasoning_effort"] != nil {
		t.Fatalf("direct OpenAI body should preserve user reasoning params without translation, got %#v", up)
	}
	internal := cloneTopLevelJSONMap(body)
	internal[internalReasoningEffortKey] = true
	internal[internalProviderAPIKeysKey] = map[string]string{"openai": "secret-key"}
	up = prepareBodyForTarget(internal, TargetConfig{Provider: "openai", Model: "gpt"}, ProviderConfig{}, APIChat)
	if up["reasoning_effort"] != "high" {
		t.Fatalf("expected internal reasoning_effort=high, got %#v", up["reasoning_effort"])
	}
	if _, ok := up["reasoning"]; ok {
		t.Fatalf("expected internal OpenAI reasoning object to be translated away, got %#v", up["reasoning"])
	}
	if _, ok := up[internalReasoningEffortKey]; ok {
		t.Fatalf("internal translation marker leaked upstream: %#v", up)
	}
	if _, ok := up[internalProviderAPIKeysKey]; ok {
		t.Fatalf("internal provider key marker leaked upstream: %#v", up)
	}
	or := prepareBodyForTarget(body, TargetConfig{Provider: "openrouter", Model: "or-model"}, ProviderConfig{}, APIChat)
	if _, ok := or["reasoning"].(map[string]any); !ok {
		t.Fatalf("expected non-OpenAI reasoning object to be preserved, got %#v", or["reasoning"])
	}
}

func TestPrepareBodyExtraBodyDoesNotOverrideClientFields(t *testing.T) {
	body := map[string]any{
		"model":           "gpt-4o-mini",
		"messages":        []any{map[string]any{"role": "user", "content": "keep me"}},
		"tools":           []any{map[string]any{"type": "function", "function": map[string]any{"name": "lookup"}}},
		"response_format": map[string]any{"type": "json_object"},
		"temperature":     0.2,
	}
	target := TargetConfig{
		Provider: "p",
		Model:    "upstream-model",
		ExtraBody: map[string]any{
			"messages":        []any{map[string]any{"role": "system", "content": "rewrite"}},
			"tools":           []any{},
			"response_format": map[string]any{"type": "text"},
			"temperature":     1.0,
			"provider":        map[string]any{"order": []any{"openai"}},
		},
	}

	up := prepareBodyForTarget(body, target, ProviderConfig{}, APIChat)

	if up["model"] != "upstream-model" {
		t.Fatalf("expected only model mapping to change, got %#v", up["model"])
	}
	if content := up["messages"].([]any)[0].(map[string]any)["content"]; content != "keep me" {
		t.Fatalf("extra_body overrode messages: %#v", up["messages"])
	}
	if len(up["tools"].([]any)) != 1 {
		t.Fatalf("extra_body overrode tools: %#v", up["tools"])
	}
	if up["response_format"].(map[string]any)["type"] != "json_object" {
		t.Fatalf("extra_body overrode response_format: %#v", up["response_format"])
	}
	if up["temperature"] != 0.2 {
		t.Fatalf("extra_body overrode temperature: %#v", up["temperature"])
	}
	if _, ok := up["provider"]; ok {
		t.Fatalf("non-OpenRouter provider-specific body leaked upstream: %#v", up["provider"])
	}

	up = prepareBodyForTarget(body, TargetConfig{Provider: "openrouter", Model: "or-model", ExtraBody: target.ExtraBody}, ProviderConfig{}, APIChat)
	if _, ok := up["provider"]; !ok {
		t.Fatalf("expected new OpenRouter provider extension to be added when absent: %#v", up)
	}
}

func TestListenerPromptRedactsRequestSecrets(t *testing.T) {
	original := map[string]any{
		"model":    "xrouter/t",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"xrouter": map[string]any{
			"provider_api_keys": map[string]any{"p": "body-secret"},
		},
		internalProviderAPIKeysKey: map[string]string{"p": "internal-secret"},
	}
	primary := UpstreamResult{TargetName: "t", Target: TargetConfig{Model: "upstream"}, Status: http.StatusOK, Body: []byte(`{"choices":[{"message":{"content":"ok"}}]}`)}
	body := buildListenerBody(original, RouteDecision{RouteName: "xrouter/t"}, APIChat, primary, ListenerConfig{Target: "listener"})
	msgs := body["messages"].([]any)
	user := msgs[1].(map[string]any)
	content := user["content"].(string)
	if strings.Contains(content, "body-secret") || strings.Contains(content, "internal-secret") {
		t.Fatalf("listener prompt leaked provider key material: %s", content)
	}
	if !strings.Contains(content, "[redacted]") {
		t.Fatalf("expected listener prompt to include redaction marker, got %s", content)
	}
}

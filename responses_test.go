package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResponsesToChatBodyStringInput(t *testing.T) {
	body := map[string]any{
		"model":             "xrouter/auto",
		"instructions":      "Be concise.",
		"input":             "Explain routers.",
		"max_output_tokens": 123,
	}
	chat, err := responsesToChatBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if chat["max_tokens"] != 123 {
		t.Fatalf("expected max_tokens mapping, got %#v", chat["max_tokens"])
	}
	msgs, ok := chat["messages"].([]any)
	if !ok || len(msgs) != 2 {
		t.Fatalf("expected two messages, got %#v", chat["messages"])
	}
}

func TestChatCompletionToResponse(t *testing.T) {
	raw := []byte(`{"id":"chatcmpl_1","created":123,"model":"m1","choices":[{"message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`)
	wrapped, err := chatCompletionToResponse(raw, map[string]any{}, "m1", "route", "target")
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(wrapped, &obj); err != nil {
		t.Fatal(err)
	}
	if obj["object"] != "response" || obj["output_text"] != "hello" {
		t.Fatalf("unexpected wrapped response: %s", wrapped)
	}
}

func TestChatCompletionToResponsePreservesToolCalls(t *testing.T) {
	raw := []byte(`{"id":"chatcmpl_1","created":123,"model":"m1","choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]}}]}`)
	wrapped, err := chatCompletionToResponse(raw, map[string]any{}, "m1", "route", "target")
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(wrapped, &obj); err != nil {
		t.Fatal(err)
	}
	output, ok := obj["output"].([]any)
	if !ok || len(output) != 1 {
		t.Fatalf("expected one tool-call output item, got %#v", obj["output"])
	}
	call, ok := output[0].(map[string]any)
	if !ok {
		t.Fatalf("expected object output item, got %#v", output[0])
	}
	if call["type"] != "function_call" || call["call_id"] != "call_1" || call["name"] != "lookup" || call["arguments"] != `{"q":"x"}` {
		t.Fatalf("tool call was not preserved: %#v", call)
	}
}

func TestResponsesNativeRetryableFailureFallsThroughToChatShimOnly(t *testing.T) {
	var calls []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		model := stringFromAny(body["model"])
		calls = append(calls, r.URL.Path+":"+model)
		if r.URL.Path == "/responses" && model == "native-model" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "retry native"})
			return
		}
		if r.URL.Path == "/responses" && model == "shim-model" {
			writeJSON(w, http.StatusTeapot, map[string]any{"error": "shim target must not be called as native responses"})
			return
		}
		if r.URL.Path == "/chat/completions" && model == "shim-model" {
			writeJSON(w, http.StatusOK, map[string]any{
				"id":      "chatcmpl_1",
				"object":  "chat.completion",
				"created": 123,
				"model":   "shim-model",
				"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "shim ok"}}},
			})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unexpected call"})
	}))
	defer upstream.Close()

	cfg := Config{
		Providers: map[string]ProviderConfig{
			"p": {BaseURL: upstream.URL, Supports: []string{"chat", "responses"}},
		},
		Targets: map[string]TargetConfig{
			"native": {Provider: "p", Model: "native-model", Capabilities: CapabilityConfig{Responses: true}},
			"shim":   {Provider: "p", Model: "shim-model"},
		},
		Routes: map[string]RouteConfig{
			"xrouter/responses": {Type: "direct", Target: "native", Fallbacks: []string{"shim"}},
		},
	}
	cfg.applyDefaults()
	s := NewServer(cfg)

	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"xrouter/responses","input":"hi"}`))
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected shim fallback success, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.Join(calls, ","), "/responses:shim-model") {
		t.Fatalf("shim-only target was called through native Responses path: %v", calls)
	}
	if !strings.Contains(rec.Body.String(), "shim ok") {
		t.Fatalf("expected shim response body, got %s", rec.Body.String())
	}
}

func TestResponsesShimPreservesBodyProviderKeyOverride(t *testing.T) {
	var auth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		writeJSON(w, http.StatusOK, map[string]any{
			"id":      "chatcmpl_1",
			"object":  "chat.completion",
			"created": 123,
			"model":   "shim-model",
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
		})
	}))
	defer upstream.Close()

	cfg := Config{
		RequestOverrides: RequestOverrideConfig{Enabled: true, AllowProviderKeyOverride: true},
		Providers: map[string]ProviderConfig{
			"p": {BaseURL: upstream.URL, APIKey: "server-key", Supports: []string{"chat"}},
		},
		Targets: map[string]TargetConfig{
			"shim": {Provider: "p", Model: "shim-model"},
		},
		Routes: map[string]RouteConfig{
			"xrouter/responses": {Type: "direct", Target: "shim"},
		},
	}
	cfg.applyDefaults()
	s := NewServer(cfg)

	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"xrouter/responses","input":"hi","xrouter":{"provider_api_keys":{"p":"body-key"}}}`))
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected responses shim success, got %d: %s", rec.Code, rec.Body.String())
	}
	if auth != "Bearer body-key" {
		t.Fatalf("expected body provider key override to survive shim, got %q", auth)
	}
}

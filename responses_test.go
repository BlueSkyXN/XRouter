package main

import (
	"encoding/json"
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

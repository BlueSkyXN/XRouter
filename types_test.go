package main

import (
	"encoding/json"
	"testing"
)

func TestCloneJSONMapPreservesJSONNumberPrecision(t *testing.T) {
	in := map[string]any{
		"request_id": json.Number("9007199254740993"),
		"nested": map[string]any{
			"max_id": json.Number("9223372036854775807"),
		},
		"items": []any{json.Number("9007199254740995")},
	}

	out := cloneJSONMap(in)

	if got := out["request_id"].(json.Number).String(); got != "9007199254740993" {
		t.Fatalf("request_id lost precision: %s", got)
	}
	nested := out["nested"].(map[string]any)
	if got := nested["max_id"].(json.Number).String(); got != "9223372036854775807" {
		t.Fatalf("nested max_id lost precision: %s", got)
	}
	items := out["items"].([]any)
	if got := items[0].(json.Number).String(); got != "9007199254740995" {
		t.Fatalf("list item lost precision: %s", got)
	}
}

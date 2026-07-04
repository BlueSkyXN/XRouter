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

func TestApplyDefaultsKeepsParallelismUnsetForCandidateRace(t *testing.T) {
	cfg := Config{
		Routes: map[string]RouteConfig{
			"xrouter/race": {
				Type:       "mov",
				Flow:       "boundary_guard_race_v1",
				Candidates: []string{"a", "b"},
			},
		},
	}

	cfg.applyDefaults()

	if got := cfg.Routes["xrouter/race"].Parallelism; got != 0 {
		t.Fatalf("expected unset parallelism to remain 0, got %d", got)
	}
}

func TestApplyDefaultsSetsRequestOverrideBounds(t *testing.T) {
	var cfg Config
	cfg.applyDefaults()
	if got := cfg.RequestOverrides.MaxRoutingTargets; got != 32 {
		t.Fatalf("expected default max routing targets 32, got %d", got)
	}
	if got := cfg.RequestOverrides.MaxShadowTargets; got != 4 {
		t.Fatalf("expected default max shadow targets 4, got %d", got)
	}
	if got := cfg.RequestOverrides.MaxListenerTargets; got != 4 {
		t.Fatalf("expected default max listener targets 4, got %d", got)
	}
}

func TestEffectiveParallelismDefaultsToWorkItems(t *testing.T) {
	tests := []struct {
		name       string
		configured int
		workItems  int
		want       int
	}{
		{name: "unset", configured: 0, workItems: 3, want: 3},
		{name: "negative", configured: -1, workItems: 3, want: 3},
		{name: "cap", configured: 2, workItems: 3, want: 2},
		{name: "oversized", configured: 9, workItems: 3, want: 3},
		{name: "no work", configured: 0, workItems: 0, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveParallelism(tt.configured, tt.workItems); got != tt.want {
				t.Fatalf("effectiveParallelism(%d, %d) = %d, want %d", tt.configured, tt.workItems, got, tt.want)
			}
		})
	}
}

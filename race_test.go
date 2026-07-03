package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestReasoningBoundaryFormula(t *testing.T) {
	cfg := RaceConfig{BoundaryStart: 516, BoundaryStep: 518, BoundaryTolerance: 0}
	for _, n := range []int{516, 1034, 1552, 2070} {
		if !reasoningBoundaryHit(n, cfg) {
			t.Fatalf("expected %d to hit boundary", n)
		}
	}
	for _, n := range []int{515, 517, 1000, 1035} {
		if reasoningBoundaryHit(n, cfg) {
			t.Fatalf("expected %d not to hit boundary", n)
		}
	}
	cfg.BoundaryTolerance = 2
	if !reasoningBoundaryHit(1035, cfg) {
		t.Fatalf("expected tolerance to mark 1035 as boundary-adjacent")
	}
}

func TestRaceBoundaryAwareSelection(t *testing.T) {
	boundary := chatBodyForRace("longer but suspicious", 516, 800, "stop")
	normal := chatBodyForRace("shorter but normal", 900, 500, "stop")
	cfg := defaultRaceConfig(RaceConfig{Selection: "boundary_aware", BoundaryPenalty: 5000})
	attempts := []raceAttempt{
		{Index: 0, Target: "a", Result: UpstreamResult{TargetName: "a", Status: http.StatusOK, Body: boundary}, Succeeded: true},
		{Index: 1, Target: "b", Result: UpstreamResult{TargetName: "b", Status: http.StatusOK, Body: normal}, Succeeded: true},
	}
	for i := range attempts {
		attempts[i].Metrics = metricsFromResult(attempts[i].Result, cfg)
		attempts[i].Score = raceScore(attempts[i], cfg)
	}
	res, err := selectRaceResult(attempts, RouteConfig{Race: cfg}, "test/race")
	if err != nil {
		t.Fatal(err)
	}
	if res.TargetName != "b" {
		t.Fatalf("expected non-boundary target b, got %s", res.TargetName)
	}
}

func TestBuildRacePlansWithEfforts(t *testing.T) {
	plans := buildRacePlans(RouteConfig{Candidates: []string{"a", "b"}, Race: RaceConfig{Efforts: []string{"medium", "high"}, Replicas: 9}})
	if len(plans) != 4 {
		t.Fatalf("expected 2 targets x 2 efforts, got %d", len(plans))
	}
	if plans[0].Target != "a" || plans[0].Effort != "medium" || plans[3].Target != "b" || plans[3].Effort != "high" {
		t.Fatalf("unexpected plans: %+v", plans)
	}
}

func TestAddXRouterBodyMetadataPreservesRaceMetadata(t *testing.T) {
	raw := []byte(`{"choices":[{"message":{"content":"ok"}}],"xrouter":{"synthetic":true,"race":{"winner_target":"fast","attempts":[{"target":"fast"}]}}}`)

	got := addXRouterBodyMetadata(raw, "xrouter/race", "fast", "upstream-fast")

	var obj map[string]any
	if err := json.Unmarshal(got, &obj); err != nil {
		t.Fatal(err)
	}
	xr := obj["xrouter"].(map[string]any)
	if xr["route"] != "xrouter/race" || xr["target"] != "fast" || xr["upstream_model"] != "upstream-fast" {
		t.Fatalf("metadata fields not attached correctly: %+v", xr)
	}
	if xr["synthetic"] != true {
		t.Fatalf("existing xrouter metadata was not preserved: %+v", xr)
	}
	race := xr["race"].(map[string]any)
	if race["winner_target"] != "fast" {
		t.Fatalf("race metadata was overwritten: %+v", race)
	}
}

func chatBodyForRace(text string, reasoningTokens, completionTokens int, finishReason string) []byte {
	body := map[string]any{
		"choices": []any{map[string]any{"finish_reason": finishReason, "message": map[string]any{"role": "assistant", "content": text}}},
		"usage": map[string]any{
			"completion_tokens":         completionTokens,
			"completion_tokens_details": map[string]any{"reasoning_tokens": reasoningTokens},
		},
	}
	b, _ := json.Marshal(body)
	return b
}

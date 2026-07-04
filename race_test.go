package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

func TestRaceSelectionPrefersNonDegradedSuccessOverHigherScore(t *testing.T) {
	boundary := chatBodyForRace(strings.Repeat("boundary ", 8000), 516, 10000, "stop")
	normal := chatBodyForRace("short normal answer", 900, 50, "stop")
	cfg := defaultRaceConfig(RaceConfig{Selection: "boundary_aware", BoundaryPenalty: 100})
	attempts := []raceAttempt{
		{Index: 0, Target: "a", Result: UpstreamResult{TargetName: "a", Status: http.StatusOK, Body: boundary}, Succeeded: true},
		{Index: 1, Target: "b", Result: UpstreamResult{TargetName: "b", Status: http.StatusOK, Body: normal}, Succeeded: true},
	}
	for i := range attempts {
		attempts[i].Metrics = metricsFromResult(attempts[i].Result, cfg)
		attempts[i].Score = raceScore(attempts[i], cfg)
	}
	if attempts[0].Score <= attempts[1].Score {
		t.Fatalf("test setup expected degraded attempt to have higher raw score, got %+v", attempts)
	}
	res, err := selectRaceResult(attempts, RouteConfig{Race: cfg}, "test/race")
	if err != nil {
		t.Fatal(err)
	}
	if res.TargetName != "b" {
		t.Fatalf("expected non-degraded target b despite lower raw score, got %s", res.TargetName)
	}
}

func TestRaceScoreNormalizesSelection(t *testing.T) {
	cfg := defaultRaceConfig(RaceConfig{Selection: "fastest-acceptable"})
	cfg.Selection = "fastest-acceptable"
	body := chatBodyForRace("ok", 0, 5, "stop")
	att := raceAttempt{
		Index:     0,
		Target:    "a",
		Result:    UpstreamResult{TargetName: "a", Status: http.StatusOK, Body: body},
		Metrics:   metricsFromResult(UpstreamResult{TargetName: "a", Status: http.StatusOK, Body: body}, cfg),
		Succeeded: true,
	}
	if score := raceScore(att, cfg); score < 100_000 {
		t.Fatalf("expected hyphenated fastest-acceptable to receive fastest bonus, got %f", score)
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

func TestApproxTokenCountCountsCJKCharacters(t *testing.T) {
	if got := approxTokenCount("这是一个中文回答"); got < 8 {
		t.Fatalf("expected CJK token estimate to count near characters, got %d", got)
	}
	if got := approxTokenCount("one two three four"); got != 4 {
		t.Fatalf("expected whitespace words to dominate English estimate, got %d", got)
	}
}

func TestFastestAcceptableRaceCancelsSlowerAttempts(t *testing.T) {
	slowStarted := make(chan struct{})
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-slowStarted:
		case <-time.After(100 * time.Millisecond):
		}
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": "fast acceptable answer"}}}})
	}))
	defer fast.Close()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(slowStarted)
		select {
		case <-r.Context().Done():
			return
		case <-time.After(500 * time.Millisecond):
			writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": "slow answer"}}}})
		}
	}))
	defer slow.Close()
	cfg := Config{
		Providers: map[string]ProviderConfig{
			"fast": {BaseURL: fast.URL, Supports: []string{"chat"}},
			"slow": {BaseURL: slow.URL, Supports: []string{"chat"}},
		},
		Targets: map[string]TargetConfig{
			"fast": {Provider: "fast", Model: "fast"},
			"slow": {Provider: "slow", Model: "slow"},
		},
	}
	cfg.applyDefaults()
	s := NewServer(cfg)
	route := RouteConfig{Parallelism: 2, Race: RaceConfig{Selection: "fastest_acceptable", Targets: []string{"slow", "fast"}, Replicas: 1}}
	start := time.Now()
	attempts := s.runRaceAttempts(context.Background(), httptest.NewRequest("POST", "/", nil), map[string]any{"messages": []any{}}, route, "xrouter/race")
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("expected fastest_acceptable to return before slow attempt completes, took %s", elapsed)
	}
	res, err := selectRaceResult(attempts, route, "xrouter/race")
	if err != nil {
		t.Fatal(err)
	}
	if res.TargetName != "fast" {
		t.Fatalf("expected fast winner, got %s attempts=%+v", res.TargetName, attempts)
	}
	for _, att := range attempts {
		if att.Target == "slow" && att.Result.Err == nil {
			t.Fatalf("expected slow attempt to be marked canceled, got %+v", att)
		}
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

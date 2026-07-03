package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

type raceAttempt struct {
	Index     int
	Target    string
	Effort    string
	Replica   int
	Result    UpstreamResult
	Metrics   raceMetrics
	Score     float64
	Succeeded bool
}

type raceMetrics struct {
	VisibleTokens   int    `json:"visible_tokens"`
	OutputTokens    int    `json:"output_tokens"`
	ReasoningTokens int    `json:"reasoning_tokens"`
	TotalTokens     int    `json:"total_tokens"`
	BoundaryHit     bool   `json:"boundary_hit"`
	Incomplete      bool   `json:"incomplete"`
	FinishReason    string `json:"finish_reason,omitempty"`
	Status          string `json:"status,omitempty"`
}

func (s *Server) movParallelRaceMaxOutput(ctx context.Context, r *http.Request, body map[string]any, decision RouteDecision) (UpstreamResult, error) {
	route := decision.Route
	route.Race.Selection = "max_output"
	attempts := s.runRaceAttempts(ctx, r, body, route, decision.RouteName)
	return selectRaceResult(attempts, route, decision.RouteName)
}

func (s *Server) movBoundaryGuardRace(ctx context.Context, r *http.Request, body map[string]any, decision RouteDecision) (UpstreamResult, error) {
	route := decision.Route
	if strings.TrimSpace(route.Race.Selection) == "" {
		route.Race.Selection = "boundary_aware"
	}
	attempts := s.runRaceAttempts(ctx, r, body, route, decision.RouteName)
	return selectRaceResult(attempts, route, decision.RouteName)
}

func (s *Server) movEffortLadderRace(ctx context.Context, r *http.Request, body map[string]any, decision RouteDecision) (UpstreamResult, error) {
	route := decision.Route
	if len(route.Race.Efforts) == 0 {
		route.Race.Efforts = []string{"medium", "high"}
	}
	if strings.TrimSpace(route.Race.Selection) == "" {
		route.Race.Selection = "boundary_aware"
	}
	attempts := s.runRaceAttempts(ctx, r, body, route, decision.RouteName)
	return selectRaceResult(attempts, route, decision.RouteName)
}

func (s *Server) movSerialBoundaryEscalate(ctx context.Context, r *http.Request, body map[string]any, decision RouteDecision) (UpstreamResult, error) {
	route := decision.Route
	primary := firstAvailable(route.Target, route.Candidates, route.References)
	if primary == "" {
		return UpstreamResult{}, fmt.Errorf("serial_boundary_escalate requires target/candidates")
	}
	body0 := bodyWithRaceVariant(body, "")
	first := s.callTargetBytes(ctx, primary, APIChat, body0, r)
	s.metrics.Record(decision.RouteName+"/race-primary", primary, first.Status, first.Duration)
	attempts := []raceAttempt{{Index: 0, Target: primary, Replica: 1, Result: first, Metrics: metricsFromResult(first, route.Race)}}
	attempts[0].Succeeded = raceSucceeded(attempts[0].Result)
	attempts[0].Score = raceScore(attempts[0], route.Race)
	if attempts[0].Succeeded && !raceLooksDegraded(attempts[0].Metrics, route.Race) {
		first.Body = addRaceMetadata(first.Body, decision.RouteName, attempts[0], attempts, route.Race)
		return first, nil
	}
	escalations := uniqueStrings(append(append([]string{}, route.Fallbacks...), route.References...))
	if route.Aggregator != "" {
		escalations = append([]string{route.Aggregator}, escalations...)
	}
	escalations = uniqueStrings(escalations)
	idx := len(attempts)
	for _, target := range escalations {
		if target == "" || target == primary {
			continue
		}
		res := s.callTargetBytes(ctx, target, APIChat, bodyWithRaceVariant(body, ""), r)
		s.metrics.Record(decision.RouteName+"/race-escalate", target, res.Status, res.Duration)
		att := raceAttempt{Index: idx, Target: target, Replica: 1, Result: res, Metrics: metricsFromResult(res, route.Race)}
		att.Succeeded = raceSucceeded(att.Result)
		att.Score = raceScore(att, route.Race)
		attempts = append(attempts, att)
		idx++
		if att.Succeeded && !raceLooksDegraded(att.Metrics, route.Race) && strings.EqualFold(route.Race.Selection, "fastest_acceptable") {
			res.Body = addRaceMetadata(res.Body, decision.RouteName, att, attempts, route.Race)
			return res, nil
		}
	}
	return selectRaceResult(attempts, route, decision.RouteName)
}

func (s *Server) runRaceAttempts(ctx context.Context, r *http.Request, body map[string]any, route RouteConfig, routeName string) []raceAttempt {
	plans := buildRacePlans(route)
	if len(plans) == 0 {
		return nil
	}
	parallelism := effectiveParallelism(route.Parallelism, len(plans))
	sem := make(chan struct{}, parallelism)
	out := make([]raceAttempt, len(plans))
	var wg sync.WaitGroup
	for i, p := range plans {
		i, p := i, p
		out[i] = raceAttempt{Index: i, Target: p.Target, Effort: p.Effort, Replica: p.Replica}
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				out[i].Result = UpstreamResult{TargetName: p.Target, Status: 0, Duration: 0, Err: ctx.Err()}
				out[i].Metrics = metricsFromResult(out[i].Result, route.Race)
				out[i].Score = raceScore(out[i], route.Race)
				return
			}
			variant := bodyWithRaceVariant(body, p.Effort)
			res := s.callTargetBytes(ctx, p.Target, APIChat, variant, r)
			s.metrics.Record(routeName+"/race", p.Target, res.Status, res.Duration)
			out[i].Result = res
			out[i].Metrics = metricsFromResult(res, route.Race)
			out[i].Succeeded = raceSucceeded(res)
			out[i].Score = raceScore(out[i], route.Race)
		}()
	}
	wg.Wait()
	return out
}

type racePlan struct {
	Target  string
	Effort  string
	Replica int
}

func buildRacePlans(route RouteConfig) []racePlan {
	targets := uniqueStrings(route.Race.Targets)
	if len(targets) == 0 {
		targets = uniqueStrings(append(append(append([]string{}, route.Candidates...), route.References...), route.Target))
	}
	if len(targets) == 0 && route.Aggregator != "" {
		targets = []string{route.Aggregator}
	}
	replicas := route.Race.Replicas
	if replicas <= 0 {
		replicas = 2
	}
	var out []racePlan
	if len(route.Race.Efforts) > 0 {
		for _, target := range targets {
			for _, effort := range route.Race.Efforts {
				out = append(out, racePlan{Target: target, Effort: strings.TrimSpace(effort), Replica: 1})
			}
		}
		return out
	}
	for _, target := range targets {
		for i := 0; i < replicas; i++ {
			out = append(out, racePlan{Target: target, Replica: i + 1})
		}
	}
	return out
}

func bodyWithRaceVariant(body map[string]any, effort string) map[string]any {
	out := cloneJSONMap(body)
	out["stream"] = false
	delete(out, "xrouter")
	if strings.TrimSpace(effort) != "" {
		reasoning := map[string]any{"effort": strings.TrimSpace(effort)}
		if existing, ok := out["reasoning"].(map[string]any); ok {
			for k, v := range existing {
				reasoning[k] = v
			}
			reasoning["effort"] = strings.TrimSpace(effort)
		}
		out["reasoning"] = reasoning
	}
	return out
}

func selectRaceResult(attempts []raceAttempt, route RouteConfig, routeName string) (UpstreamResult, error) {
	if len(attempts) == 0 {
		return UpstreamResult{}, fmt.Errorf("race flow has no attempts")
	}
	sorted := append([]raceAttempt(nil), attempts...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Score > sorted[j].Score
	})
	winner := sorted[0]
	for _, a := range sorted {
		if a.Succeeded {
			winner = a
			break
		}
	}
	res := winner.Result
	res.Body = addRaceMetadata(res.Body, routeName, winner, attempts, route.Race)
	return res, nil
}

func raceSucceeded(res UpstreamResult) bool {
	return res.Err == nil && res.Status >= 200 && res.Status < 300 && len(res.Body) > 0
}

func raceLooksDegraded(m raceMetrics, cfg RaceConfig) bool {
	if m.Incomplete || m.BoundaryHit {
		return true
	}
	if cfg.MinVisibleTokens > 0 && m.VisibleTokens < cfg.MinVisibleTokens {
		return true
	}
	return false
}

func raceScore(a raceAttempt, cfg RaceConfig) float64 {
	if !a.Succeeded {
		return -1_000_000 - float64(a.Index)
	}
	m := a.Metrics
	visibleWeight := cfg.VisibleWeight
	if visibleWeight <= 0 {
		visibleWeight = 1
	}
	outputWeight := cfg.OutputWeight
	reasonWeight := cfg.ReasoningWeight
	latencyWeight := cfg.LatencyWeight
	score := visibleWeight*float64(m.VisibleTokens) + outputWeight*float64(m.OutputTokens) + reasonWeight*float64(m.ReasoningTokens)
	switch strings.ToLower(strings.TrimSpace(cfg.Selection)) {
	case "usage_total":
		score += float64(m.TotalTokens)
	case "max_output", "longest_output":
		score += 3 * float64(m.VisibleTokens)
	case "fastest_acceptable":
		if !raceLooksDegraded(m, cfg) {
			score += 100_000
		}
	case "boundary_aware", "degradation_guard", "guarded_max_output":
		// Defaults already include boundary/incomplete penalties below.
	}
	if cfg.MinVisibleTokens > 0 && m.VisibleTokens < cfg.MinVisibleTokens {
		score -= float64(cfg.MinVisibleTokens - m.VisibleTokens)
	}
	if m.BoundaryHit {
		pen := cfg.BoundaryPenalty
		if pen <= 0 {
			pen = 2500
		}
		score -= pen
	}
	if m.Incomplete {
		pen := cfg.IncompletePenalty
		if pen <= 0 {
			pen = 5000
		}
		score -= pen
	}
	if latencyWeight > 0 {
		score -= latencyWeight * float64(a.Result.Duration.Milliseconds())
	}
	return score
}

func metricsFromResult(res UpstreamResult, cfg RaceConfig) raceMetrics {
	m := raceMetrics{}
	if res.Err != nil {
		m.Status = res.Err.Error()
		m.Incomplete = true
		return m
	}
	var obj map[string]any
	if err := json.Unmarshal(res.Body, &obj); err != nil {
		m.VisibleTokens = approxTokenCount(string(res.Body))
		return m
	}
	text := extractChatText(obj)
	m.VisibleTokens = approxTokenCount(text)
	m.OutputTokens = firstPositiveNumber(
		numberAt(obj, "usage", "completion_tokens"),
		numberAt(obj, "usage", "output_tokens"),
		numberAt(obj, "token_count", "output_tokens"),
	)
	m.TotalTokens = firstPositiveNumber(
		numberAt(obj, "usage", "total_tokens"),
		numberAt(obj, "token_count", "total_tokens"),
	)
	m.ReasoningTokens = firstPositiveNumber(
		numberAt(obj, "usage", "completion_tokens_details", "reasoning_tokens"),
		numberAt(obj, "usage", "output_tokens_details", "reasoning_tokens"),
		numberAt(obj, "usage", "reasoning_output_tokens"),
		numberAt(obj, "token_count", "reasoning_output_tokens"),
		findNumberByKey(obj, "reasoning_output_tokens"),
	)
	m.FinishReason = firstNonEmpty(
		stringAt(obj, "choices", "0", "finish_reason"),
		stringAt(obj, "incomplete_details", "reason"),
	)
	m.Status = firstNonEmpty(stringFromAny(obj["status"]), m.FinishReason)
	lower := strings.ToLower(m.Status + " " + m.FinishReason)
	m.Incomplete = strings.Contains(lower, "incomplete") || strings.Contains(lower, "max_tokens") || strings.Contains(lower, "max_output_tokens") || strings.Contains(lower, "length")
	m.BoundaryHit = reasoningBoundaryHit(m.ReasoningTokens, cfg)
	return m
}

func reasoningBoundaryHit(tokens int, cfg RaceConfig) bool {
	if tokens <= 0 {
		return false
	}
	start := cfg.BoundaryStart
	if start <= 0 {
		start = 516
	}
	step := cfg.BoundaryStep
	if step <= 0 {
		step = 518
	}
	tol := cfg.BoundaryTolerance
	if tol < 0 {
		tol = 0
	}
	if tokens < start-tol {
		return false
	}
	delta := tokens - start
	rem := ((delta % step) + step) % step
	return rem <= tol || step-rem <= tol
}

func approxTokenCount(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	words := len(strings.Fields(s))
	runes := utf8.RuneCountInString(s)
	byChars := int(math.Ceil(float64(runes) / 4.0))
	if words > byChars {
		return words
	}
	return byChars
}

func numberAt(obj map[string]any, path ...string) int {
	var cur any = obj
	for _, key := range path {
		switch x := cur.(type) {
		case map[string]any:
			cur = x[key]
		case []any:
			idx := -1
			_, _ = fmt.Sscanf(key, "%d", &idx)
			if idx < 0 || idx >= len(x) {
				return 0
			}
			cur = x[idx]
		default:
			return 0
		}
	}
	switch v := cur.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}

func stringAt(obj map[string]any, path ...string) string {
	var cur any = obj
	for _, key := range path {
		switch x := cur.(type) {
		case map[string]any:
			cur = x[key]
		case []any:
			idx := -1
			_, _ = fmt.Sscanf(key, "%d", &idx)
			if idx < 0 || idx >= len(x) {
				return ""
			}
			cur = x[idx]
		default:
			return ""
		}
	}
	return stringFromAny(cur)
}

func firstPositiveNumber(vals ...int) int {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}

func findNumberByKey(v any, key string) int {
	switch x := v.(type) {
	case map[string]any:
		for k, vv := range x {
			if strings.EqualFold(k, key) {
				switch n := vv.(type) {
				case float64:
					return int(n)
				case int:
					return n
				}
			}
			if n := findNumberByKey(vv, key); n > 0 {
				return n
			}
		}
	case []any:
		for _, vv := range x {
			if n := findNumberByKey(vv, key); n > 0 {
				return n
			}
		}
	}
	return 0
}

func addRaceMetadata(raw []byte, route string, winner raceAttempt, attempts []raceAttempt, cfg RaceConfig) []byte {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return raw
	}
	xr, _ := obj["xrouter"].(map[string]any)
	if xr == nil {
		xr = map[string]any{}
	}
	meta := map[string]any{
		"route":         route,
		"strategy":      "race",
		"selection":     cfg.Selection,
		"winner_target": winner.Target,
		"winner_effort": winner.Effort,
		"winner_score":  winner.Score,
		"winner":        winner.Metrics,
	}
	if cfg.IncludeDebug {
		items := make([]any, 0, len(attempts))
		for _, a := range attempts {
			items = append(items, map[string]any{"index": a.Index, "target": a.Target, "effort": a.Effort, "replica": a.Replica, "status": a.Result.Status, "score": a.Score, "metrics": a.Metrics})
		}
		meta["attempts"] = items
	}
	xr["race"] = meta
	obj["xrouter"] = xr
	b, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return b
}

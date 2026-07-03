package main

import (
	"math"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"
)

type scoredTarget struct {
	Name  string
	Score float64
}

type requestFeatures struct {
	Text        string
	NeedsTools  bool
	NeedsJSON   bool
	NeedsVision bool
	Complexity  float64
}

func (s *Server) routeCandidates(route RouteConfig, body map[string]any, kind APIKind, sessionID string, r *http.Request, controls XRouterControls) []string {
	features := analyzeRequest(body, kind)
	candidates := route.Candidates
	if len(candidates) == 0 {
		for name := range s.cfg.Targets {
			candidates = append(candidates, name)
		}
	}
	candidates = uniqueStrings(candidates)
	filtered := make([]string, 0, len(candidates))
	for _, name := range candidates {
		t, ok := s.targetByName(name)
		if !ok {
			continue
		}
		if !targetCompatibleWithFeatures(t, features, kind) {
			continue
		}
		filtered = append(filtered, name)
	}
	if len(filtered) == 0 {
		return nil
	}
	stickyName := ""
	if sticky, ok := s.sticky.Get(sessionID); ok && stringInSlice(sticky, filtered) {
		stickyName = sticky
	}
	objective := objectiveFrom(body, route)
	if controls.Objective != "" {
		objective = controls.Objective
	}
	complexity := features.Complexity
	maxCost, maxLatency := maxCostAndLatency(s, filtered)
	prefixKey := ""
	cacheScores := map[string]float64{}
	if s.prefixBK != nil && s.prefixBK.Enabled(route, controls) {
		if key, ok := s.prefixBK.PrefixKey(body, kind, route, controls); ok {
			prefixKey = key
			cacheScores = s.prefixBK.Scores(key, filtered, s.cfg.Targets, route)
		}
	}
	_ = prefixKey // kept for explicit BK decision trace in future persistent traces.
	judgeScores := map[string]float64{}
	if route.Judge.Enabled {
		if controls.JudgeEnabled != nil && !*controls.JudgeEnabled {
			judgeScores = map[string]float64{}
		} else {
			judgeScores = s.judgeRouterScores(r, body, route, filtered)
		}
	}
	scored := make([]scoredTarget, 0, len(filtered))
	for _, name := range filtered {
		t, _ := s.targetByName(name)
		score := scoreTargetV3WithFeatures(t, route, objective, complexity, maxCost, maxLatency, cacheScores[name], judgeScores[name], stickyName == name, features)
		score += keywordRuleScore(route.KeywordRules, t, name, features.Text)
		score += s.metrics.TargetScoreAdjustment(name, maxLatency)
		scored = append(scored, scoredTarget{Name: name, Score: score})
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
	out := make([]string, 0, len(scored))
	for _, st := range scored {
		out = append(out, st.Name)
	}
	return out
}

func maxCostAndLatency(s *Server, candidates []string) (float64, float64) {
	maxCost := 0.0
	maxLatency := 0.0
	for _, name := range candidates {
		t, _ := s.targetByName(name)
		cost := t.CostIn + t.CostOut
		if cost > maxCost {
			maxCost = cost
		}
		if t.LatencyMS > maxLatency {
			maxLatency = t.LatencyMS
		}
	}
	if maxCost <= 0 {
		maxCost = 1
	}
	if maxLatency <= 0 {
		maxLatency = 1
	}
	return maxCost, maxLatency
}

func (s *Server) targetCompatible(t TargetConfig, body map[string]any, kind APIKind) bool {
	return targetCompatibleWithFeatures(t, analyzeRequest(body, kind), kind)
}

func targetCompatibleWithFeatures(t TargetConfig, features requestFeatures, kind APIKind) bool {
	if kind == APIResponses && !t.Capabilities.Responses {
		// Chat-only targets can still be used through XRouter's Responses shim.
		if t.Provider == "" {
			return false
		}
	}
	if features.NeedsTools && !t.Capabilities.Tools {
		return false
	}
	if features.NeedsJSON && !t.Capabilities.JSON {
		return false
	}
	if features.NeedsVision && !t.Capabilities.Vision {
		return false
	}
	return true
}

func scoreTargetV3(t TargetConfig, route RouteConfig, objective string, complexity, maxCost, maxLatency, cacheScore, judgeScore float64, sticky bool, body map[string]any) float64 {
	return scoreTargetV3WithFeatures(t, route, objective, complexity, maxCost, maxLatency, cacheScore, judgeScore, sticky, analyzeRequest(body, APIChat))
}

func scoreTargetV3WithFeatures(t TargetConfig, route RouteConfig, objective string, complexity, maxCost, maxLatency, cacheScore, judgeScore float64, sticky bool, features requestFeatures) float64 {
	costScore := 1.0 - math.Min(1, (t.CostIn+t.CostOut)/maxCost)
	latencyScore := 1.0 - math.Min(1, t.LatencyMS/maxLatency)
	quality := clamp01(t.Quality)
	reliability := clamp01(t.Reliability)
	capability := capabilityFitScoreWithFeatures(t, features)
	w := defaultSmartWeights(objective, route.Weights)
	score := w.Quality*quality + w.Cost*costScore + w.Latency*latencyScore + w.Reliability*reliability + w.Capability*capability + w.Cache*cacheScore + w.Judge*judgeScore
	if sticky {
		score += w.Sticky
	}
	// Complex prompts should tilt toward quality even when the objective is not pure quality.
	score += 0.12 * complexity * quality
	return score
}

func capabilityFitScore(t TargetConfig, body map[string]any) float64 {
	return capabilityFitScoreWithFeatures(t, analyzeRequest(body, APIChat))
}

func capabilityFitScoreWithFeatures(t TargetConfig, features requestFeatures) float64 {
	score := 0.5
	need := 0
	fit := 0
	if features.NeedsTools {
		need++
		if t.Capabilities.Tools {
			fit++
		}
	}
	if features.NeedsJSON {
		need++
		if t.Capabilities.JSON {
			fit++
		}
	}
	if features.NeedsVision {
		need++
		if t.Capabilities.Vision {
			fit++
		}
	}
	if need == 0 {
		return score
	}
	return float64(fit) / float64(need)
}

func keywordRuleScore(rules []KeywordRule, t TargetConfig, targetName, text string) float64 {
	if len(rules) == 0 {
		return 0
	}
	text = strings.ToLower(text)
	tagSet := map[string]struct{}{}
	for _, tag := range t.Tags {
		tagSet[strings.ToLower(strings.TrimSpace(tag))] = struct{}{}
	}
	score := 0.0
	for _, rule := range rules {
		if !ruleMatchesText(rule, text) {
			continue
		}
		applies := len(rule.Targets) == 0 && len(rule.Tags) == 0
		for _, name := range rule.Targets {
			if strings.TrimSpace(name) == targetName {
				applies = true
				break
			}
		}
		if !applies {
			for _, tag := range rule.Tags {
				if _, ok := tagSet[strings.ToLower(strings.TrimSpace(tag))]; ok {
					applies = true
					break
				}
			}
		}
		if !applies {
			continue
		}
		boost := rule.Boost
		if boost == 0 {
			boost = 0.05
		}
		score += boost - rule.Penalty
	}
	return score
}

func ruleMatchesText(rule KeywordRule, text string) bool {
	if len(rule.Any) > 0 {
		ok := false
		for _, kw := range rule.Any {
			if strings.Contains(text, strings.ToLower(kw)) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for _, kw := range rule.All {
		if !strings.Contains(text, strings.ToLower(kw)) {
			return false
		}
	}
	return true
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func estimateComplexity(body map[string]any, kind APIKind) float64 {
	return analyzeRequest(body, kind).Complexity
}

func analyzeRequest(body map[string]any, kind APIKind) requestFeatures {
	text := requestText(body, kind)
	chars := utf8.RuneCountInString(text)
	score := math.Min(0.45, float64(chars)/8000.0)
	lower := strings.ToLower(text)
	markers := []string{"```", "prove", "derive", "architecture", "debug", "refactor", "security", "benchmark", "math", "sql", "rust", "golang", "kubernetes", "distributed", "并发", "架构", "推理", "证明", "代码"}
	for _, m := range markers {
		if strings.Contains(lower, m) {
			score += 0.06
		}
	}
	features := requestFeatures{
		Text:        text,
		NeedsTools:  requestNeedsTools(body),
		NeedsJSON:   requestNeedsJSON(body),
		NeedsVision: requestNeedsVision(body),
	}
	if features.NeedsTools {
		score += 0.12
	}
	if features.NeedsJSON {
		score += 0.08
	}
	if features.NeedsVision {
		score += 0.15
	}
	features.Complexity = clamp01(score)
	return features
}

func requestText(body map[string]any, kind APIKind) string {
	var b strings.Builder
	if kind == APIChat {
		if msgs, ok := body["messages"].([]any); ok {
			for _, msg := range msgs {
				if mm, ok := msg.(map[string]any); ok {
					b.WriteString(" ")
					b.WriteString(contentToText(mm["content"]))
				}
			}
		}
	} else {
		if s, ok := body["instructions"].(string); ok {
			b.WriteString(s)
			b.WriteString(" ")
		}
		b.WriteString(contentToText(body["input"]))
	}
	return b.String()
}

func contentToText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		parts := make([]string, 0, len(x))
		for _, p := range x {
			switch pp := p.(type) {
			case string:
				parts = append(parts, pp)
			case map[string]any:
				for _, key := range []string{"text", "input_text", "output_text"} {
					if s, ok := pp[key].(string); ok {
						parts = append(parts, s)
					}
				}
			default:
				parts = append(parts, compactJSON(pp))
			}
		}
		return strings.Join(parts, " ")
	case map[string]any:
		return compactJSON(x)
	default:
		if v == nil {
			return ""
		}
		return compactJSON(v)
	}
}

func requestNeedsTools(body map[string]any) bool {
	if tools, ok := body["tools"].([]any); ok && len(tools) > 0 {
		return true
	}
	return body["tool_choice"] != nil
}

func requestNeedsJSON(body map[string]any) bool {
	if rf, ok := body["response_format"].(map[string]any); ok {
		if typ, _ := rf["type"].(string); strings.Contains(strings.ToLower(typ), "json") {
			return true
		}
	}
	if text, ok := body["text"].(map[string]any); ok {
		if format, ok := text["format"].(map[string]any); ok {
			if typ, _ := format["type"].(string); strings.Contains(strings.ToLower(typ), "json") {
				return true
			}
		}
	}
	return false
}

func requestNeedsVision(body map[string]any) bool {
	if msgs, ok := body["messages"].([]any); ok {
		for _, msg := range msgs {
			if mm, ok := msg.(map[string]any); ok && contentNeedsVision(mm["content"]) {
				return true
			}
		}
	}
	return contentNeedsVision(body["input"])
}

func contentNeedsVision(v any) bool {
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			if contentNeedsVision(item) {
				return true
			}
		}
	case map[string]any:
		typ := strings.ToLower(strings.TrimSpace(stringFromAny(x["type"])))
		if typ == "image_url" || typ == "input_image" {
			return true
		}
		if _, ok := x["image_url"]; ok {
			return true
		}
		for _, key := range []string{"content", "input"} {
			if contentNeedsVision(x[key]) {
				return true
			}
		}
	}
	return false
}

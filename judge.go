package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type judgeDecision struct {
	Target     string             `json:"target"`
	Confidence float64            `json:"confidence"`
	Scores     map[string]float64 `json:"scores"`
	Reason     string             `json:"reason"`
}

func (s *Server) judgeRouterScores(r *http.Request, body map[string]any, route RouteConfig, candidates []string) map[string]float64 {
	out := map[string]float64{}
	if !route.Judge.Enabled || route.Judge.Target == "" || len(candidates) == 0 {
		return out
	}
	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
	}
	timeout := time.Duration(route.Judge.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 1200 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	judgeBody := buildJudgeRouterBody(body, route, candidates)
	res := s.callTargetBytes(ctx, route.Judge.Target, APIChat, judgeBody, r)
	s.metrics.Record("judge_router", route.Judge.Target, res.Status, res.Duration)
	if res.Err != nil || res.Status < 200 || res.Status >= 300 {
		return out
	}
	var obj map[string]any
	if err := json.Unmarshal(res.Body, &obj); err != nil {
		return out
	}
	text := strings.TrimSpace(extractChatText(obj))
	if text == "" {
		return out
	}
	var jd judgeDecision
	if err := json.Unmarshal([]byte(extractJSONObject(text)), &jd); err != nil {
		// Fallback: allow the judge to return a bare target name.
		for _, c := range candidates {
			if strings.Contains(text, c) {
				out[c] = 1
				return out
			}
		}
		return out
	}
	if len(jd.Scores) > 0 {
		for _, c := range candidates {
			out[c] = clamp01(jd.Scores[c])
		}
	}
	if jd.Target != "" && stringInSlice(jd.Target, candidates) {
		conf := jd.Confidence
		if conf <= 0 {
			conf = 0.75
		}
		if out[jd.Target] < conf {
			out[jd.Target] = clamp01(conf)
		}
	}
	return out
}

func buildJudgeRouterBody(body map[string]any, route RouteConfig, candidates []string) map[string]any {
	prompt := strings.TrimSpace(route.Judge.Prompt)
	if prompt == "" {
		prompt = "You are a routing judge. Choose exactly one target from the candidate list for the request. Return compact JSON only: {\"target\":\"...\",\"confidence\":0.0,\"scores\":{...},\"reason\":\"...\"}."
	}
	var b strings.Builder
	b.WriteString(prompt)
	b.WriteString("\n\nCandidates:\n")
	for _, c := range candidates {
		b.WriteString("- ")
		b.WriteString(c)
		b.WriteString("\n")
	}
	b.WriteString("\nUser request text:\n")
	b.WriteString(requestText(body, APIChat))
	return map[string]any{
		"model":           route.Judge.Target,
		"stream":          false,
		"temperature":     0,
		"response_format": map[string]any{"type": "json_object"},
		"messages": []any{
			map[string]any{"role": "system", "content": "Return valid JSON only."},
			map[string]any{"role": "user", "content": b.String()},
		},
	}
}

func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start >= 0 && end >= start {
		return s[start : end+1]
	}
	return fmt.Sprintf("{\"target\":%q}", s)
}

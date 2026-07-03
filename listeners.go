package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

func defaultListenerPrompt() string {
	return "You are an XRouter listener. Review the request and primary answer. Return concise JSON with keys: verdict, risks, notes. Do not answer the user directly."
}

func (s *Server) launchShadowCalls(r *http.Request, body map[string]any, decision RouteDecision, kind APIKind, exclude ...string) {
	if decision.Controls.DisableShadow {
		return
	}
	targets := uniqueStrings(append(append([]string{}, decision.Route.ShadowTargets...), decision.Controls.ShadowTargets...))
	if len(targets) == 0 {
		return
	}
	limit := s.cfg.RequestOverrides.MaxShadowTargets
	if limit <= 0 {
		limit = 4
	}
	excludeSet := map[string]struct{}{}
	for _, x := range exclude {
		excludeSet[x] = struct{}{}
	}
	asyncReq := cloneIncomingRequest(r)
	count := 0
	for _, target := range targets {
		if _, skip := excludeSet[target]; skip {
			continue
		}
		count++
		if count > limit {
			break
		}
		target := target
		shadowBody := cloneTopLevelJSONMap(body)
		shadowBody["stream"] = false
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			res := s.callTargetBytes(ctx, target, kind, shadowBody, asyncReq)
			s.metrics.Record(decision.RouteName+"/shadow", target, res.Status, res.Duration)
		}()
	}
}

func cloneIncomingRequest(r *http.Request) *http.Request {
	if r == nil {
		return &http.Request{Header: http.Header{}}
	}
	return &http.Request{Header: r.Header.Clone()}
}

func (s *Server) runSerialListeners(ctx context.Context, r *http.Request, body map[string]any, decision RouteDecision, kind APIKind, primary UpstreamResult) UpstreamResult {
	if decision.Controls.DisableListeners || primary.Status < 200 || primary.Status >= 300 {
		return primary
	}
	listeners := s.listenersForDecision(decision)
	if len(listeners) == 0 {
		return primary
	}
	outputs := make([]map[string]any, 0, len(listeners))
	attach := decision.Controls.IncludeListenerOutput
	for _, listener := range listeners {
		if listener.Target == "" || listener.Mode != "serial" {
			continue
		}
		if listener.SampleRate > 0 && listener.SampleRate < 1 && rand.Float64() > listener.SampleRate {
			continue
		}
		if listener.IncludeResponse {
			attach = true
		}
		timeout := listener.TimeoutMS
		if timeout <= 0 {
			timeout = 15000
		}
		lctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
		listenerBody := buildListenerBody(body, decision, kind, primary, listener)
		res := s.callTargetBytes(lctx, listener.Target, APIChat, listenerBody, r)
		cancel()
		s.metrics.Record(decision.RouteName+"/listener", listener.Target, res.Status, res.Duration)
		entry := map[string]any{
			"name":     listener.Name,
			"target":   listener.Target,
			"status":   res.Status,
			"duration": res.Duration.Milliseconds(),
		}
		if entry["name"] == "" {
			entry["name"] = listener.Target
		}
		if res.Err != nil {
			entry["error"] = res.Err.Error()
		} else if res.Status < 200 || res.Status >= 300 {
			entry["error"] = strings.TrimSpace(string(res.Body))
		} else {
			var obj map[string]any
			if err := json.Unmarshal(res.Body, &obj); err == nil {
				entry["text"] = extractChatText(obj)
			} else {
				entry["text"] = strings.TrimSpace(string(res.Body))
			}
		}
		outputs = append(outputs, entry)
	}
	if attach && len(outputs) > 0 {
		primary.Body = attachXRouterMetadata(primary.Body, map[string]any{
			"route":            decision.RouteName,
			"target":           primary.TargetName,
			"upstream_model":   primary.Target.Model,
			"execution_type":   decision.Route.Type,
			"complexity":       decision.Complexity,
			"serial_listeners": outputs,
		})
	}
	return primary
}

func (s *Server) listenersForDecision(decision RouteDecision) []ListenerConfig {
	out := append([]ListenerConfig{}, decision.Route.SerialListeners...)
	for _, target := range decision.Controls.ListenerTargets {
		out = append(out, ListenerConfig{
			Name:            target,
			Target:          target,
			Mode:            "serial",
			Prompt:          defaultListenerPrompt(),
			TimeoutMS:       15000,
			SampleRate:      1,
			IncludeResponse: decision.Controls.IncludeListenerOutput,
		})
	}
	limit := s.cfg.RequestOverrides.MaxListenerTargets
	if limit <= 0 {
		limit = 4
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func buildListenerBody(original map[string]any, decision RouteDecision, kind APIKind, primary UpstreamResult, listener ListenerConfig) map[string]any {
	prompt := listener.Prompt
	if strings.TrimSpace(prompt) == "" {
		prompt = defaultListenerPrompt()
	}
	content := fmt.Sprintf("%s\n\nAPI kind: %s\nRoute: %s\nPrimary target: %s\nPrimary upstream model: %s\n\nOriginal request JSON:\n%s\n\nPrimary response JSON:\n%s",
		prompt, kind, decision.RouteName, primary.TargetName, primary.Target.Model, truncateRunes(compactJSON(original), 4000), truncateRunes(strings.TrimSpace(string(primary.Body)), 4000))
	return map[string]any{
		"model":       listener.Target,
		"temperature": 0,
		"stream":      false,
		"messages": []any{
			map[string]any{"role": "system", "content": "You are a side-channel evaluator for an LLM gateway."},
			map[string]any{"role": "user", "content": content},
		},
		"response_format": map[string]any{"type": "json_object"},
	}
}

func attachXRouterMetadata(raw []byte, meta map[string]any) []byte {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return raw
	}
	xr, _ := obj["xrouter"].(map[string]any)
	if xr == nil {
		xr = map[string]any{}
	}
	for k, v := range meta {
		xr[k] = v
	}
	obj["xrouter"] = xr
	b, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return b
}

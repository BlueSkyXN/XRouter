package main

import (
	"fmt"
	"net/http"
	"strings"
)

func (s *Server) controlsFromRequest(r *http.Request, body map[string]any) (XRouterControls, error) {
	var c XRouterControls
	if !s.cfg.RequestOverrides.Enabled {
		return c, nil
	}
	x := requestExtension(body)
	c.Route = firstNonEmpty(stringFromAny(x["route"]), r.Header.Get("x-xrouter-route"))
	c.Mode = normalizeMode(firstNonEmpty(stringFromAny(x["mode"]), r.Header.Get("x-xrouter-mode")))
	c.Target = firstNonEmpty(stringFromAny(x["target"]), r.Header.Get("x-xrouter-target"))
	var err error
	maxRoutingTargets := s.cfg.RequestOverrides.MaxRoutingTargets
	if maxRoutingTargets <= 0 {
		maxRoutingTargets = 32
	}
	c.Targets, err = boundedControlList("targets", firstNonEmptyList(listFromAny(x["targets"]), splitCSV(r.Header.Get("x-xrouter-targets"))), maxRoutingTargets)
	if err != nil {
		return c, err
	}
	c.Candidates, err = boundedControlList("candidates", firstNonEmptyList(listFromAny(x["candidates"]), splitCSV(r.Header.Get("x-xrouter-candidates"))), maxRoutingTargets)
	if err != nil {
		return c, err
	}
	c.References, err = boundedControlList("references", firstNonEmptyList(listFromAny(x["references"]), splitCSV(r.Header.Get("x-xrouter-references"))), maxRoutingTargets)
	if err != nil {
		return c, err
	}
	c.Aggregator = firstNonEmpty(stringFromAny(x["aggregator"]), r.Header.Get("x-xrouter-aggregator"))
	c.Objective = strings.ToLower(firstNonEmpty(stringFromAny(x["objective"]), r.Header.Get("x-xrouter-objective")))
	c.MultiModel = normalizeMultiModel(firstNonEmpty(stringFromAny(x["multi_model"]), r.Header.Get("x-xrouter-multi-model")))
	c.SessionID = firstNonEmpty(stringFromAny(x["session_id"]), r.Header.Get("x-xrouter-session-id"), r.Header.Get("x-session-id"))
	c.DryRun = boolFromAny(x["dry_run"]) || headerBool(r, "x-xrouter-dry-run")
	c.Explain = boolFromAny(x["explain"]) || headerBool(r, "x-xrouter-explain")
	c.CachePrefixHint = firstNonEmpty(stringFromAny(x["cache_prefix_hint"]), stringFromAny(nestedMap(x, "cache")["prefix_hint"]), r.Header.Get("x-xrouter-cache-prefix-hint"))
	c.DisablePrefixCache = boolFromAny(x["disable_prefix_cache"]) || headerBool(r, "x-xrouter-disable-prefix-cache")
	if v, ok := x["judge_enabled"]; ok {
		b := boolFromAny(v)
		c.JudgeEnabled = &b
	}
	c.ShadowTargets, err = boundedControlList("shadow_targets", firstNonEmptyList(listFromAny(x["shadow_targets"]), splitCSV(r.Header.Get("x-xrouter-shadow-targets"))), s.cfg.RequestOverrides.MaxShadowTargets)
	if err != nil {
		return c, err
	}
	c.ListenerTargets, err = boundedControlList("listener_targets", firstNonEmptyList(listFromAny(x["listener_targets"]), splitCSV(r.Header.Get("x-xrouter-listener-targets"))), s.cfg.RequestOverrides.MaxListenerTargets)
	if err != nil {
		return c, err
	}
	c.IncludeListenerOutput = boolFromAny(x["include_listener_output"]) || headerBool(r, "x-xrouter-include-listeners")
	c.DisableListeners = boolFromAny(x["disable_listeners"]) || headerBool(r, "x-xrouter-disable-listeners")
	c.DisableShadow = boolFromAny(x["disable_shadow"]) || headerBool(r, "x-xrouter-disable-shadow")
	c.ProviderAPIKeys = providerAPIKeysFromExtension(x)
	if len(c.ProviderAPIKeys) > 0 {
		body[internalProviderAPIKeysKey] = c.ProviderAPIKeys
	}
	return c, nil
}

func applyControlsToRoute(route RouteConfig, c XRouterControls) RouteConfig {
	if c.Mode != "" && c.Mode != "bypass" {
		route.Type = normalizeRouteKind(c.Mode)
		route.Kind = route.Type
	}
	if c.Mode == "bypass" {
		route.Type = "direct"
		route.Kind = "direct"
	}
	if c.Target != "" {
		route.Target = c.Target
	}
	if len(c.Targets) > 0 {
		route.Candidates = c.Targets
	}
	if len(c.Candidates) > 0 {
		route.Candidates = c.Candidates
	}
	if len(c.References) > 0 {
		route.References = c.References
	}
	if c.Aggregator != "" {
		route.Aggregator = c.Aggregator
	}
	if c.Objective != "" {
		route.Objective = c.Objective
	}
	if c.MultiModel != "" {
		route.MultiModel = c.MultiModel
	}
	return route
}

func sessionIDWithControls(r *http.Request, body map[string]any, c XRouterControls) string {
	if strings.TrimSpace(c.SessionID) != "" {
		return strings.TrimSpace(c.SessionID)
	}
	return sessionIDFromRequest(r, body)
}

func normalizeMode(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "direct", "auto", "moa", "mov", "bypass", "passthrough":
		return s
	default:
		return ""
	}
}

func normalizeMultiModel(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "true", "yes", "on", "always":
		return "always"
	case "false", "no", "off", "never":
		return "never"
	case "auto":
		return "auto"
	default:
		return ""
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func firstNonEmptyList(lists ...[]string) []string {
	for _, xs := range lists {
		if len(uniqueStrings(xs)) > 0 {
			return uniqueStrings(xs)
		}
	}
	return nil
}

func boundedControlList(name string, xs []string, max int) ([]string, error) {
	xs = uniqueStrings(xs)
	if max <= 0 {
		max = 1
	}
	if len(xs) > max {
		return nil, fmt.Errorf("request override %s has %d entries, max %d", name, len(xs), max)
	}
	return xs, nil
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	return uniqueStrings(parts)
}

func stringFromAny(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func boolFromAny(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return s == "true" || s == "1" || s == "yes" || s == "on"
	default:
		return false
	}
}

func headerBool(r *http.Request, name string) bool {
	return boolFromAny(r.Header.Get(name))
}

func listFromAny(v any) []string {
	switch x := v.(type) {
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s := stringFromAny(item); s != "" {
				out = append(out, s)
			}
		}
		return uniqueStrings(out)
	case []string:
		return uniqueStrings(x)
	case string:
		return splitCSV(x)
	default:
		return nil
	}
}

func mapStringStringFromAny(v any) map[string]string {
	out := map[string]string{}
	m, ok := v.(map[string]any)
	if !ok {
		return out
	}
	for k, vv := range m {
		if s := stringFromAny(vv); s != "" {
			out[strings.ToLower(strings.TrimSpace(k))] = s
		}
	}
	return out
}

func providerAPIKeysFromExtension(x map[string]any) map[string]string {
	out := mapStringStringFromAny(x["provider_api_keys"])
	for k, v := range mapStringStringFromAny(x["provider_keys"]) {
		if _, exists := out[k]; !exists {
			out[k] = v
		}
	}
	return out
}

func nestedMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	if x, ok := m[key].(map[string]any); ok {
		return x
	}
	return map[string]any{}
}

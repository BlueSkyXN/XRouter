package main

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	body, err := readJSONBody(w, r, s.cfg.Server.MaxRequestBodyBytes)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	model := strings.TrimSpace(getString(body, "model"))
	if model == "" {
		writeError(w, http.StatusBadRequest, "missing_model", "request.model is required")
		return
	}
	decision, err := s.resolve(model, body, APIChat, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "route_error", err.Error())
		return
	}
	if decision.Controls.DryRun {
		s.writeDryRun(w, decision)
		return
	}
	if decision.Route.Type == "moa" {
		s.handleMoAChat(w, r, body, decision)
		return
	}
	if decision.Route.Type == "mov" {
		s.handleMoVChat(w, r, body, decision)
		return
	}
	s.handleDirectLike(w, r, body, decision, APIChat)
}

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	body, err := readJSONBody(w, r, s.cfg.Server.MaxRequestBodyBytes)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	model := strings.TrimSpace(getString(body, "model"))
	if model == "" {
		writeError(w, http.StatusBadRequest, "missing_model", "request.model is required")
		return
	}
	decision, err := s.resolve(model, body, APIResponses, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "route_error", err.Error())
		return
	}
	if decision.Controls.DryRun {
		s.writeDryRun(w, decision)
		return
	}
	if decision.Route.Type == "moa" {
		s.handleMoAResponses(w, r, body, decision)
		return
	}
	if decision.Route.Type == "mov" {
		s.handleMoVResponses(w, r, body, decision)
		return
	}
	s.handleResponsesDirectLike(w, r, body, decision)
}

func (s *Server) resolve(model string, body map[string]any, kind APIKind, r *http.Request) (RouteDecision, error) {
	controls, err := s.controlsFromRequest(r, body)
	if err != nil {
		return RouteDecision{}, err
	}
	lookup := model
	if controls.Route != "" {
		lookup = controls.Route
	}
	complexity := estimateComplexity(body, kind)

	if controls.Target != "" && (controls.Mode == "" || controls.Mode == "direct" || controls.Mode == "bypass") {
		route := RouteConfig{Type: "direct", Kind: "direct", Target: controls.Target, MultiModel: "never", StickyTTLSeconds: 300}
		return RouteDecision{PublicModel: model, RouteName: lookupOrModel(controls.Route, model), Route: route, TargetNames: []string{controls.Target}, IsVirtual: true, Controls: controls, Complexity: complexity}, nil
	}
	if len(controls.Targets) > 0 && (controls.Mode == "" || controls.Mode == "direct" || controls.Mode == "bypass") {
		route := RouteConfig{Type: "direct", Kind: "direct", Target: controls.Targets[0], MultiModel: "never", StickyTTLSeconds: 300}
		return RouteDecision{PublicModel: model, RouteName: lookupOrModel(controls.Route, model), Route: route, TargetNames: uniqueStrings(controls.Targets), IsVirtual: true, Controls: controls, Complexity: complexity}, nil
	}

	if configured, routeName, ok := s.resolveRouteByModelID(lookup); ok {
		return s.materializeRouteDecision(model, routeName, configured, body, kind, r, controls, complexity)
	}

	if _, ok := s.configuredTargetByName(lookup); ok {
		return RouteDecision{
			PublicModel: model,
			RouteName:   lookup,
			Route:       RouteConfig{Type: "direct", Kind: "direct", Target: lookup, MultiModel: "never", StickyTTLSeconds: 300},
			TargetNames: []string{lookup},
			IsVirtual:   false,
			Controls:    controls,
			Complexity:  complexity,
		}, nil
	}

	if s.cfg.Routing.DefaultRoute != "" {
		if configured, ok := s.cfg.Routes[s.cfg.Routing.DefaultRoute]; ok {
			return s.materializeRouteDecision(model, s.cfg.Routing.DefaultRoute, configured, body, kind, r, controls, complexity)
		}
	}

	switch normalizeUnknownModelPolicy(s.cfg.Routing.UnknownModelPolicy) {
	case "passthrough_openrouter":
		if _, ok := s.passthroughTarget(lookup, "openrouter"); !ok {
			return RouteDecision{}, fmt.Errorf("unknown model or route %q: passthrough_openrouter requires configured provider %q", lookup, "openrouter")
		}
		return RouteDecision{PublicModel: model, RouteName: lookup, Route: RouteConfig{Type: "direct", Kind: "direct", Target: lookup, MultiModel: "never", StickyTTLSeconds: 300}, TargetNames: []string{lookup}, IsVirtual: false, Controls: controls, Complexity: complexity}, nil
	case "passthrough_openai":
		if _, ok := s.passthroughTarget(lookup, "openai"); !ok {
			return RouteDecision{}, fmt.Errorf("unknown model or route %q: passthrough_openai requires configured provider %q", lookup, "openai")
		}
		return RouteDecision{PublicModel: model, RouteName: lookup, Route: RouteConfig{Type: "direct", Kind: "direct", Target: lookup, MultiModel: "never", StickyTTLSeconds: 300}, TargetNames: []string{lookup}, IsVirtual: false, Controls: controls, Complexity: complexity}, nil
	default:
		return RouteDecision{}, fmt.Errorf("unknown model or route %q", lookup)
	}
}

func normalizeUnknownModelPolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "passthrough_openrouter", "openrouter":
		return "passthrough_openrouter"
	case "passthrough_openai", "openai", "passthrough":
		return "passthrough_openai"
	default:
		return "reject"
	}
}

func (s *Server) resolveRouteByModelID(modelID string) (RouteConfig, string, bool) {
	if configured, ok := s.cfg.Routes[modelID]; ok {
		return configured, modelID, true
	}
	bestName := ""
	bestLen := -1
	var best RouteConfig
	names := make([]string, 0, len(s.cfg.Routes))
	for name := range s.cfg.Routes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		route := s.cfg.Routes[name]
		prefixes := append([]string{}, route.MatchPrefixes...)
		if strings.HasSuffix(name, "*") {
			prefixes = append(prefixes, strings.TrimSuffix(name, "*"))
		}
		for _, prefix := range prefixes {
			prefix = strings.TrimSpace(prefix)
			if prefix == "" {
				continue
			}
			if strings.HasPrefix(modelID, prefix) && (len(prefix) > bestLen || (len(prefix) == bestLen && (bestName == "" || name < bestName))) {
				bestLen = len(prefix)
				bestName = name
				best = route
			}
		}
	}
	if bestName != "" {
		return best, bestName, true
	}
	return RouteConfig{}, "", false
}

func (s *Server) materializeRouteDecision(model, routeName string, configured RouteConfig, body map[string]any, kind APIKind, r *http.Request, controls XRouterControls, complexity float64) (RouteDecision, error) {
	route := applyControlsToRoute(configured, controls)
	route.Type = normalizeRouteKind(route.Type)
	route.Kind = route.Type
	decision := RouteDecision{PublicModel: model, RouteName: routeName, Route: route, IsVirtual: true, Controls: controls, Complexity: complexity}
	switch route.Type {
	case "direct":
		if route.Target == "" {
			if len(route.Candidates) > 0 {
				route.Target = route.Candidates[0]
				decision.Route = route
			} else {
				return decision, fmt.Errorf("direct route %q missing target", routeName)
			}
		}
		decision.TargetNames = append([]string{route.Target}, route.Fallbacks...)
	case "auto":
		if s.shouldUseMoA(route, controls, complexity) {
			moaRoute, rn, err := s.moaRouteForAuto(routeName, route, controls)
			if err != nil {
				return decision, err
			}
			if moaRoute.Type == "moa" || moaRoute.Type == "mov" {
				decision.Route = moaRoute
				decision.RouteName = rn
				if moaRoute.Type == "mov" {
					decision.TargetNames = movPrimaryTargets(moaRoute)
				} else {
					decision.TargetNames = uniqueStrings(append([]string{moaRoute.Aggregator}, moaRoute.Fallbacks...))
				}
				return decision, nil
			}
		}
		sid := sessionIDWithControls(r, body, controls)
		decision.TargetNames = s.routeCandidates(route, body, kind, sid, r, controls)
	case "moa":
		if route.Aggregator == "" || len(route.References) == 0 {
			return decision, fmt.Errorf("moa route %q requires aggregator and references", routeName)
		}
		decision.TargetNames = append([]string{route.Aggregator}, route.Fallbacks...)
	case "mov":
		if route.Flow == "" {
			route.Flow = "parallel_synthesize_v1"
		}
		decision.Route = route
		decision.TargetNames = movPrimaryTargets(route)
	case "passthrough":
		decision.Route = RouteConfig{Type: "direct", Kind: "direct", Target: model, MultiModel: "never", StickyTTLSeconds: 300}
		decision.TargetNames = []string{model}
	default:
		return decision, fmt.Errorf("unsupported route type %q", route.Type)
	}
	decision.TargetNames = uniqueStrings(decision.TargetNames)
	if len(decision.TargetNames) == 0 && decision.Route.Type != "moa" && decision.Route.Type != "mov" {
		return decision, fmt.Errorf("route %q has no usable candidates", routeName)
	}
	return decision, nil
}

func lookupOrModel(lookup, model string) string {
	if strings.TrimSpace(lookup) != "" {
		return strings.TrimSpace(lookup)
	}
	return model
}

func (s *Server) shouldUseMoA(route RouteConfig, controls XRouterControls, complexity float64) bool {
	if controls.Mode == "moa" || controls.Mode == "mov" {
		return true
	}
	policy := route.MultiModel
	if controls.MultiModel != "" {
		policy = controls.MultiModel
	}
	policy = normalizeMultiModel(policy)
	switch policy {
	case "always":
		return true
	case "auto":
		threshold := route.MoAComplexityThreshold
		if threshold <= 0 {
			threshold = 0.62
		}
		return complexity >= threshold
	default:
		return false
	}
}

func (s *Server) moaRouteForAuto(autoRouteName string, route RouteConfig, controls XRouterControls) (RouteConfig, string, error) {
	if route.MoARoute != "" {
		moa, ok := s.cfg.Routes[route.MoARoute]
		if !ok {
			return RouteConfig{}, autoRouteName, fmt.Errorf("auto route %q references missing moa_route %q", autoRouteName, route.MoARoute)
		}
		moa = applyControlsToRoute(moa, controls)
		moa.Type = normalizeRouteKind(moa.Type)
		// Preserve operational listeners from the auto route unless the MoA route overrides them.
		if len(moa.ShadowTargets) == 0 {
			moa.ShadowTargets = route.ShadowTargets
		}
		if len(moa.SerialListeners) == 0 {
			moa.SerialListeners = route.SerialListeners
		}
		if moa.Aggregator == "" || len(moa.References) == 0 {
			return RouteConfig{}, route.MoARoute, fmt.Errorf("moa_route %q requires aggregator and references", route.MoARoute)
		}
		return moa, route.MoARoute, nil
	}
	moa := route
	moa.Type = "moa"
	moa = applyControlsToRoute(moa, controls)
	if moa.Aggregator == "" || len(moa.References) == 0 {
		return RouteConfig{Type: "auto"}, autoRouteName, nil
	}
	return moa, autoRouteName + "#moa", nil
}

func (s *Server) handleDirectLike(w http.ResponseWriter, r *http.Request, body map[string]any, decision RouteDecision, kind APIKind) {
	stream := getBool(body, "stream")
	sid := sessionIDWithControls(r, body, decision.Controls)
	if stream {
		s.handleStreamWithFallback(w, r, body, decision, kind, sid)
		return
	}
	result := s.callWithFallback(r.Context(), body, decision, kind, r)
	if result.Err != nil && result.Status == 0 {
		writeError(w, http.StatusBadGateway, "upstream_error", result.Err.Error())
		return
	}
	if result.Status >= 200 && result.Status < 300 {
		s.launchShadowCalls(r, body, decision, kind, result.TargetName)
		result = s.runSerialListeners(r.Context(), r, body, decision, kind, result)
		if sid != "" && strings.HasPrefix(decision.Route.Type, "auto") {
			s.sticky.Set(sid, result.TargetName, time.Duration(decision.Route.StickyTTLSeconds)*time.Second)
		}
	}
	s.writeUpstreamResult(w, decision, result)
}

func (s *Server) handleStreamWithFallback(w http.ResponseWriter, r *http.Request, body map[string]any, decision RouteDecision, kind APIKind, sid string) {
	if len(decision.TargetNames) > 0 {
		s.launchShadowCalls(r, body, decision, kind, decision.TargetNames[0])
	}
	var last UpstreamResult
	for _, targetName := range decision.TargetNames {
		res := s.streamTarget(r.Context(), w, targetName, kind, body, r)
		s.metrics.Record(decision.RouteName, targetName, res.Status, res.Duration)
		last = res
		if res.Wrote {
			if res.Err == nil && res.Status >= 200 && res.Status < 300 && sid != "" && decision.Route.Type == "auto" {
				s.sticky.Set(sid, targetName, time.Duration(decision.Route.StickyTTLSeconds)*time.Second)
			}
			return
		}
		if res.Err == nil && res.Status >= 200 && res.Status < 300 {
			if sid != "" && decision.Route.Type == "auto" {
				s.sticky.Set(sid, targetName, time.Duration(decision.Route.StickyTTLSeconds)*time.Second)
			}
			return
		}
		if res.Status > 0 && !retryableStatus(res.Status) {
			break
		}
	}
	if last.Body != nil && last.Status > 0 {
		s.writeUpstreamResult(w, decision, last)
		return
	}
	if last.Err != nil {
		writeError(w, http.StatusBadGateway, "upstream_error", last.Err.Error())
		return
	}
	writeError(w, http.StatusBadGateway, "upstream_error", "all upstreams failed")
}

func (s *Server) callWithFallback(ctx context.Context, body map[string]any, decision RouteDecision, kind APIKind, incoming *http.Request) UpstreamResult {
	var last UpstreamResult
	for _, targetName := range decision.TargetNames {
		res := s.callTargetBytes(ctx, targetName, kind, body, incoming)
		s.metrics.Record(decision.RouteName, targetName, res.Status, res.Duration)
		last = res
		if res.Err == nil && res.Status >= 200 && res.Status < 300 {
			s.updatePrefixCache(body, decision, kind, res)
			return res
		}
		if res.Status > 0 && !retryableStatus(res.Status) {
			return res
		}
	}
	return last
}

func (s *Server) writeUpstreamResult(w http.ResponseWriter, decision RouteDecision, result UpstreamResult) {
	for k, v := range result.Headers {
		if isHopByHopHeader(k) {
			continue
		}
		w.Header().Set(k, v)
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.Header().Set("x-xrouter-route", decision.RouteName)
	w.Header().Set("x-xrouter-target", result.TargetName)
	w.Header().Set("x-xrouter-upstream-model", result.Target.Model)
	w.Header().Set("x-xrouter-execution-type", decision.Route.Type)
	w.Header().Set("x-xrouter-complexity", fmt.Sprintf("%.3f", decision.Complexity))
	w.WriteHeader(result.Status)
	body := result.Body
	if decision.Controls.Explain && result.Status >= 200 && result.Status < 300 {
		body = attachXRouterMetadata(body, map[string]any{
			"route":          decision.RouteName,
			"target":         result.TargetName,
			"upstream_model": result.Target.Model,
			"execution_type": decision.Route.Type,
			"complexity":     decision.Complexity,
			"targets":        decision.TargetNames,
		})
	}
	_, _ = w.Write(body)
}

func addXRouterBodyMetadata(raw []byte, route, target, upstreamModel string) []byte {
	return attachXRouterMetadata(raw, map[string]any{"route": route, "target": target, "upstream_model": upstreamModel})
}

func (s *Server) updatePrefixCache(body map[string]any, decision RouteDecision, kind APIKind, res UpstreamResult) {
	if res.TargetName == "" || s.prefixBK == nil || !s.prefixBK.Enabled(decision.Route, decision.Controls) {
		return
	}
	if !boolPtrValue(s.cfg.PrefixCache.UpdateFromUsage, true) {
		return
	}
	key, ok := s.prefixBK.PrefixKey(body, kind, decision.Route, decision.Controls)
	if !ok {
		return
	}
	s.prefixBK.Touch(key, res.TargetName, res.Target, extractCachedTokens(res.Body))
}

func (s *Server) writeDryRun(w http.ResponseWriter, decision RouteDecision) {
	writeJSON(w, http.StatusOK, map[string]any{
		"object":     "xrouter.route_decision",
		"model":      decision.PublicModel,
		"route":      decision.RouteName,
		"type":       decision.Route.Type,
		"flow":       decision.Route.Flow,
		"complexity": decision.Complexity,
		"targets":    decision.TargetNames,
		"is_virtual": decision.IsVirtual,
	})
}

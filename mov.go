package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func movPrimaryTargets(route RouteConfig) []string {
	var out []string
	out = append(out, route.Target)
	out = append(out, route.Candidates...)
	out = append(out, route.References...)
	out = append(out, route.Aggregator)
	out = append(out, route.Fallbacks...)
	return uniqueStrings(out)
}

func (s *Server) handleMoVChat(w http.ResponseWriter, r *http.Request, body map[string]any, decision RouteDecision) {
	if getBool(body, "stream") {
		writeError(w, http.StatusBadRequest, "unsupported_streaming", "MoV routes require stream=false in this implementation")
		return
	}
	res, err := s.executeMoVChat(r.Context(), r, body, decision)
	if err != nil {
		writeError(w, http.StatusBadGateway, "mov_error", err.Error())
		return
	}
	if res.Err != nil && res.Status == 0 {
		writeError(w, http.StatusBadGateway, "upstream_error", res.Err.Error())
		return
	}
	if res.Status >= 200 && res.Status < 300 {
		res.Body = addXRouterBodyMetadata(res.Body, decision.RouteName, res.TargetName, res.Target.Model)
		s.launchShadowCalls(r, body, decision, APIChat, res.TargetName)
		res = s.runSerialListeners(r.Context(), r, body, decision, APIChat, res)
	}
	s.writeUpstreamResult(w, decision, res)
}

func (s *Server) handleMoVResponses(w http.ResponseWriter, r *http.Request, body map[string]any, decision RouteDecision) {
	if getBool(body, "stream") {
		writeError(w, http.StatusBadRequest, "unsupported_streaming", "MoV Responses shim requires stream=false")
		return
	}
	if body["previous_response_id"] != nil || body["conversation"] != nil {
		writeError(w, http.StatusBadRequest, "unsupported_state", "MoV Responses shim cannot handle previous_response_id or conversation")
		return
	}
	chatBody, err := responsesToChatBody(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "responses_shim_error", err.Error())
		return
	}
	chatBody["model"] = body["model"]
	chatRes, err := s.executeMoVChat(r.Context(), r, chatBody, decision)
	if err != nil {
		writeError(w, http.StatusBadGateway, "mov_error", err.Error())
		return
	}
	if chatRes.Err != nil && chatRes.Status == 0 {
		writeError(w, http.StatusBadGateway, "upstream_error", chatRes.Err.Error())
		return
	}
	if chatRes.Status < 200 || chatRes.Status >= 300 {
		s.writeUpstreamResult(w, decision, chatRes)
		return
	}
	wrapped, err := chatCompletionToResponse(chatRes.Body, body, chatRes.Target.Model, decision.RouteName, chatRes.TargetName)
	if err != nil {
		writeError(w, http.StatusBadGateway, "responses_wrap_error", err.Error())
		return
	}
	wrappedResult := UpstreamResult{TargetName: chatRes.TargetName, Target: chatRes.Target, Status: http.StatusOK, Headers: map[string]string{"Content-Type": "application/json"}, Body: wrapped, Duration: chatRes.Duration}
	s.launchShadowCalls(r, body, decision, APIResponses, chatRes.TargetName)
	wrappedResult = s.runSerialListeners(r.Context(), r, body, decision, APIResponses, wrappedResult)
	s.writeUpstreamResult(w, decision, wrappedResult)
}

func (s *Server) executeMoVChat(ctx context.Context, r *http.Request, body map[string]any, decision RouteDecision) (UpstreamResult, error) {
	flow := strings.ToLower(strings.TrimSpace(decision.Route.Flow))
	if flow == "" {
		flow = "parallel_synthesize_v1"
	}
	switch flow {
	case "parallel_synthesize_v1", "moa_parallel_synthesize_v1":
		return s.movParallelSynthesize(ctx, r, body, decision)
	case "parallel_race_max_output_v1", "race_max_output_v1", "max_output_race_v1":
		return s.movParallelRaceMaxOutput(ctx, r, body, decision)
	case "boundary_guard_race_v1", "parallel_boundary_guard_race_v1", "degradation_guard_race_v1":
		return s.movBoundaryGuardRace(ctx, r, body, decision)
	case "effort_ladder_race_v1", "reasoning_effort_ladder_race_v1":
		return s.movEffortLadderRace(ctx, r, body, decision)
	case "serial_boundary_escalate_v1", "boundary_guard_escalate_v1":
		return s.movSerialBoundaryEscalate(ctx, r, body, decision)
	case "parallel_judge_select_v1":
		return s.movParallelJudgeSelect(ctx, r, body, decision)
	case "best_of_n_self_consistency_v1":
		return s.movBestOfN(ctx, r, body, decision)
	case "propose_critique_revise_v1":
		return s.movProposeCritiqueRevise(ctx, r, body, decision)
	case "serial_chain_relay_v1":
		return s.movSerialChainRelay(ctx, r, body, decision)
	case "map_reduce_specialists_v1":
		return s.movParallelSynthesize(ctx, r, body, decision)
	case "verify_then_escalate_v1":
		return s.movVerifyThenEscalate(ctx, r, body, decision)
	case "cascade_budget_v1":
		return s.movCascadeBudget(ctx, r, body, decision)
	case "dual_path_tool_acting_v1":
		return s.movDualPathToolActing(ctx, r, body, decision)
	case "shadow_evaluation_v1":
		return s.movShadowEvaluation(ctx, r, body, decision)
	default:
		return UpstreamResult{}, fmt.Errorf("unsupported mov flow %q", flow)
	}
}

func isSupportedMoVFlow(flow string) bool {
	switch strings.ToLower(strings.TrimSpace(flow)) {
	case "",
		"parallel_synthesize_v1", "moa_parallel_synthesize_v1",
		"parallel_race_max_output_v1", "race_max_output_v1", "max_output_race_v1",
		"boundary_guard_race_v1", "parallel_boundary_guard_race_v1", "degradation_guard_race_v1",
		"effort_ladder_race_v1", "reasoning_effort_ladder_race_v1",
		"serial_boundary_escalate_v1", "boundary_guard_escalate_v1",
		"parallel_judge_select_v1",
		"best_of_n_self_consistency_v1",
		"propose_critique_revise_v1",
		"serial_chain_relay_v1",
		"map_reduce_specialists_v1",
		"verify_then_escalate_v1",
		"cascade_budget_v1",
		"dual_path_tool_acting_v1",
		"shadow_evaluation_v1":
		return true
	default:
		return false
	}
}

func (s *Server) movParallelSynthesize(ctx context.Context, r *http.Request, body map[string]any, decision RouteDecision) (UpstreamResult, error) {
	route := decision.Route
	if len(route.References) == 0 {
		route.References = route.Candidates
	}
	if route.Aggregator == "" {
		if len(route.Fallbacks) > 0 {
			route.Aggregator = route.Fallbacks[0]
		}
		if route.Aggregator == "" && len(route.Candidates) > 0 {
			route.Aggregator = route.Candidates[0]
		}
	}
	if route.Aggregator == "" || len(route.References) == 0 {
		return UpstreamResult{}, fmt.Errorf("parallel_synthesize requires aggregator and references/candidates")
	}
	moaDecision := decision
	moaDecision.Route = route
	moaDecision.Route.Type = "moa"
	return s.executeMoAChat(ctx, r, body, moaDecision)
}

func (s *Server) movParallelJudgeSelect(ctx context.Context, r *http.Request, body map[string]any, decision RouteDecision) (UpstreamResult, error) {
	route := decision.Route
	candidates := uniqueStrings(append(route.Candidates, route.References...))
	if len(candidates) == 0 {
		return UpstreamResult{}, fmt.Errorf("parallel_judge_select requires candidates or references")
	}
	route.References = candidates
	refs := s.collectReferences(ctx, r, body, route, decision.RouteName+"/judge-select")
	selected := firstUsableReference(refs)
	if route.Judge.Target != "" || route.Aggregator != "" {
		judgeTarget := firstNonEmpty(route.Judge.Target, route.Aggregator)
		if pick := s.askJudgeToSelect(ctx, r, body, judgeTarget, refs); pick != "" {
			for _, ref := range refs {
				if ref.Target == pick && ref.Err == "" {
					selected = ref
					break
				}
			}
		}
	}
	if selected.Target == "" {
		return UpstreamResult{}, fmt.Errorf("all candidate calls failed")
	}
	return syntheticChatResult(selected.Target, selected.Model, selected.Text, decision.RouteName), nil
}

func (s *Server) movBestOfN(ctx context.Context, r *http.Request, body map[string]any, decision RouteDecision) (UpstreamResult, error) {
	route := decision.Route
	if len(route.References) == 0 {
		target := firstNonEmpty(route.Target, route.Aggregator)
		if target == "" && len(route.Candidates) > 0 {
			target = route.Candidates[0]
		}
		if target == "" {
			return UpstreamResult{}, fmt.Errorf("best_of_n requires target/candidate")
		}
		n := route.Parallelism
		if n <= 0 {
			n = 3
		}
		for i := 0; i < n; i++ {
			route.References = append(route.References, target)
		}
	}
	if route.Aggregator == "" {
		route.Aggregator = route.References[0]
	}
	moaDecision := decision
	moaDecision.Route = route
	moaDecision.Route.Type = "moa"
	return s.executeMoAChat(ctx, r, body, moaDecision)
}

func (s *Server) movProposeCritiqueRevise(ctx context.Context, r *http.Request, body map[string]any, decision RouteDecision) (UpstreamResult, error) {
	route := decision.Route
	draftTarget := firstAvailable(route.Target, route.Candidates, route.References, []string{route.Aggregator})
	criticTarget := nthAvailable(1, route.Candidates, route.References)
	reviserTarget := firstNonEmpty(route.Aggregator, nthAvailable(2, route.Candidates, route.References), draftTarget)
	if draftTarget == "" || criticTarget == "" || reviserTarget == "" {
		return UpstreamResult{}, fmt.Errorf("propose_critique_revise requires draft, critic, reviser targets")
	}
	draft := s.callTargetBytes(ctx, draftTarget, APIChat, withSystem(body, "Produce a clear first draft."), r)
	if draft.Err != nil || draft.Status < 200 || draft.Status >= 300 {
		return draft, nil
	}
	draftText := textFromResult(draft)
	critiqueBody := withSystem(body, "Critique this draft for correctness, omissions, and ambiguity.\n\nDRAFT:\n"+draftText)
	critique := s.callTargetBytes(ctx, criticTarget, APIChat, critiqueBody, r)
	critiqueText := textFromResult(critique)
	finalBody := withSystem(body, "Revise the answer using the draft and critique. Return only the final answer.\n\nDRAFT:\n"+draftText+"\n\nCRITIQUE:\n"+critiqueText)
	finalDecision := decision
	finalDecision.TargetNames = []string{reviserTarget}
	return s.callWithFallback(ctx, finalBody, finalDecision, APIChat, r), nil
}

func (s *Server) movSerialChainRelay(ctx context.Context, r *http.Request, body map[string]any, decision RouteDecision) (UpstreamResult, error) {
	chain := uniqueStrings(append(decision.Route.Candidates, decision.Route.References...))
	if decision.Route.Target != "" {
		chain = append([]string{decision.Route.Target}, chain...)
	}
	if decision.Route.Aggregator != "" {
		chain = append(chain, decision.Route.Aggregator)
	}
	chain = uniqueStrings(chain)
	if len(chain) == 0 {
		return UpstreamResult{}, fmt.Errorf("serial_chain_relay requires targets")
	}
	currentBody := cloneJSONMap(body)
	var last UpstreamResult
	for i, target := range chain {
		if i > 0 {
			currentBody = withSystem(body, fmt.Sprintf("Continue from the previous model output. Improve it and return the best current answer.\n\nPREVIOUS OUTPUT:\n%s", textFromResult(last)))
		}
		last = s.callTargetBytes(ctx, target, APIChat, currentBody, r)
		s.metrics.Record(decision.RouteName+"/serial", target, last.Status, last.Duration)
		if last.Err != nil || last.Status < 200 || last.Status >= 300 {
			return last, nil
		}
	}
	return last, nil
}

func (s *Server) movVerifyThenEscalate(ctx context.Context, r *http.Request, body map[string]any, decision RouteDecision) (UpstreamResult, error) {
	route := decision.Route
	primary := firstAvailable(route.Target, route.Candidates, route.References)
	if primary == "" {
		return UpstreamResult{}, fmt.Errorf("verify_then_escalate requires primary target")
	}
	primaryRes := s.callTargetBytes(ctx, primary, APIChat, body, r)
	if primaryRes.Err != nil || primaryRes.Status < 200 || primaryRes.Status >= 300 {
		return primaryRes, nil
	}
	verifier := firstNonEmpty(route.Judge.Target, nthAvailable(1, route.Candidates, route.References))
	if verifier == "" {
		return primaryRes, nil
	}
	verifyBody := withSystem(body, "Verify the following answer. Return JSON only: {\"pass\":true|false,\"reason\":\"...\"}.\n\nANSWER:\n"+textFromResult(primaryRes))
	verifyBody["response_format"] = map[string]any{"type": "json_object"}
	verifyBody["temperature"] = 0
	verifyRes := s.callTargetBytes(ctx, verifier, APIChat, verifyBody, r)
	if verifyRes.Err == nil && verifyRes.Status >= 200 && verifyRes.Status < 300 && verificationPassed(textFromResult(verifyRes)) {
		return primaryRes, nil
	}
	escalation := firstNonEmpty(route.Aggregator, nthAvailable(2, route.Candidates, route.References), primary)
	finalBody := withSystem(body, "The initial answer did not pass verification or verification failed. Produce a stronger final answer.\n\nINITIAL ANSWER:\n"+textFromResult(primaryRes)+"\n\nVERIFIER:\n"+textFromResult(verifyRes))
	finalDecision := decision
	finalDecision.TargetNames = []string{escalation}
	return s.callWithFallback(ctx, finalBody, finalDecision, APIChat, r), nil
}

func (s *Server) movCascadeBudget(ctx context.Context, r *http.Request, body map[string]any, decision RouteDecision) (UpstreamResult, error) {
	chain := uniqueStrings(append(decision.Route.Candidates, decision.Route.Fallbacks...))
	if decision.Route.Target != "" {
		chain = append([]string{decision.Route.Target}, chain...)
	}
	if len(chain) == 0 {
		return UpstreamResult{}, fmt.Errorf("cascade_budget requires target/candidates")
	}
	var last UpstreamResult
	for _, target := range chain {
		res := s.callTargetBytes(ctx, target, APIChat, body, r)
		s.metrics.Record(decision.RouteName+"/cascade", target, res.Status, res.Duration)
		last = res
		if cascadeResultAcceptable(res, body, decision.Route.Race) {
			return res, nil
		}
	}
	return last, nil
}

func (s *Server) movDualPathToolActing(ctx context.Context, r *http.Request, body map[string]any, decision RouteDecision) (UpstreamResult, error) {
	actor := firstAvailable(decision.Route.Target, decision.Route.Candidates, []string{decision.Route.Aggregator})
	if actor == "" {
		return UpstreamResult{}, fmt.Errorf("dual_path_tool_acting requires actor target")
	}
	d := decision
	d.TargetNames = []string{actor}
	res := s.callWithFallback(ctx, body, d, APIChat, r)
	return res, nil
}

func (s *Server) movShadowEvaluation(ctx context.Context, r *http.Request, body map[string]any, decision RouteDecision) (UpstreamResult, error) {
	primary := firstAvailable(decision.Route.Target, decision.Route.Candidates, []string{decision.Route.Aggregator})
	if primary == "" {
		return UpstreamResult{}, fmt.Errorf("shadow_evaluation requires primary target")
	}
	d := decision
	d.TargetNames = []string{primary}
	res := s.callWithFallback(ctx, body, d, APIChat, r)
	if res.Err == nil && res.Status >= 200 && res.Status < 300 {
		s.launchShadowCalls(r, body, decision, APIChat, res.TargetName)
	}
	return res, nil
}

func (s *Server) askJudgeToSelect(ctx context.Context, r *http.Request, body map[string]any, judgeTarget string, refs []referenceOutput) string {
	if judgeTarget == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("Select the best candidate. Return JSON only: {\"target\":\"candidate-target\",\"reason\":\"...\"}.\n")
	if original := strings.TrimSpace(requestText(body, APIChat)); original != "" {
		b.WriteString("\nOriginal user request:\n")
		b.WriteString(truncateRunes(original, 2000))
		b.WriteString("\n")
	}
	for _, ref := range refs {
		b.WriteString("\n[target=")
		b.WriteString(ref.Target)
		b.WriteString("]\n")
		if ref.Err != "" {
			b.WriteString("ERROR: " + ref.Err + "\n")
		} else {
			b.WriteString(ref.Text + "\n")
		}
	}
	judgeBody := map[string]any{"model": judgeTarget, "stream": false, "temperature": 0, "response_format": map[string]any{"type": "json_object"}, "messages": []any{map[string]any{"role": "user", "content": b.String()}}}
	res := s.callTargetBytes(ctx, judgeTarget, APIChat, judgeBody, r)
	if res.Err != nil || res.Status < 200 || res.Status >= 300 {
		return ""
	}
	var obj map[string]any
	if err := json.Unmarshal(res.Body, &obj); err != nil {
		return ""
	}
	text := extractChatText(obj)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(extractJSONObject(text)), &parsed); err != nil {
		return ""
	}
	return stringFromAny(parsed["target"])
}

func firstUsableReference(refs []referenceOutput) referenceOutput {
	for _, ref := range refs {
		if ref.Err == "" && strings.TrimSpace(ref.Text) != "" {
			return ref
		}
	}
	return referenceOutput{}
}

func syntheticChatResult(targetName, model, text, route string) UpstreamResult {
	if model == "" {
		model = targetName
	}
	body, _ := json.Marshal(map[string]any{"id": randomID("chatcmpl_xrouter"), "object": "chat.completion", "created": 0, "model": model, "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": text}, "finish_reason": "stop"}}, "xrouter": map[string]any{"route": route, "target": targetName, "synthetic": true}})
	return UpstreamResult{TargetName: targetName, Target: TargetConfig{Model: model}, Status: http.StatusOK, Headers: map[string]string{"Content-Type": "application/json"}, Body: body}
}

func withSystem(body map[string]any, prompt string) map[string]any {
	out := cloneTopLevelJSONMap(body)
	out["stream"] = false
	msgs, _ := out["messages"].([]any)
	out["messages"] = append([]any{map[string]any{"role": "system", "content": prompt}}, msgs...)
	return out
}

func cascadeResultAcceptable(res UpstreamResult, body map[string]any, cfg RaceConfig) bool {
	if !raceSucceeded(res) {
		return false
	}
	metrics := metricsFromResult(res, cfg)
	if raceLooksDegraded(metrics, cfg) {
		return false
	}
	if requestNeedsJSON(body) && !resultContainsJSONContent(res) {
		return false
	}
	return true
}

func resultContainsJSONContent(res UpstreamResult) bool {
	text := strings.TrimSpace(textFromResult(res))
	if text == "" {
		text = strings.TrimSpace(string(res.Body))
	}
	return containsValidJSONObject(text)
}

func containsValidJSONObject(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if json.Valid([]byte(text)) {
		return true
	}
	start := strings.IndexByte(text, '{')
	end := strings.LastIndexByte(text, '}')
	if start < 0 || end < start {
		return false
	}
	return json.Valid([]byte(text[start : end+1]))
}

func textFromResult(res UpstreamResult) string {
	var obj map[string]any
	if err := json.Unmarshal(res.Body, &obj); err != nil {
		return strings.TrimSpace(string(res.Body))
	}
	return extractChatText(obj)
}

func verificationPassed(text string) bool {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(extractJSONObject(text)), &parsed); err != nil {
		return false
	}
	for _, key := range []string{"pass", "passed", "ok"} {
		switch v := parsed[key].(type) {
		case bool:
			if v {
				return true
			}
		case string:
			if isTrueishVerificationValue(v) {
				return true
			}
		}
	}
	return false
}

func isTrueishVerificationValue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "pass", "passed", "ok", "yes":
		return true
	default:
		return false
	}
}

func firstAvailable(first string, lists ...[]string) string {
	if strings.TrimSpace(first) != "" {
		return strings.TrimSpace(first)
	}
	for _, xs := range lists {
		for _, x := range xs {
			if strings.TrimSpace(x) != "" {
				return strings.TrimSpace(x)
			}
		}
	}
	return ""
}

func nthAvailable(n int, lists ...[]string) string {
	var flat []string
	for _, xs := range lists {
		flat = append(flat, xs...)
	}
	flat = uniqueStrings(flat)
	if n >= 0 && n < len(flat) {
		return flat[n]
	}
	return ""
}

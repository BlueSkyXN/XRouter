package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleResponsesDirectLike(w http.ResponseWriter, r *http.Request, body map[string]any, decision RouteDecision) {
	stream := getBool(body, "stream")
	var nativeTargets []string
	var shimTargets []string
	for _, name := range decision.TargetNames {
		t, ok := s.targetByName(name)
		if !ok {
			continue
		}
		p, ok := s.providerForTarget(t)
		if ok && p.supports(APIResponses) && t.Capabilities.Responses {
			nativeTargets = append(nativeTargets, name)
		} else {
			shimTargets = append(shimTargets, name)
		}
	}
	if len(nativeTargets) > 0 {
		nd := decision
		nd.TargetNames = append(nativeTargets, shimTargets...)
		if stream {
			s.handleStreamWithFallback(w, r, body, nd, APIResponses, sessionIDWithControls(r, body, decision.Controls))
			return
		}
		result := s.callWithFallback(r.Context(), body, nd, APIResponses, r)
		if result.Err == nil && result.Status >= 200 && result.Status < 300 {
			s.launchShadowCalls(r, body, decision, APIResponses, result.TargetName)
			result = s.runSerialListeners(r.Context(), r, body, decision, APIResponses, result)
			if sid := sessionIDWithControls(r, body, decision.Controls); sid != "" && decision.Route.Type == "auto" {
				s.sticky.Set(sid, result.TargetName, time.Duration(decision.Route.StickyTTLSeconds)*time.Second)
			}
			s.writeUpstreamResult(w, nd, result)
			return
		}
		if result.Status > 0 && !retryableStatus(result.Status) {
			s.writeUpstreamResult(w, nd, result)
			return
		}
		// Retryable native failure can fall through to shim candidates.
	}
	if stream {
		writeError(w, http.StatusBadRequest, "unsupported_streaming", "Responses shim does not support stream=true; use a native responses target or set stream=false")
		return
	}
	if body["previous_response_id"] != nil || body["conversation"] != nil {
		writeError(w, http.StatusBadRequest, "unsupported_state", "Responses shim cannot handle previous_response_id or conversation; select a native OpenAI Responses target")
		return
	}
	chatBody, err := responsesToChatBody(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "responses_shim_error", err.Error())
		return
	}
	shimDecision := decision
	shimDecision.TargetNames = shimTargets
	if len(shimDecision.TargetNames) == 0 {
		shimDecision.TargetNames = decision.TargetNames
	}
	res := s.callWithFallback(r.Context(), chatBody, shimDecision, APIChat, r)
	if res.Err != nil && res.Status == 0 {
		writeError(w, http.StatusBadGateway, "upstream_error", res.Err.Error())
		return
	}
	if res.Status < 200 || res.Status >= 300 {
		s.writeUpstreamResult(w, shimDecision, res)
		return
	}
	wrapped, err := chatCompletionToResponse(res.Body, body, res.Target.Model, decision.RouteName, res.TargetName)
	if err != nil {
		writeError(w, http.StatusBadGateway, "responses_wrap_error", err.Error())
		return
	}
	wrappedResult := UpstreamResult{
		TargetName: res.TargetName,
		Target:     res.Target,
		Status:     http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       wrapped,
		Duration:   res.Duration,
	}
	s.launchShadowCalls(r, chatBody, shimDecision, APIChat, res.TargetName)
	wrappedResult = s.runSerialListeners(r.Context(), r, body, decision, APIResponses, wrappedResult)
	if sid := sessionIDWithControls(r, body, decision.Controls); sid != "" && decision.Route.Type == "auto" {
		s.sticky.Set(sid, res.TargetName, time.Duration(decision.Route.StickyTTLSeconds)*time.Second)
	}
	s.writeUpstreamResult(w, decision, wrappedResult)
}

func responsesToChatBody(body map[string]any) (map[string]any, error) {
	chat := map[string]any{}
	copyKeys := []string{"model", "temperature", "top_p", "presence_penalty", "frequency_penalty", "tools", "tool_choice", "parallel_tool_calls", "metadata", "stream", "response_format", "seed", "stop"}
	for _, k := range copyKeys {
		if v, ok := body[k]; ok {
			chat[k] = v
		}
	}
	if v, ok := body["max_output_tokens"]; ok {
		chat["max_tokens"] = v
	}
	if text, ok := body["text"].(map[string]any); ok {
		if format, ok := text["format"].(map[string]any); ok {
			chat["response_format"] = format
		}
	}
	messages := make([]any, 0)
	if inst, ok := body["instructions"].(string); ok && strings.TrimSpace(inst) != "" {
		messages = append(messages, map[string]any{"role": "system", "content": inst})
	}
	input, ok := body["input"]
	if !ok {
		return nil, fmt.Errorf("responses request requires input for chat shim")
	}
	msgs, err := responseInputToMessages(input)
	if err != nil {
		return nil, err
	}
	messages = append(messages, msgs...)
	chat["messages"] = messages
	return chat, nil
}

func responseInputToMessages(input any) ([]any, error) {
	switch v := input.(type) {
	case string:
		return []any{map[string]any{"role": "user", "content": v}}, nil
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				out = append(out, map[string]any{"role": "user", "content": contentToText(item)})
				continue
			}
			role, _ := m["role"].(string)
			if role == "" {
				role = "user"
			}
			if typ, _ := m["type"].(string); typ != "" && typ != "message" {
				out = append(out, map[string]any{"role": role, "content": contentToText(m)})
				continue
			}
			content := normalizeResponsesContent(m["content"])
			out = append(out, map[string]any{"role": role, "content": content})
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("responses input array is empty")
		}
		return out, nil
	default:
		return []any{map[string]any{"role": "user", "content": contentToText(input)}}, nil
	}
}

func normalizeResponsesContent(v any) any {
	switch parts := v.(type) {
	case string:
		return parts
	case []any:
		out := make([]any, 0, len(parts))
		for _, part := range parts {
			pm, ok := part.(map[string]any)
			if !ok {
				out = append(out, map[string]any{"type": "text", "text": contentToText(part)})
				continue
			}
			typ, _ := pm["type"].(string)
			switch typ {
			case "input_text", "output_text", "text":
				text, _ := pm["text"].(string)
				out = append(out, map[string]any{"type": "text", "text": text})
			case "input_image", "image_url":
				if u, ok := pm["image_url"].(string); ok {
					out = append(out, map[string]any{"type": "image_url", "image_url": map[string]any{"url": u}})
				} else {
					out = append(out, pm)
				}
			default:
				out = append(out, map[string]any{"type": "text", "text": compactJSON(pm)})
			}
		}
		if len(out) == 1 {
			if m, ok := out[0].(map[string]any); ok {
				if m["type"] == "text" {
					return m["text"]
				}
			}
		}
		return out
	default:
		return contentToText(v)
	}
}

func chatCompletionToResponse(raw []byte, original map[string]any, upstreamModel, route, target string) ([]byte, error) {
	var chat map[string]any
	if err := json.Unmarshal(raw, &chat); err != nil {
		return nil, err
	}
	text := extractChatText(chat)
	created := time.Now().Unix()
	if c, ok := chat["created"].(json.Number); ok {
		if i, err := c.Int64(); err == nil {
			created = i
		}
	} else if f, ok := chat["created"].(float64); ok {
		created = int64(f)
	}
	model, _ := chat["model"].(string)
	if model == "" {
		model = upstreamModel
	}
	usage := map[string]any{}
	if u, ok := chat["usage"].(map[string]any); ok {
		usage = map[string]any{
			"input_tokens":  u["prompt_tokens"],
			"output_tokens": u["completion_tokens"],
			"total_tokens":  u["total_tokens"],
		}
	}
	toolCalls := extractChatToolCalls(chat)
	output := make([]any, 0, 1+len(toolCalls))
	if text != "" || len(toolCalls) == 0 {
		output = append(output, map[string]any{
			"type":   "message",
			"id":     randomID("msg_xrouter"),
			"status": "completed",
			"role":   "assistant",
			"content": []any{
				map[string]any{"type": "output_text", "text": text, "annotations": []any{}},
			},
		})
	}
	output = append(output, toolCalls...)
	resp := map[string]any{
		"id":                  randomID("resp_xrouter"),
		"object":              "response",
		"created_at":          created,
		"status":              "completed",
		"model":               model,
		"output_text":         text,
		"parallel_tool_calls": true,
		"store":               false,
		"output":              output,
		"usage":               usage,
		"xrouter": map[string]any{
			"mode":           "responses_shim",
			"route":          route,
			"target":         target,
			"upstream_model": model,
		},
	}
	if v, ok := original["metadata"]; ok {
		resp["metadata"] = v
	}
	return json.Marshal(resp)
}

func extractChatText(chat map[string]any) string {
	if msg, ok := firstChatMessage(chat); ok {
		return contentToText(msg["content"])
	}
	if choice, ok := firstChatChoice(chat); ok {
		if text, ok := choice["text"].(string); ok {
			return text
		}
	}
	return ""
}

func firstChatChoice(chat map[string]any) (map[string]any, bool) {
	choices, ok := chat["choices"].([]any)
	if !ok || len(choices) == 0 {
		return nil, false
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return nil, false
	}
	return choice, true
}

func firstChatMessage(chat map[string]any) (map[string]any, bool) {
	choice, ok := firstChatChoice(chat)
	if !ok {
		return nil, false
	}
	if msg, ok := choice["message"].(map[string]any); ok {
		return msg, true
	}
	return nil, false
}

func extractChatToolCalls(chat map[string]any) []any {
	msg, ok := firstChatMessage(chat)
	if !ok {
		return nil
	}
	raw, ok := msg["tool_calls"].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make([]any, 0, len(raw))
	for _, item := range raw {
		call, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if typ := strings.ToLower(strings.TrimSpace(stringFromAny(call["type"]))); typ != "" && typ != "function" {
			out = append(out, map[string]any{
				"type":           "tool_call",
				"id":             firstNonEmpty(stringFromAny(call["id"]), randomID("tc_xrouter")),
				"status":         "completed",
				"chat_tool_call": call,
			})
			continue
		}
		fn, _ := call["function"].(map[string]any)
		callID := firstNonEmpty(stringFromAny(call["id"]), randomID("call_xrouter"))
		out = append(out, map[string]any{
			"type":      "function_call",
			"id":        randomID("fc_xrouter"),
			"call_id":   callID,
			"name":      stringFromAny(fn["name"]),
			"arguments": stringFromAny(fn["arguments"]),
			"status":    "completed",
		})
	}
	return out
}

func callMoAAsResponse(ctx context.Context, s *Server, r *http.Request, body map[string]any, decision RouteDecision) (UpstreamResult, error) {
	chatBody, err := responsesToChatBody(body)
	if err != nil {
		return UpstreamResult{}, err
	}
	chatBody["model"] = body["model"]
	return s.executeMoAChat(ctx, r, chatBody, decision)
}

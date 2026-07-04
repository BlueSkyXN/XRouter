package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	internalReasoningEffortKey = "_xrouter_internal_reasoning_effort"
	internalProviderAPIKeysKey = "_xrouter_internal_provider_api_keys"
)

func (s *Server) targetByName(name string) (TargetConfig, bool) {
	if t, ok := s.configuredTargetByName(name); ok {
		return t, true
	}
	switch normalizeUnknownModelPolicy(s.cfg.Routing.UnknownModelPolicy) {
	case "passthrough_openrouter":
		return s.passthroughTarget(name, "openrouter")
	case "passthrough_openai":
		return s.passthroughTarget(name, "openai")
	default:
		return TargetConfig{}, false
	}
}

func (s *Server) configuredTargetByName(name string) (TargetConfig, bool) {
	if t, ok := s.cfg.Targets[name]; ok {
		return t, true
	}
	return TargetConfig{}, false
}

func (s *Server) passthroughTarget(model, providerName string) (TargetConfig, bool) {
	model = strings.TrimSpace(model)
	providerName = strings.TrimSpace(strings.ToLower(providerName))
	if model == "" || providerName == "" {
		return TargetConfig{}, false
	}
	if _, ok := s.cfg.Providers[providerName]; !ok {
		return TargetConfig{}, false
	}
	caps := CapabilityConfig{Tools: true, Vision: true, JSON: true, Responses: providerName == "openai"}
	return TargetConfig{
		Provider:     providerName,
		Model:        model,
		Quality:      0.5,
		Reliability:  0.95,
		LatencyMS:    1500,
		Capabilities: caps,
	}, true
}

func (s *Server) providerForTarget(target TargetConfig) (ProviderConfig, bool) {
	p, ok := s.cfg.Providers[target.Provider]
	return p, ok
}

func upstreamURL(provider ProviderConfig, kind APIKind) (string, error) {
	base := strings.TrimRight(provider.BaseURL, "/")
	if base == "" {
		return "", fmt.Errorf("provider base_url is empty")
	}
	if _, err := url.Parse(base); err != nil {
		return "", fmt.Errorf("invalid provider base_url %q: %w", base, err)
	}
	switch kind {
	case APIChat:
		return base + "/chat/completions", nil
	case APIResponses:
		return base + "/responses", nil
	default:
		return "", fmt.Errorf("unsupported API kind %q", kind)
	}
}

func (s *Server) callTargetBytes(ctx context.Context, targetName string, kind APIKind, body map[string]any, incoming *http.Request) UpstreamResult {
	start := time.Now()
	target, ok := s.targetByName(targetName)
	if !ok {
		return UpstreamResult{TargetName: targetName, Status: 0, Duration: time.Since(start), Err: fmt.Errorf("unknown target %q", targetName)}
	}
	provider, ok := s.providerForTarget(target)
	if !ok {
		return UpstreamResult{TargetName: targetName, Target: target, Status: 0, Duration: time.Since(start), Err: fmt.Errorf("unknown provider %q", target.Provider)}
	}
	if !provider.supports(kind) {
		return UpstreamResult{TargetName: targetName, Target: target, Status: 0, Duration: time.Since(start), Err: fmt.Errorf("provider %q does not support %s", target.Provider, kind)}
	}
	url, err := upstreamURL(provider, kind)
	if err != nil {
		return UpstreamResult{TargetName: targetName, Target: target, Status: 0, Duration: time.Since(start), Err: err}
	}
	upBody := prepareBodyForTarget(body, target, provider, kind)
	payload, err := json.Marshal(upBody)
	if err != nil {
		return UpstreamResult{TargetName: targetName, Target: target, Status: 0, Duration: time.Since(start), Err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return UpstreamResult{TargetName: targetName, Target: target, Status: 0, Duration: time.Since(start), Err: err}
	}
	applyProviderHeaders(req, provider, target, incoming, body, s.cfg.RequestOverrides.Enabled && s.cfg.RequestOverrides.AllowProviderKeyOverride)
	resp, err := s.client.Do(req)
	if err != nil {
		return UpstreamResult{TargetName: targetName, Target: target, Status: 0, Duration: time.Since(start), Err: err}
	}
	defer resp.Body.Close()
	data, readErr := readLimitedResponseBody(resp.Body, s.cfg.Server.MaxUpstreamBodyBytes)
	if readErr != nil {
		return UpstreamResult{TargetName: targetName, Target: target, Status: 0, Headers: flattenHeaders(resp.Header), Duration: time.Since(start), Err: readErr}
	}
	return UpstreamResult{TargetName: targetName, Target: target, Status: resp.StatusCode, Headers: flattenHeaders(resp.Header), Body: data, Duration: time.Since(start)}
}

func (s *Server) streamTarget(ctx context.Context, w http.ResponseWriter, targetName string, kind APIKind, body map[string]any, incoming *http.Request) UpstreamResult {
	start := time.Now()
	target, ok := s.targetByName(targetName)
	if !ok {
		return UpstreamResult{TargetName: targetName, Status: 0, Duration: time.Since(start), Err: fmt.Errorf("unknown target %q", targetName)}
	}
	provider, ok := s.providerForTarget(target)
	if !ok {
		return UpstreamResult{TargetName: targetName, Target: target, Status: 0, Duration: time.Since(start), Err: fmt.Errorf("unknown provider %q", target.Provider)}
	}
	if !provider.supports(kind) {
		return UpstreamResult{TargetName: targetName, Target: target, Status: 0, Duration: time.Since(start), Err: fmt.Errorf("provider %q does not support %s", target.Provider, kind)}
	}
	url, err := upstreamURL(provider, kind)
	if err != nil {
		return UpstreamResult{TargetName: targetName, Target: target, Status: 0, Duration: time.Since(start), Err: err}
	}
	upBody := prepareBodyForTarget(body, target, provider, kind)
	payload, err := json.Marshal(upBody)
	if err != nil {
		return UpstreamResult{TargetName: targetName, Target: target, Status: 0, Duration: time.Since(start), Err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return UpstreamResult{TargetName: targetName, Target: target, Status: 0, Duration: time.Since(start), Err: err}
	}
	applyProviderHeaders(req, provider, target, incoming, body, s.cfg.RequestOverrides.Enabled && s.cfg.RequestOverrides.AllowProviderKeyOverride)
	resp, err := s.streamClient.Do(req)
	if err != nil {
		return UpstreamResult{TargetName: targetName, Target: target, Status: 0, Duration: time.Since(start), Err: err}
	}
	defer resp.Body.Close()
	if retryableStatus(resp.StatusCode) {
		data, _ := readLimitedResponseBody(resp.Body, s.cfg.Server.MaxUpstreamBodyBytes)
		return UpstreamResult{TargetName: targetName, Target: target, Status: resp.StatusCode, Headers: flattenHeaders(resp.Header), Body: data, Duration: time.Since(start)}
	}
	copyResponseHeaders(w, resp.Header)
	w.Header().Set("x-xrouter-target", targetName)
	w.Header().Set("x-xrouter-upstream-model", target.Model)
	w.WriteHeader(resp.StatusCode)
	writer := io.Writer(w)
	if f, ok := w.(http.Flusher); ok {
		writer = flushWriter{w: w, f: f}
	}
	_, copyErr := io.Copy(writer, resp.Body)
	return UpstreamResult{TargetName: targetName, Target: target, Status: resp.StatusCode, Headers: flattenHeaders(resp.Header), Duration: time.Since(start), Wrote: true, Err: copyErr}
}

func prepareBodyForTarget(body map[string]any, target TargetConfig, provider ProviderConfig, kind APIKind) map[string]any {
	up := cloneTopLevelJSONMap(body)
	translateInternalReasoning := boolFromAny(up[internalReasoningEffortKey])
	delete(up, internalReasoningEffortKey)
	delete(up, internalProviderAPIKeysKey)
	delete(up, "xrouter")
	for k, v := range target.ExtraBody {
		if strings.TrimSpace(k) == "" {
			continue
		}
		if _, exists := up[k]; exists {
			continue
		}
		up[k] = v
	}
	up["model"] = target.Model
	// Avoid leaking OpenRouter-only extensions to non-OpenRouter providers.
	if target.Provider != "openrouter" {
		delete(up, "session_id")
		delete(up, "plugins")
		delete(up, "provider")
		delete(up, "models")
	}
	if kind == APIChat {
		// Chat endpoint does not understand Responses-only fields.
		delete(up, "instructions")
		delete(up, "input")
		delete(up, "previous_response_id")
		delete(up, "max_output_tokens")
	}
	translateBodyForProvider(up, target, translateInternalReasoning)
	return up
}

func translateBodyForProvider(body map[string]any, target TargetConfig, translateInternalReasoning bool) {
	if target.Provider != "openai" || !translateInternalReasoning {
		return
	}
	reasoning, ok := body["reasoning"].(map[string]any)
	if !ok {
		return
	}
	if effort := strings.TrimSpace(stringFromAny(reasoning["effort"])); effort != "" {
		body["reasoning_effort"] = effort
	}
	delete(body, "reasoning")
}

func readLimitedResponseBody(r io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return io.ReadAll(r)
	}
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return data, fmt.Errorf("read upstream response: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("upstream response exceeded max_upstream_body_bytes (%d)", maxBytes)
	}
	return data, nil
}

type flushWriter struct {
	w io.Writer
	f http.Flusher
}

func (fw flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if fw.f != nil {
		fw.f.Flush()
	}
	return n, err
}

func applyProviderHeaders(req *http.Request, provider ProviderConfig, target TargetConfig, incoming *http.Request, body map[string]any, allowKeyOverride bool) {
	req.Header.Set("Content-Type", "application/json")
	key := strings.TrimSpace(provider.APIKey)
	if key == "" && provider.APIKeyEnv != "" {
		key = strings.TrimSpace(os.Getenv(provider.APIKeyEnv))
	}
	if allowKeyOverride {
		if override := providerAPIKeyOverride(incoming, body, target.Provider); override != "" {
			key = override
		}
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	for k, v := range provider.Headers {
		if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			req.Header.Set(k, v)
		}
	}
	if target.Provider == "openrouter" && incoming != nil {
		if s := strings.TrimSpace(incoming.Header.Get("x-session-id")); s != "" {
			req.Header.Set("x-session-id", s)
		}
	}
}

func flattenHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for k, vals := range h {
		if len(vals) > 0 {
			out[k] = vals[0]
		}
	}
	return out
}

func copyResponseHeaders(w http.ResponseWriter, h http.Header) {
	blocked := map[string]struct{}{
		"Content-Length":      {},
		"Connection":          {},
		"Keep-Alive":          {},
		"Proxy-Authenticate":  {},
		"Proxy-Authorization": {},
		"Te":                  {},
		"Trailer":             {},
		"Transfer-Encoding":   {},
		"Upgrade":             {},
	}
	for k, vals := range h {
		if _, skip := blocked[k]; skip || isHopByHopHeader(k) {
			continue
		}
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
}

func providerAPIKeyOverride(incoming *http.Request, body map[string]any, providerName string) string {
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	if incoming != nil {
		for _, h := range []string{
			"x-xrouter-provider-key-" + providerName,
			"x-xrouter-provider-api-key-" + providerName,
			"x-xrouter-provider-key",
		} {
			if s := strings.TrimSpace(incoming.Header.Get(h)); s != "" {
				return s
			}
		}
	}
	x := requestExtension(body)
	if m, ok := x["provider_api_keys"].(map[string]any); ok {
		for k, v := range m {
			if strings.EqualFold(strings.TrimSpace(k), providerName) {
				return stringFromAny(v)
			}
		}
	}
	if m, ok := x["provider_keys"].(map[string]any); ok {
		for k, v := range m {
			if strings.EqualFold(strings.TrimSpace(k), providerName) {
				return stringFromAny(v)
			}
		}
	}
	if m, ok := body[internalProviderAPIKeysKey].(map[string]string); ok {
		for k, v := range m {
			if strings.EqualFold(strings.TrimSpace(k), providerName) {
				return strings.TrimSpace(v)
			}
		}
	}
	if m, ok := body[internalProviderAPIKeysKey].(map[string]any); ok {
		for k, v := range m {
			if strings.EqualFold(strings.TrimSpace(k), providerName) {
				return stringFromAny(v)
			}
		}
	}
	return ""
}

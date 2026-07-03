package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, ResponseError{Error: ErrorBody{Message: msg, Type: "xrouter_error", Code: code}})
}

func readJSONBody(w http.ResponseWriter, r *http.Request, maxBytes int64) (map[string]any, error) {
	defer r.Body.Close()
	if maxBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	}
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	var body map[string]any
	if err := dec.Decode(&body); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("request body must contain a single JSON object")
		}
		return nil, err
	}
	if body == nil {
		body = map[string]any{}
	}
	return body, nil
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		switch s := v.(type) {
		case string:
			return s
		case fmt.Stringer:
			return s.String()
		}
	}
	return ""
}

func getBool(m map[string]any, key string) bool {
	if v, ok := m[key]; ok {
		switch b := v.(type) {
		case bool:
			return b
		case string:
			bb, _ := strconv.ParseBool(b)
			return bb
		}
	}
	return false
}

func randomID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func retryableStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500
}

func bearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return strings.TrimSpace(r.Header.Get("x-api-key"))
}

func sessionIDFromRequest(r *http.Request, body map[string]any) string {
	for _, h := range []string{"x-session-id", "x-xrouter-session-id"} {
		if s := strings.TrimSpace(r.Header.Get(h)); s != "" {
			return s
		}
	}
	if s := getString(body, "session_id"); s != "" {
		return s
	}
	if meta, ok := body["metadata"].(map[string]any); ok {
		for _, k := range []string{"session_id", "x_session_id", "conversation_id"} {
			if s, ok := meta[k].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func requestExtension(body map[string]any) map[string]any {
	if x, ok := body["xrouter"].(map[string]any); ok {
		return x
	}
	return map[string]any{}
}

func objectiveFrom(body map[string]any, route RouteConfig) string {
	if x := requestExtension(body); x != nil {
		if s, ok := x["objective"].(string); ok && strings.TrimSpace(s) != "" {
			return strings.ToLower(strings.TrimSpace(s))
		}
	}
	if route.Objective != "" {
		return strings.ToLower(route.Objective)
	}
	return "balanced"
}

func stringInSlice(v string, xs []string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	return string(rs[:max]) + "\n[truncated]"
}

func isHopByHopHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade", "content-length":
		return true
	default:
		return false
	}
}

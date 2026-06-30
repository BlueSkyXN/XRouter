package main

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type Metrics struct {
	mu      sync.Mutex
	counts  map[string]int64
	latency map[string]time.Duration
}

func NewMetrics() *Metrics {
	return &Metrics{counts: map[string]int64{}, latency: map[string]time.Duration{}}
}

func (m *Metrics) Record(route, target string, status int, d time.Duration) {
	if route == "" {
		route = "unknown"
	}
	if target == "" {
		target = "unknown"
	}
	key := fmt.Sprintf("route=%q,target=%q,status=%q", route, target, statusClass(status))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counts[key]++
	m.latency[key] += d
}

func statusClass(status int) string {
	if status <= 0 {
		return "error"
	}
	return fmt.Sprintf("%dxx", status/100)
}

func (m *Metrics) Handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]string, 0, len(m.counts))
	for k := range m.counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# HELP xrouter_requests_total Total upstream attempts by route, target, and status class.\n")
	b.WriteString("# TYPE xrouter_requests_total counter\n")
	for _, k := range keys {
		b.WriteString(fmt.Sprintf("xrouter_requests_total{%s} %d\n", k, m.counts[k]))
	}
	b.WriteString("# HELP xrouter_upstream_latency_seconds_sum Sum of upstream latency.\n")
	b.WriteString("# TYPE xrouter_upstream_latency_seconds_sum counter\n")
	for _, k := range keys {
		b.WriteString(fmt.Sprintf("xrouter_upstream_latency_seconds_sum{%s} %.6f\n", k, m.latency[k].Seconds()))
	}
	_, _ = w.Write([]byte(b.String()))
}

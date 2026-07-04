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
	mu          sync.Mutex
	counts      map[string]int64
	latency     map[string]time.Duration
	targetStats map[string]*TargetRuntimeStats
}

type TargetRuntimeStats struct {
	Samples             int
	SuccessEWMA         float64
	LatencyEWMA         time.Duration
	ConsecutiveFailures int
	CircuitOpenUntil    time.Time
}

const (
	maxMetricSeries         = 4096
	maxRuntimeTargetStats   = 1024
	circuitFailureThreshold = 3
	circuitCooldown         = 30 * time.Second
)

func NewMetrics() *Metrics {
	return &Metrics{counts: map[string]int64{}, latency: map[string]time.Duration{}, targetStats: map[string]*TargetRuntimeStats{}}
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
	if _, ok := m.counts[key]; !ok && len(m.counts) >= maxMetricSeries {
		key = fmt.Sprintf("route=%q,target=%q,status=%q", "__overflow__", "__overflow__", statusClass(status))
	}
	m.counts[key]++
	m.latency[key] += d
	m.recordTargetStatsLocked(target, status, d)
}

func (m *Metrics) recordTargetStatsLocked(target string, status int, d time.Duration) {
	if target == "" {
		target = "unknown"
	}
	if _, ok := m.targetStats[target]; !ok && len(m.targetStats) >= maxRuntimeTargetStats {
		target = "__overflow__"
	}
	st := m.targetStats[target]
	if st == nil {
		st = &TargetRuntimeStats{SuccessEWMA: 1}
		m.targetStats[target] = st
	}
	success := status >= 200 && status < 300
	retryableFailure := status <= 0 || retryableStatus(status)
	alpha := 0.25
	if st.Samples == 0 {
		if success {
			st.SuccessEWMA = 1
		} else {
			st.SuccessEWMA = 0
		}
		st.LatencyEWMA = d
	} else {
		obs := 0.0
		if success {
			obs = 1
		}
		st.SuccessEWMA = alpha*obs + (1-alpha)*st.SuccessEWMA
		if d > 0 {
			st.LatencyEWMA = time.Duration(alpha*float64(d) + (1-alpha)*float64(st.LatencyEWMA))
		}
	}
	st.Samples++
	if success {
		st.ConsecutiveFailures = 0
		st.CircuitOpenUntil = time.Time{}
		return
	}
	if retryableFailure {
		st.ConsecutiveFailures++
		if st.ConsecutiveFailures >= circuitFailureThreshold {
			st.CircuitOpenUntil = time.Now().Add(circuitCooldown)
		}
	}
}

func (m *Metrics) TargetStats(target string) TargetRuntimeStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st := m.targetStats[target]; st != nil {
		return *st
	}
	return TargetRuntimeStats{}
}

func (m *Metrics) TargetScoreAdjustment(target string, maxLatency float64) float64 {
	st := m.TargetStats(target)
	if st.Samples == 0 {
		return 0
	}
	if time.Now().Before(st.CircuitOpenUntil) {
		return -1000
	}
	successAdj := (st.SuccessEWMA - 0.95) * 0.25
	latencyAdj := 0.0
	if maxLatency > 0 && st.LatencyEWMA > 0 {
		latencyAdj = -0.08 * clamp01(float64(st.LatencyEWMA.Milliseconds())/maxLatency)
	}
	return successAdj + latencyAdj
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

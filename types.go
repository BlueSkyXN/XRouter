package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Config struct {
	Server           ServerConfig              `json:"server"`
	Auth             AuthConfig                `json:"auth"`
	RequestOverrides RequestOverrideConfig     `json:"request_overrides"`
	Routing          RoutingConfig             `json:"routing"`
	PrefixCache      PrefixCacheConfig         `json:"prefix_cache"`
	Providers        map[string]ProviderConfig `json:"providers"`
	Targets          map[string]TargetConfig   `json:"targets"`
	Routes           map[string]RouteConfig    `json:"routes"`
}

type ServerConfig struct {
	Listen               string `json:"listen"`
	RequestTimeoutMS     int    `json:"request_timeout_ms"`
	ReadHeaderTimeoutMS  int    `json:"read_header_timeout_ms"`
	MaxRequestBodyBytes  int64  `json:"max_request_body_bytes"`
	MaxUpstreamBodyBytes int64  `json:"max_upstream_body_bytes"`
	Debug                bool   `json:"debug"`
}

type AuthConfig struct {
	APIKeysEnv string   `json:"api_keys_env"`
	APIKeys    []string `json:"api_keys"`
}

type RequestOverrideConfig struct {
	Enabled                  bool `json:"enabled"`
	AllowProviderKeyOverride bool `json:"allow_provider_key_override"`
	MaxShadowTargets         int  `json:"max_shadow_targets"`
	MaxListenerTargets       int  `json:"max_listener_targets"`
}

type ProviderConfig struct {
	BaseURL   string            `json:"base_url"`
	APIKeyEnv string            `json:"api_key_env"`
	APIKey    string            `json:"api_key"`
	Headers   map[string]string `json:"headers"`
	Supports  []string          `json:"supports"`
}

type CapabilityConfig struct {
	Tools     bool `json:"tools"`
	Vision    bool `json:"vision"`
	JSON      bool `json:"json"`
	Responses bool `json:"responses"`
}

type TargetConfig struct {
	Provider          string           `json:"provider"`
	Model             string           `json:"model"`
	Quality           float64          `json:"quality"`
	CostIn            float64          `json:"cost_in"`
	CostOut           float64          `json:"cost_out"`
	LatencyMS         float64          `json:"latency_ms"`
	Reliability       float64          `json:"reliability"`
	CacheSupportScore float64          `json:"cache_support_score"`
	Capabilities      CapabilityConfig `json:"capabilities"`
	Tags              []string         `json:"tags"`
	ExtraBody         map[string]any   `json:"extra_body"`
}

type RouteConfig struct {
	Type                   string                 `json:"type"`
	Kind                   string                 `json:"kind"`
	MatchPrefixes          []string               `json:"match_prefixes"`
	Target                 string                 `json:"target"`
	Candidates             []string               `json:"candidates"`
	Fallbacks              []string               `json:"fallbacks"`
	Objective              string                 `json:"objective"`
	StickyTTLSeconds       int                    `json:"sticky_ttl_seconds"`
	MultiModel             string                 `json:"multi_model"`
	MoARoute               string                 `json:"moa_route"`
	MoAComplexityThreshold float64                `json:"moa_complexity_threshold"`
	References             []string               `json:"references"`
	Aggregator             string                 `json:"aggregator"`
	Parallelism            int                    `json:"parallelism"`
	AllowPartial           bool                   `json:"allow_partial"`
	Flow                   string                 `json:"flow"`
	Stages                 []MoVStage             `json:"stages"`
	ReferencePrompt        string                 `json:"reference_prompt"`
	SynthesisPrompt        string                 `json:"synthesis_prompt"`
	ShadowTargets          []string               `json:"shadow_targets"`
	SerialListeners        []ListenerConfig       `json:"serial_listeners"`
	Weights                SmartWeights           `json:"weights"`
	KeywordRules           []KeywordRule          `json:"keyword_rules"`
	PrefixCache            PrefixCacheRouteConfig `json:"prefix_cache"`
	Judge                  JudgeConfig            `json:"judge"`
	Race                   RaceConfig             `json:"race"`
}

type RoutingConfig struct {
	UnknownModelPolicy string `json:"unknown_model_policy"` // reject | passthrough_openai | passthrough_openrouter
	DefaultRoute       string `json:"default_route"`
}

type SmartWeights struct {
	Quality     float64 `json:"quality"`
	Cost        float64 `json:"cost"`
	Latency     float64 `json:"latency"`
	Reliability float64 `json:"reliability"`
	Capability  float64 `json:"capability"`
	Cache       float64 `json:"cache"`
	Sticky      float64 `json:"sticky"`
	Judge       float64 `json:"judge"`
}

type KeywordRule struct {
	Name    string   `json:"name"`
	Any     []string `json:"any"`
	All     []string `json:"all"`
	Targets []string `json:"targets"`
	Tags    []string `json:"tags"`
	Boost   float64  `json:"boost"`
	Penalty float64  `json:"penalty"`
	Require bool     `json:"require"`
}

type PrefixCacheConfig struct {
	Enabled            bool   `json:"enabled"`
	MaxEntries         int    `json:"max_entries"`
	TTLSeconds         int    `json:"ttl_seconds"`
	PrefixChars        int    `json:"prefix_chars"`
	MinPrefixChars     int    `json:"min_prefix_chars"`
	HashSalt           string `json:"hash_salt"`
	RecencyHalfLifeSec int    `json:"recency_half_life_seconds"`
	UpdateFromUsage    bool   `json:"update_from_usage"`
}

type PrefixCacheRouteConfig struct {
	Enabled        *bool   `json:"enabled"`
	Weight         float64 `json:"weight"`
	PrefixChars    int     `json:"prefix_chars"`
	MinPrefixChars int     `json:"min_prefix_chars"`
}

type JudgeConfig struct {
	Enabled    bool     `json:"enabled"`
	Target     string   `json:"target"`
	Candidates []string `json:"candidates"`
	TimeoutMS  int      `json:"timeout_ms"`
	Weight     float64  `json:"weight"`
	Prompt     string   `json:"prompt"`
}

type MoVStage struct {
	Name    string   `json:"name"`
	Role    string   `json:"role"`
	Targets []string `json:"targets"`
	Prompt  string   `json:"prompt"`
}

type ListenerConfig struct {
	Name            string  `json:"name"`
	Target          string  `json:"target"`
	Mode            string  `json:"mode"`
	Prompt          string  `json:"prompt"`
	IncludeResponse bool    `json:"include_response"`
	TimeoutMS       int     `json:"timeout_ms"`
	SampleRate      float64 `json:"sample_rate"`
}

// RaceConfig controls MoV flows that issue multiple equivalent or near-equivalent
// attempts and choose a winner. It is intentionally transport-agnostic: the
// scorer reads generic OpenAI-style usage fields when present and falls back to
// visible-output length when reasoning-token telemetry is unavailable.
type RaceConfig struct {
	Selection         string   `json:"selection"` // max_output | usage_total | boundary_aware | fastest_acceptable
	Replicas          int      `json:"replicas"`
	Targets           []string `json:"targets"`
	Efforts           []string `json:"efforts"`
	BoundaryStart     int      `json:"boundary_start"`
	BoundaryStep      int      `json:"boundary_step"`
	BoundaryTolerance int      `json:"boundary_tolerance"`
	BoundaryPenalty   float64  `json:"boundary_penalty"`
	IncompletePenalty float64  `json:"incomplete_penalty"`
	MinVisibleTokens  int      `json:"min_visible_tokens"`
	VisibleWeight     float64  `json:"visible_weight"`
	OutputWeight      float64  `json:"output_weight"`
	ReasoningWeight   float64  `json:"reasoning_weight"`
	LatencyWeight     float64  `json:"latency_weight"`
	IncludeDebug      bool     `json:"include_debug"`
}

type XRouterControls struct {
	Route                 string            `json:"route"`
	Mode                  string            `json:"mode"`
	Target                string            `json:"target"`
	Targets               []string          `json:"targets"`
	Candidates            []string          `json:"candidates"`
	References            []string          `json:"references"`
	Aggregator            string            `json:"aggregator"`
	Objective             string            `json:"objective"`
	MultiModel            string            `json:"multi_model"`
	SessionID             string            `json:"session_id"`
	DryRun                bool              `json:"dry_run"`
	Explain               bool              `json:"explain"`
	CachePrefixHint       string            `json:"cache_prefix_hint"`
	DisablePrefixCache    bool              `json:"disable_prefix_cache"`
	JudgeEnabled          *bool             `json:"judge_enabled"`
	ShadowTargets         []string          `json:"shadow_targets"`
	ListenerTargets       []string          `json:"listener_targets"`
	IncludeListenerOutput bool              `json:"include_listener_output"`
	DisableListeners      bool              `json:"disable_listeners"`
	DisableShadow         bool              `json:"disable_shadow"`
	ProviderAPIKeys       map[string]string `json:"provider_api_keys"`
}

type APIKind string

const (
	APIChat      APIKind = "chat"
	APIResponses APIKind = "responses"
)

type UpstreamResult struct {
	TargetName string
	Target     TargetConfig
	Status     int
	Headers    map[string]string
	Body       []byte
	Duration   time.Duration
	Wrote      bool
	Err        error
}

type RouteDecision struct {
	PublicModel string
	RouteName   string
	Route       RouteConfig
	TargetNames []string
	IsVirtual   bool
	Controls    XRouterControls
	Complexity  float64
}

type ResponseError struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

func (c *Config) applyDefaults() {
	if c.Server.Listen == "" {
		c.Server.Listen = ":8080"
	}
	if c.Server.RequestTimeoutMS <= 0 {
		c.Server.RequestTimeoutMS = 120000
	}
	if c.Server.ReadHeaderTimeoutMS <= 0 {
		c.Server.ReadHeaderTimeoutMS = 10000
	}
	if c.Server.MaxRequestBodyBytes <= 0 {
		c.Server.MaxRequestBodyBytes = 32 << 20
	}
	if c.Server.MaxUpstreamBodyBytes <= 0 {
		c.Server.MaxUpstreamBodyBytes = 64 << 20
	}
	if c.RequestOverrides.MaxShadowTargets <= 0 {
		c.RequestOverrides.MaxShadowTargets = 4
	}
	if c.RequestOverrides.MaxListenerTargets <= 0 {
		c.RequestOverrides.MaxListenerTargets = 4
	}
	if c.Routing.UnknownModelPolicy == "" {
		c.Routing.UnknownModelPolicy = "reject"
	}
	if c.PrefixCache.MaxEntries <= 0 {
		c.PrefixCache.MaxEntries = 4096
	}
	if c.PrefixCache.TTLSeconds <= 0 {
		c.PrefixCache.TTLSeconds = 86400
	}
	if c.PrefixCache.PrefixChars <= 0 {
		c.PrefixCache.PrefixChars = 4096
	}
	if c.PrefixCache.MinPrefixChars <= 0 {
		c.PrefixCache.MinPrefixChars = 128
	}
	if c.PrefixCache.RecencyHalfLifeSec <= 0 {
		c.PrefixCache.RecencyHalfLifeSec = 1800
	}
	if c.Providers == nil {
		c.Providers = map[string]ProviderConfig{}
	}
	if c.Targets == nil {
		c.Targets = map[string]TargetConfig{}
	}
	if c.Routes == nil {
		c.Routes = map[string]RouteConfig{}
	}
	for k, r := range c.Routes {
		r.Type = normalizeRouteKind(firstNonEmpty(r.Kind, r.Type))
		r.Kind = r.Type
		if r.Flow == "" && r.Type == "moa" {
			r.Flow = "parallel_synthesize_v1"
		}
		if r.Objective == "" {
			r.Objective = "balanced"
		}
		if r.StickyTTLSeconds <= 0 {
			r.StickyTTLSeconds = 300
		}
		r.MultiModel = strings.ToLower(strings.TrimSpace(r.MultiModel))
		if r.MultiModel == "" {
			if r.Type == "moa" || r.Type == "mov" {
				r.MultiModel = "always"
			} else {
				r.MultiModel = "never"
			}
		}
		if r.MoAComplexityThreshold <= 0 {
			r.MoAComplexityThreshold = 0.62
		}
		if r.Parallelism < 0 {
			r.Parallelism = 0
		}
		if r.ReferencePrompt == "" {
			r.ReferencePrompt = "Give an independent concise answer."
		}
		if r.SynthesisPrompt == "" {
			r.SynthesisPrompt = "Synthesize the reference outputs into one final answer."
		}
		configuredJudgeWeight := r.Judge.Weight
		r.Weights = defaultSmartWeights(r.Objective, r.Weights)
		if r.PrefixCache.Weight <= 0 {
			r.PrefixCache.Weight = 0.18
		}
		if r.Judge.TimeoutMS <= 0 {
			r.Judge.TimeoutMS = 1200
		}
		if configuredJudgeWeight > 0 {
			r.Weights.Judge = configuredJudgeWeight
		} else {
			r.Judge.Weight = r.Weights.Judge
		}
		r.Race = defaultRaceConfig(r.Race)
		for i := range r.SerialListeners {
			l := &r.SerialListeners[i]
			l.Mode = strings.ToLower(strings.TrimSpace(l.Mode))
			if l.Mode == "" {
				l.Mode = "serial"
			}
			if l.TimeoutMS <= 0 {
				l.TimeoutMS = 15000
			}
			if l.SampleRate <= 0 {
				l.SampleRate = 1
			}
			if l.Prompt == "" {
				l.Prompt = defaultListenerPrompt()
			}
		}
		c.Routes[k] = r
	}
	for k, t := range c.Targets {
		if t.Quality == 0 {
			t.Quality = 0.5
		}
		if t.Reliability == 0 {
			t.Reliability = 0.95
		}
		if t.LatencyMS == 0 {
			t.LatencyMS = 1500
		}
		if t.CacheSupportScore == 0 {
			t.CacheSupportScore = 0.5
		}
		c.Targets[k] = t
	}
}

func effectiveParallelism(configured, workItems int) int {
	if workItems <= 0 {
		return 1
	}
	if configured <= 0 || configured > workItems {
		return workItems
	}
	return configured
}

func defaultRaceConfig(r RaceConfig) RaceConfig {
	r.Selection = normalizeRaceSelection(r.Selection)
	if r.Replicas <= 0 {
		r.Replicas = 2
	}
	if r.BoundaryStart <= 0 {
		r.BoundaryStart = 516
	}
	if r.BoundaryStep <= 0 {
		r.BoundaryStep = 518
	}
	if r.BoundaryTolerance < 0 {
		r.BoundaryTolerance = 0
	}
	if r.BoundaryPenalty <= 0 {
		r.BoundaryPenalty = 2500
	}
	if r.IncompletePenalty <= 0 {
		r.IncompletePenalty = 5000
	}
	if r.VisibleWeight <= 0 {
		r.VisibleWeight = 1.0
	}
	if r.OutputWeight < 0 {
		r.OutputWeight = 0
	}
	if r.OutputWeight == 0 {
		r.OutputWeight = 0.2
	}
	if r.ReasoningWeight < 0 {
		r.ReasoningWeight = 0
	}
	if r.LatencyWeight < 0 {
		r.LatencyWeight = 0
	}
	return r
}

func normalizeRaceSelection(selection string) string {
	return strings.ToLower(strings.TrimSpace(strings.ReplaceAll(selection, "-", "_")))
}

func normalizeRouteKind(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "-", "_")
	switch s {
	case "", "direct", "direct_alias", "alias":
		return "direct"
	case "auto", "smart", "smart_router":
		return "auto"
	case "moa":
		return "moa"
	case "mov", "multi", "multi_model":
		return "mov"
	case "pass_through", "passthrough":
		return "passthrough"
	default:
		return s
	}
}

func defaultSmartWeights(objective string, w SmartWeights) SmartWeights {
	if w.Quality+w.Cost+w.Latency+w.Reliability+w.Capability+w.Cache+w.Sticky+w.Judge > 0 {
		return w
	}
	switch strings.ToLower(strings.TrimSpace(objective)) {
	case "quality":
		return SmartWeights{Quality: 0.48, Cost: 0.06, Latency: 0.06, Reliability: 0.18, Capability: 0.12, Cache: 0.04, Sticky: 0.02, Judge: 0.04}
	case "cost":
		return SmartWeights{Quality: 0.16, Cost: 0.42, Latency: 0.12, Reliability: 0.12, Capability: 0.08, Cache: 0.06, Sticky: 0.02, Judge: 0.02}
	case "latency":
		return SmartWeights{Quality: 0.18, Cost: 0.10, Latency: 0.40, Reliability: 0.14, Capability: 0.08, Cache: 0.06, Sticky: 0.02, Judge: 0.02}
	default:
		return SmartWeights{Quality: 0.30, Cost: 0.18, Latency: 0.14, Reliability: 0.16, Capability: 0.10, Cache: 0.08, Sticky: 0.02, Judge: 0.02}
	}
}

func (p ProviderConfig) supports(kind APIKind) bool {
	for _, s := range p.Supports {
		if strings.EqualFold(s, string(kind)) {
			return true
		}
	}
	return false
}

func (t TargetConfig) supports(kind APIKind) bool {
	if kind == APIChat {
		return true
	}
	if kind == APIResponses {
		return t.Capabilities.Responses
	}
	return false
}

func cloneJSONMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	b, _ := json.Marshal(in)
	var out map[string]any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	_ = dec.Decode(&out)
	if out == nil {
		out = map[string]any{}
	}
	return out
}

func cloneTopLevelJSONMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func compactJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err != nil {
		return string(b)
	}
	return buf.String()
}

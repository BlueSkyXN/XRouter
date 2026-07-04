package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

type PrefixCacheStore struct {
	mu      sync.Mutex
	cfg     PrefixCacheConfig
	entries map[string]map[string]*PrefixCacheEntry
	count   int
}

type PrefixCacheEntry struct {
	Target       string    `json:"target"`
	LastSeen     time.Time `json:"last_seen"`
	Hits         int       `json:"hits"`
	CachedTokens int       `json:"cached_tokens"`
	Provider     string    `json:"provider"`
}

func NewPrefixCacheStore(cfg PrefixCacheConfig) *PrefixCacheStore {
	if weakPrefixHashSalt(cfg.HashSalt) {
		cfg.HashSalt = runtimePrefixHashSalt()
	}
	return &PrefixCacheStore{cfg: cfg, entries: map[string]map[string]*PrefixCacheEntry{}}
}

func weakPrefixHashSalt(s string) bool {
	s = strings.TrimSpace(s)
	return s == "" || s == "replace-me-per-deployment"
}

func runtimePrefixHashSalt() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	fallback := sha256.Sum256([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	return hex.EncodeToString(fallback[:])
}

func (p *PrefixCacheStore) Enabled(route RouteConfig, controls XRouterControls) bool {
	if p == nil || controls.DisablePrefixCache {
		return false
	}
	if route.PrefixCache.Enabled != nil {
		return *route.PrefixCache.Enabled
	}
	return p.cfg.Enabled
}

func (p *PrefixCacheStore) PrefixKey(body map[string]any, kind APIKind, route RouteConfig, controls XRouterControls) (string, bool) {
	if p == nil {
		return "", false
	}
	text := strings.TrimSpace(controls.CachePrefixHint)
	if text == "" {
		text = requestText(body, kind)
	}
	prefixChars := route.PrefixCache.PrefixChars
	if prefixChars <= 0 {
		prefixChars = p.cfg.PrefixChars
	}
	minChars := route.PrefixCache.MinPrefixChars
	if minChars <= 0 {
		minChars = p.cfg.MinPrefixChars
	}
	runes := []rune(text)
	if len(runes) < minChars {
		return "", false
	}
	if len(runes) > prefixChars {
		runes = runes[:prefixChars]
	}
	// Hash only: do not keep raw prompt prefix in memory.
	h := sha256.Sum256([]byte(p.cfg.HashSalt + "\x00" + string(runes)))
	return hex.EncodeToString(h[:]), true
}

func (p *PrefixCacheStore) Scores(key string, candidates []string, targets map[string]TargetConfig, route RouteConfig) map[string]float64 {
	out := map[string]float64{}
	if p == nil || key == "" {
		return out
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	bucket := p.entries[key]
	if len(bucket) == 0 {
		return out
	}
	ttl := time.Duration(p.cfg.TTLSeconds) * time.Second
	half := float64(p.cfg.RecencyHalfLifeSec)
	if half <= 0 {
		half = 1800
	}
	for _, name := range candidates {
		entry := bucket[name]
		if entry == nil {
			continue
		}
		age := now.Sub(entry.LastSeen)
		if age > ttl {
			delete(bucket, name)
			p.count--
			if len(bucket) == 0 {
				delete(p.entries, key)
			}
			continue
		}
		recency := math.Pow(0.5, age.Seconds()/half)
		strength := math.Min(1, float64(entry.Hits)/4.0)
		cached := math.Min(1, float64(entry.CachedTokens)/4096.0)
		cacheSupport := 0.5
		if t, ok := targets[name]; ok && t.CacheSupportScore > 0 {
			cacheSupport = clamp01(t.CacheSupportScore)
		}
		out[name] = clamp01((0.55*recency + 0.25*strength + 0.20*cached) * cacheSupport)
	}
	return out
}

func (p *PrefixCacheStore) Touch(key, targetName string, target TargetConfig, cachedTokens int) {
	if p == nil || key == "" || targetName == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.entries[key] == nil {
		p.entries[key] = map[string]*PrefixCacheEntry{}
	}
	entry := p.entries[key][targetName]
	if entry == nil {
		entry = &PrefixCacheEntry{Target: targetName, Provider: target.Provider}
		p.entries[key][targetName] = entry
		p.count++
	}
	entry.LastSeen = time.Now()
	entry.Hits++
	if cachedTokens > entry.CachedTokens {
		entry.CachedTokens = cachedTokens
	}
	p.evictLocked()
}

func (p *PrefixCacheStore) Snapshot() map[string][]PrefixCacheEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := map[string][]PrefixCacheEntry{}
	for k, bucket := range p.entries {
		for _, e := range bucket {
			out[k] = append(out[k], *e)
		}
		sort.Slice(out[k], func(i, j int) bool { return out[k][i].LastSeen.After(out[k][j].LastSeen) })
	}
	return out
}

func (p *PrefixCacheStore) evictLocked() {
	max := p.cfg.MaxEntries
	if max <= 0 {
		max = 4096
	}
	if p.count <= max {
		return
	}
	type pair struct {
		key, target string
		last        time.Time
	}
	var all []pair
	for k, bucket := range p.entries {
		for t, e := range bucket {
			all = append(all, pair{k, t, e.LastSeen})
		}
	}
	if len(all) <= max {
		p.count = len(all)
		return
	}
	sort.Slice(all, func(i, j int) bool { return all[i].last.Before(all[j].last) })
	over := len(all) - max
	batch := max / 10
	if batch < 1 {
		batch = 1
	}
	if over < batch {
		over = batch
	}
	if over > len(all) {
		over = len(all)
	}
	for _, x := range all[:over] {
		delete(p.entries[x.key], x.target)
		p.count--
		if len(p.entries[x.key]) == 0 {
			delete(p.entries, x.key)
		}
	}
}

func extractCachedTokens(raw []byte) int {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return 0
	}
	usage, _ := obj["usage"].(map[string]any)
	if usage == nil {
		return 0
	}
	for _, path := range [][]string{{"prompt_tokens_details", "cached_tokens"}, {"input_tokens_details", "cached_tokens"}} {
		m, _ := usage[path[0]].(map[string]any)
		if m == nil {
			continue
		}
		if n := anyToInt(m[path[1]]); n > 0 {
			return n
		}
	}
	return 0
}

func anyToInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case json.Number:
		i, _ := x.Int64()
		return int(i)
	default:
		return 0
	}
}

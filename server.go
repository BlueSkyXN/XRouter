package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"
)

type Server struct {
	cfg       Config
	client    *http.Client
	sticky    *StickyStore
	metrics   *Metrics
	prefixBK  *PrefixCacheStore
	apiKeys   map[string]struct{}
	startTime time.Time
}

func NewServer(cfg Config) *Server {
	return &Server{
		cfg:       cfg,
		client:    &http.Client{Timeout: time.Duration(cfg.Server.RequestTimeoutMS) * time.Millisecond},
		sticky:    NewStickyStore(),
		metrics:   NewMetrics(),
		prefixBK:  NewPrefixCacheStore(cfg.PrefixCache),
		apiKeys:   configuredAPIKeys(cfg.Auth),
		startTime: time.Now(),
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/metrics", s.withAuth(s.metrics.Handler))
	mux.HandleFunc("/debug/prefix-cache", s.withAuth(s.handlePrefixCacheSnapshot))
	mux.HandleFunc("/v1/models", s.withAuth(s.handleModels))
	mux.HandleFunc("/v1/chat/completions", s.withAuth(s.handleChat))
	mux.HandleFunc("/v1/responses", s.withAuth(s.handleResponses))
	return s.withCommonHeaders(mux)
}

func (s *Server) withCommonHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-xrouter", "xrouter-go-only")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(s.apiKeys) == 0 {
			next(w, r)
			return
		}
		if _, ok := s.apiKeys[bearerToken(r)]; !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid XRouter API key")
			return
		}
		next(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "impl": "go", "uptime_seconds": int(time.Since(s.startTime).Seconds())})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	type modelObj struct {
		ID      string         `json:"id"`
		Object  string         `json:"object"`
		Created int64          `json:"created"`
		OwnedBy string         `json:"owned_by"`
		Meta    map[string]any `json:"xrouter,omitempty"`
	}
	ids := make([]string, 0, len(s.cfg.Routes)+len(s.cfg.Targets))
	for id := range s.cfg.Routes {
		ids = append(ids, id)
	}
	for id := range s.cfg.Targets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	data := make([]modelObj, 0, len(ids))
	for _, id := range ids {
		if route, ok := s.cfg.Routes[id]; ok {
			data = append(data, modelObj{ID: id, Object: "model", Created: 0, OwnedBy: "xrouter", Meta: map[string]any{"type": route.Type, "kind": route.Kind, "flow": route.Flow, "candidates": route.Candidates, "references": route.References, "aggregator": route.Aggregator}})
			continue
		}
		if target, ok := s.cfg.Targets[id]; ok {
			data = append(data, modelObj{ID: id, Object: "model", Created: 0, OwnedBy: target.Provider, Meta: map[string]any{"upstream_model": target.Model, "provider": target.Provider, "quality": target.Quality, "cost_in": target.CostIn, "cost_out": target.CostOut}})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
}

func (s *Server) handlePrefixCacheSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
		return
	}
	if s.prefixBK == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": s.cfg.PrefixCache.Enabled, "entries": s.prefixBK.Snapshot()})
}

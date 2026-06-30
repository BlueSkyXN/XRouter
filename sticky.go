package main

import (
	"sync"
	"time"
)

type StickyStore struct {
	mu    sync.Mutex
	items map[string]StickyItem
}

type StickyItem struct {
	Target    string
	ExpiresAt time.Time
}

func NewStickyStore() *StickyStore {
	return &StickyStore{items: map[string]StickyItem{}}
}

func (s *StickyStore) Get(sessionID string) (string, bool) {
	if sessionID == "" {
		return "", false
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[sessionID]
	if !ok {
		return "", false
	}
	if now.After(item.ExpiresAt) {
		delete(s.items, sessionID)
		return "", false
	}
	return item.Target, true
}

func (s *StickyStore) Set(sessionID, target string, ttl time.Duration) {
	if sessionID == "" || target == "" || ttl <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[sessionID] = StickyItem{Target: target, ExpiresAt: time.Now().Add(ttl)}
}

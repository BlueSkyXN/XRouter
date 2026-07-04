package main

import (
	"sync"
	"time"
)

type StickyStore struct {
	mu    sync.Mutex
	items map[string]StickyItem
	max   int
}

type StickyItem struct {
	Target    string
	ExpiresAt time.Time
	UpdatedAt time.Time
}

func NewStickyStore() *StickyStore {
	return &StickyStore{items: map[string]StickyItem{}, max: 4096}
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
	now := time.Now()
	s.sweepExpiredLocked(now)
	if s.max > 0 && len(s.items) >= s.max {
		s.evictOldestLocked()
	}
	s.items[sessionID] = StickyItem{Target: target, ExpiresAt: now.Add(ttl), UpdatedAt: now}
}

func (s *StickyStore) sweepExpiredLocked(now time.Time) {
	for id, item := range s.items {
		if now.After(item.ExpiresAt) {
			delete(s.items, id)
		}
	}
}

func (s *StickyStore) evictOldestLocked() {
	oldestID := ""
	var oldest time.Time
	for id, item := range s.items {
		candidate := item.UpdatedAt
		if candidate.IsZero() {
			candidate = item.ExpiresAt
		}
		if oldestID == "" || candidate.Before(oldest) {
			oldestID = id
			oldest = candidate
		}
	}
	if oldestID != "" {
		delete(s.items, oldestID)
	}
}

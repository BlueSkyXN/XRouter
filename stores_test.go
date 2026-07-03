package main

import (
	"fmt"
	"testing"
	"time"
)

func TestStickyStoreEvictsWhenBounded(t *testing.T) {
	store := NewStickyStore()
	store.max = 2
	store.Set("a", "ta", time.Minute)
	store.Set("b", "tb", time.Minute)
	store.Set("c", "tc", time.Minute)
	if len(store.items) != 2 {
		t.Fatalf("expected sticky store to stay bounded at 2 items, got %d", len(store.items))
	}
	if _, ok := store.Get("a"); ok {
		t.Fatal("expected oldest sticky item to be evicted")
	}
}

func TestPrefixCacheEvictsInBatchesWhenBounded(t *testing.T) {
	store := NewPrefixCacheStore(PrefixCacheConfig{MaxEntries: 5})
	target := TargetConfig{Provider: "p"}
	for i := 0; i < 8; i++ {
		store.Touch(fmt.Sprintf("key-%d", i), "target", target, 0)
	}
	if store.count > 5 {
		t.Fatalf("expected prefix cache count <= 5, got %d", store.count)
	}
	total := 0
	for _, entries := range store.Snapshot() {
		total += len(entries)
	}
	if total > 5 {
		t.Fatalf("expected snapshot size <= 5, got %d", total)
	}
}

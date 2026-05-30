package auth

import (
	"testing"
	"time"
)

func TestRateLimiterAllowsUnlimitedWhenLimitIsZero(t *testing.T) {
	limiter := NewRateLimiter()
	now := time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 100; i++ {
		ok, retry := limiter.Allow("ip:key", 0, now)
		if !ok || retry != 0 {
			t.Fatalf("request %d blocked with retry %s", i, retry)
		}
	}
}

func TestRateLimiterBlocksWithinSlidingWindow(t *testing.T) {
	limiter := NewRateLimiter()
	now := time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		ok, retry := limiter.Allow("ip:key", 2, now.Add(time.Duration(i)*time.Second))
		if !ok || retry != 0 {
			t.Fatalf("request %d blocked with retry %s", i, retry)
		}
	}

	ok, retry := limiter.Allow("ip:key", 2, now.Add(2*time.Second))
	if ok || retry <= 0 {
		t.Fatalf("expected block with retry, got ok=%v retry=%s", ok, retry)
	}

	ok, retry = limiter.Allow("ip:key", 2, now.Add(61*time.Second))
	if !ok || retry != 0 {
		t.Fatalf("expected window to slide, got ok=%v retry=%s", ok, retry)
	}
}

func TestRateLimiterPrunesStaleKeys(t *testing.T) {
	limiter := NewRateLimiter()
	now := time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		key := string(rune('a' + i))
		if ok, _ := limiter.Allow(key, 1, now); !ok {
			t.Fatalf("initial request for %s blocked", key)
		}
	}
	if len(limiter.entries) != 10 {
		t.Fatalf("got %d entries, want 10", len(limiter.entries))
	}
	if ok, _ := limiter.Allow("fresh", 1, now.Add(2*time.Minute)); !ok {
		t.Fatal("fresh request blocked")
	}
	if len(limiter.entries) != 1 {
		t.Fatalf("stale entries not pruned: %#v", limiter.entries)
	}
}

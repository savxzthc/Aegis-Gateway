package auth

import (
	"sync"
	"time"
)

// RateLimiter implements an in-memory sliding-window request limiter.
type RateLimiter struct {
	mu        sync.Mutex
	entries   map[string][]time.Time
	lastPrune time.Time
	stop      chan struct{}
}

// NewRateLimiter creates an empty sliding-window limiter.
func NewRateLimiter() *RateLimiter {
	limiter := &RateLimiter{
		entries: map[string][]time.Time{},
		stop:    make(chan struct{}),
	}
	go limiter.pruneLoop(time.Minute)
	return limiter
}

// Stop stops background pruning for tests or controlled shutdown.
func (r *RateLimiter) Stop() {
	select {
	case <-r.stop:
	default:
		close(r.stop)
	}
}

// Allow records a request if it fits within limit and returns retry timing.
func (r *RateLimiter) Allow(key string, limit int, now time.Time) (bool, time.Duration) {
	if limit <= 0 {
		return true, 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	windowStart := now.Add(-time.Minute)
	if r.lastPrune.IsZero() || now.Sub(r.lastPrune) >= time.Minute {
		r.pruneLocked(windowStart, now)
	}
	events := r.entries[key]
	keep := events[:0]
	for _, event := range events {
		if event.After(windowStart) {
			keep = append(keep, event)
		}
	}
	if len(keep) >= limit {
		retry := keep[0].Add(time.Minute).Sub(now)
		if retry < time.Second {
			retry = time.Second
		}
		r.entries[key] = keep
		return false, retry
	}
	keep = append(keep, now)
	r.entries[key] = keep
	return true, 0
}

func (r *RateLimiter) pruneLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			now := time.Now().UTC()
			r.mu.Lock()
			r.pruneLocked(now.Add(-time.Minute), now)
			r.mu.Unlock()
		case <-r.stop:
			return
		}
	}
}

func (r *RateLimiter) pruneLocked(windowStart, now time.Time) {
	for key, events := range r.entries {
		keep := events[:0]
		for _, event := range events {
			if event.After(windowStart) {
				keep = append(keep, event)
			}
		}
		if len(keep) == 0 {
			delete(r.entries, key)
			continue
		}
		r.entries[key] = keep
	}
	r.lastPrune = now
}

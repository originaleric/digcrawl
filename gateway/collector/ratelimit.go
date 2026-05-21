package collector

import (
	"sync"
	"time"
)

// RateLimiter applies per-key cooldown after calls (in-memory, single collector process).
type RateLimiter struct {
	mu       sync.Mutex
	limits   map[string]time.Time
	cooldown time.Duration
}

func NewRateLimiter(cooldown time.Duration) *RateLimiter {
	if cooldown <= 0 {
		cooldown = time.Second
	}
	return &RateLimiter{
		limits:   make(map[string]time.Time),
		cooldown: cooldown,
	}
}

// Check returns false if the key is still in cooldown.
func (r *RateLimiter) Check(key string) bool {
	if key == "" {
		return true
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	if until, ok := r.limits[key]; ok && now.Before(until) {
		return false
	}
	return true
}

// Touch starts cooldown for a key after a call completes.
func (r *RateLimiter) Touch(key string) {
	if key == "" {
		return
	}
	r.mu.Lock()
	r.limits[key] = time.Now().Add(r.cooldown)
	r.mu.Unlock()
}

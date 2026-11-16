package proxy

import (
	"sync"
	"time"
)

type tokenBucket struct {
	tokens     float64
	lastRefill time.Time
}

// IPRateLimiter provides per-client-IP token bucket rate limiting to prevent hot IPs or DDoS floods from crashing backends.
type IPRateLimiter struct {
	mu         sync.Mutex
	clients    map[string]*tokenBucket
	rate       float64 // tokens per second
	burst      float64 // maximum bucket capacity
	lastClean  time.Time
	cleanupTTL time.Duration
}

// NewIPRateLimiter creates a new IPRateLimiter with the specified rate and burst capacity.
// If rate <= 0, the rate limiter is disabled and Allow always returns true.
func NewIPRateLimiter(ratePerSec int, burst int) *IPRateLimiter {
	if ratePerSec <= 0 {
		return nil
	}
	if burst <= 0 {
		burst = ratePerSec
	}
	return &IPRateLimiter{
		clients:    make(map[string]*tokenBucket),
		rate:       float64(ratePerSec),
		burst:      float64(burst),
		lastClean:  time.Now(),
		cleanupTTL: 2 * time.Minute,
	}
}

// Allow checks if the given IP address is permitted to execute a request under token bucket rules.
func (lim *IPRateLimiter) Allow(ip string) bool {
	if lim == nil {
		return true
	}

	lim.mu.Lock()
	defer lim.mu.Unlock()

	now := time.Now()

	// Periodic cleanup of idle entries to prevent unbounded memory growth
	if now.Sub(lim.lastClean) > lim.cleanupTTL {
		for k, b := range lim.clients {
			if now.Sub(b.lastRefill) > lim.cleanupTTL {
				delete(lim.clients, k)
			}
		}
		lim.lastClean = now
	}

	b, exists := lim.clients[ip]
	if !exists {
		lim.clients[ip] = &tokenBucket{
			tokens:     lim.burst - 1,
			lastRefill: now,
		}
		return true
	}

	// Refill tokens based on elapsed duration
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * lim.rate
	if b.tokens > lim.burst {
		b.tokens = lim.burst
	}
	b.lastRefill = now

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}

	return false
}

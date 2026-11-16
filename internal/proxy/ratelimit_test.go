package proxy

import (
	"testing"
	"time"
)

func TestIPRateLimiter(t *testing.T) {
	// Rate: 10 req/s, Burst: 5
	lim := NewIPRateLimiter(10, 5)

	clientIP := "192.168.1.100"

	// First 5 requests should pass (consuming burst)
	for i := 0; i < 5; i++ {
		if !lim.Allow(clientIP) {
			t.Fatalf("request %d should have been allowed", i+1)
		}
	}

	// 6th request immediately after must be denied
	if lim.Allow(clientIP) {
		t.Errorf("6th request should have been rate limited")
	}

	// Wait 250ms -> should refill ~2.5 tokens
	time.Sleep(250 * time.Millisecond)

	// Now it should allow again
	if !lim.Allow(clientIP) {
		t.Errorf("request after token refill should have been allowed")
	}
}

func TestNilRateLimiter(t *testing.T) {
	var lim *IPRateLimiter
	if !lim.Allow("1.2.3.4") {
		t.Errorf("nil rate limiter should always allow")
	}
}

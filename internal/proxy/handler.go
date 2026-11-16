package proxy

import (
	"encoding/json"
	"math"
	"net"
	"net/http"
	"net/http/httputil"
	"sync/atomic"
	"time"

	"baryon/internal/events"
)

// Ring defines the subset of HashRing functionality required by ProxyHandler.
type Ring interface {
	GetNode(key string) *Backend
	GetNodeWithBoundedLoad(key string, isOverloaded func(*Backend) bool) *Backend
	GetNodes() []*Backend
}

// ProxyHandler handles incoming HTTP requests, routes them via consistent hashing,
// and proxies them to the selected backend.
type ProxyHandler struct {
	ring                Ring
	eventEmitter        events.EventEmitter
	trustedProxies      []string
	totalRequests       atomic.Uint64
	limiter             *IPRateLimiter
	boundedLoadFactor   float64
	maxActivePerBackend int
}

// NewProxyHandler creates a new ProxyHandler with the specified ring and options.
func NewProxyHandler(ring Ring, emitter events.EventEmitter, trustedProxies []string) *ProxyHandler {
	return &ProxyHandler{
		ring:              ring,
		eventEmitter:      emitter,
		trustedProxies:    trustedProxies,
		boundedLoadFactor: 1.25,
	}
}

// SetBoundedLoad configures the load factor for Consistent Hashing with Bounded Loads.
// A factor of 1.25 means no backend takes more than 125% of average active requests.
func (h *ProxyHandler) SetBoundedLoad(factor float64, maxActive int) {
	h.boundedLoadFactor = factor
	h.maxActivePerBackend = maxActive
}

// SetRateLimiter configures a per-client-IP token bucket rate limiter.
func (h *ProxyHandler) SetRateLimiter(ratePerSec, burst int) {
	h.limiter = NewIPRateLimiter(ratePerSec, burst)
}

// SetupBackendReverseProxy configures the httputil.ReverseProxy for a backend server.
func SetupBackendReverseProxy(b *Backend, transport http.RoundTripper) {
	rp := httputil.NewSingleHostReverseProxy(b.URL)
	if transport == nil {
		transport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   3 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   5 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}
	rp.Transport = transport

	originalDirector := rp.Director
	rp.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = b.URL.Host
	}

	rp.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":   "bad gateway",
			"backend": b.Name,
			"message": err.Error(),
		})
	}

	b.ReverseProxy = rp
}

// ServeHTTP implements http.Handler, executing consistent hash routing and reverse proxying.
func (h *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	clientIP := ExtractClientIP(r, h.trustedProxies)

	// 1. Per-IP Rate Limiting: protect upstream backends from hot IP abuse or runaway floods
	if h.limiter != nil && !h.limiter.Allow(clientIP) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":   "too many requests",
			"message": "per-client rate limit exceeded; request throttled to protect backend nodes",
		})
		return
	}

	// 2. Consistent Hashing with Bounded Loads: evaluate if primary candidate is overloaded
	var isOverloaded func(*Backend) bool
	if h.boundedLoadFactor > 0 || h.maxActivePerBackend > 0 {
		nodes := h.ring.GetNodes()
		var totalActive int64
		healthyCount := 0
		for _, n := range nodes {
			if n.IsAlive() {
				healthyCount++
				_, active := n.GetStats()
				if active > 0 {
					totalActive += active
				}
			}
		}

		if healthyCount > 0 {
			avgLoad := float64(totalActive) / float64(healthyCount)
			// Ceiling threshold: at least 5 active requests before triggering spillover
			threshold := int64(math.Ceil(avgLoad * h.boundedLoadFactor))
			if threshold < 5 {
				threshold = 5
			}
			if h.maxActivePerBackend > 0 && int64(h.maxActivePerBackend) < threshold {
				threshold = int64(h.maxActivePerBackend)
			}
			isOverloaded = func(b *Backend) bool {
				_, active := b.GetStats()
				return active >= threshold
			}
		}
	}

	// Consistent hash lookup with bounded loads (lock released immediately inside GetNode before I/O)
	backend := h.ring.GetNodeWithBoundedLoad(clientIP, isOverloaded)
	if backend == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":   "service unavailable",
			"message": "no healthy backends available in hash ring",
		})
		return
	}

	// Update atomic counters
	h.totalRequests.Add(1)
	backend.IncRequests()
	backend.IncActive()
	defer backend.DecActive()

	// Emit non-blocking routing event
	if h.eventEmitter != nil {
		h.eventEmitter.EmitRequest(events.RequestEvent{
			Time:      time.Now(),
			ClientIP:  clientIP,
			BackendID: backend.Name,
			Path:      r.URL.Path,
		})
	}

	// Forward request through reverse proxy
	backend.ReverseProxy.ServeHTTP(w, r)
}

// TotalRequests returns the total number of HTTP requests processed by the proxy.
func (h *ProxyHandler) TotalRequests() uint64 {
	return h.totalRequests.Load()
}


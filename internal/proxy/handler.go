package proxy

import (
	"encoding/json"
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
}

// ProxyHandler handles incoming HTTP requests, routes them via consistent hashing,
// and proxies them to the selected backend.
type ProxyHandler struct {
	ring           Ring
	eventEmitter   events.EventEmitter
	trustedProxies []string
	totalRequests  atomic.Uint64
}

// NewProxyHandler creates a new ProxyHandler with the specified ring and options.
func NewProxyHandler(ring Ring, emitter events.EventEmitter, trustedProxies []string) *ProxyHandler {
	return &ProxyHandler{
		ring:           ring,
		eventEmitter:   emitter,
		trustedProxies: trustedProxies,
	}
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

	// Consistent hash lookup (lock is released immediately inside GetNode before I/O)
	backend := h.ring.GetNode(clientIP)
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


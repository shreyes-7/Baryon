package proxy

import (
	"net/http/httputil"
	"net/url"
	"sync/atomic"
)

// Backend represents a target upstream HTTP server and its runtime metrics.
type Backend struct {
	Name         string
	URL          *url.URL
	ReverseProxy *httputil.ReverseProxy

	alive        atomic.Bool
	totalReqs    atomic.Uint64
	activeReqs   atomic.Int64
}

// NewBackend creates a new Backend with the given identity and target URL.
// By default, a newly instantiated backend is marked as alive.
func NewBackend(name string, targetURL *url.URL) *Backend {
	b := &Backend{
		Name: name,
		URL:  targetURL,
	}
	b.alive.Store(true)
	return b
}

// SetAlive sets the operational health status of the backend.
func (b *Backend) SetAlive(alive bool) {
	b.alive.Store(alive)
}

// IsAlive returns true if the backend is currently marked healthy.
func (b *Backend) IsAlive() bool {
	return b.alive.Load()
}

// IncRequests atomically increments the total processed request counter.
func (b *Backend) IncRequests() uint64 {
	return b.totalReqs.Add(1)
}

// IncActive atomically increments the current active (in-flight) request counter.
func (b *Backend) IncActive() int64 {
	return b.activeReqs.Add(1)
}

// DecActive atomically decrements the current active (in-flight) request counter.
func (b *Backend) DecActive() int64 {
	return b.activeReqs.Add(-1)
}

// GetStats returns the total requests processed and the current active requests.
func (b *Backend) GetStats() (total uint64, active int64) {
	return b.totalReqs.Load(), b.activeReqs.Load()
}


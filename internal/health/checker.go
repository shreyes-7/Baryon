package health

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"baryon/internal/events"
	"baryon/internal/proxy"
)

// RingManager defines the hash ring operations needed by the health checker.
type RingManager interface {
	AddNode(backend *proxy.Backend) error
	RemoveNode(name string) error
	HasNode(name string) bool
}

// CheckerConfig holds timing and operational parameters for the health checker.
type CheckerConfig struct {
	Interval    time.Duration
	DialTimeout time.Duration
}

// HealthChecker periodically verifies the TCP connectivity of configured backends
// and synchronizes the active hash ring membership.
type HealthChecker struct {
	backends []*proxy.Backend
	ring     RingManager
	emitter  events.EventEmitter
	cfg      CheckerConfig

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// NewHealthChecker creates a new HealthChecker instance.
func NewHealthChecker(backends []*proxy.Backend, ring RingManager, emitter events.EventEmitter, cfg CheckerConfig) *HealthChecker {
	if cfg.Interval <= 0 {
		cfg.Interval = 3 * time.Second
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 2 * time.Second
	}
	return &HealthChecker{
		backends: backends,
		ring:     ring,
		emitter:  emitter,
		cfg:      cfg,
		done:     make(chan struct{}),
	}
}

// ProbeTCP performs a standalone TCP connectivity check against host:port with the configured timeout.
// This is executed completely outside of any hash ring locks.
func (hc *HealthChecker) ProbeTCP(ctx context.Context, target string) error {
	dialer := net.Dialer{Timeout: hc.cfg.DialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		return err
	}
	return conn.Close()
}

// CheckBackend probes an individual backend, executes state transitions,
// and synchronizes the hash ring membership.
func (hc *HealthChecker) CheckBackend(ctx context.Context, b *proxy.Backend) {
	target := b.URL.Host
	// If port is omitted in URL, default to 80
	if _, _, err := net.SplitHostPort(target); err != nil {
		target = net.JoinHostPort(target, "80")
	}

	// Dial TCP outside of any ring locks
	err := hc.ProbeTCP(ctx, target)
	healthy := (err == nil)

	wasAlive := b.IsAlive()

	// State transition: Alive -> Dead
	if wasAlive && !healthy {
		b.SetAlive(false)
		_ = hc.ring.RemoveNode(b.Name)
		if hc.emitter != nil {
			hc.emitter.EmitNodeStatus(events.NodeStatusEvent{
				Time:      time.Now(),
				BackendID: b.Name,
				Alive:     false,
				Error:     fmt.Sprintf("TCP probe failed: %v", err),
			})
		}
		return
	}

	// State transition: Dead -> Alive
	if !wasAlive && healthy {
		b.SetAlive(true)
		_ = hc.ring.AddNode(b)
		if hc.emitter != nil {
			hc.emitter.EmitNodeStatus(events.NodeStatusEvent{
				Time:      time.Now(),
				BackendID: b.Name,
				Alive:     true,
			})
		}
		return
	}

	// Double-check ring consistency: if marked alive but not in ring, re-add
	if healthy && !hc.ring.HasNode(b.Name) {
		_ = hc.ring.AddNode(b)
	}
}

// CheckAll checks all configured backends sequentially or concurrently.
func (hc *HealthChecker) CheckAll(ctx context.Context) {
	for _, b := range hc.backends {
		if ctx.Err() != nil {
			return
		}
		hc.CheckBackend(ctx, b)
	}
}

// Start begins the health check background loop with the provided context.
func (hc *HealthChecker) Start(parentCtx context.Context) {
	hc.mu.Lock()
	ctx, cancel := context.WithCancel(parentCtx)
	hc.cancel = cancel
	hc.done = make(chan struct{})
	hc.mu.Unlock()

	go func() {
		defer close(hc.done)

		// Run an initial check immediately on startup
		hc.CheckAll(ctx)

		ticker := time.NewTicker(hc.cfg.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				hc.CheckAll(ctx)
			}
		}
	}()
}

// Stop cancels the health checker loop and waits for it to finish cleanly.
func (hc *HealthChecker) Stop() {
	hc.mu.Lock()
	if hc.cancel != nil {
		hc.cancel()
	}
	done := hc.done
	hc.mu.Unlock()

	if done != nil {
		<-done
	}
}


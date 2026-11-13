package events

import (
	"sync"
	"time"
)

// RequestEvent is emitted when the proxy routes an HTTP request to a backend.
type RequestEvent struct {
	Time      time.Time `json:"time"`
	ClientIP  string    `json:"client_ip"`
	BackendID string    `json:"backend_id"`
	Path      string    `json:"path"`
}

// NodeStatusEvent is emitted when a backend's health state changes (e.g. up or down).
type NodeStatusEvent struct {
	Time      time.Time `json:"time"`
	BackendID string    `json:"backend_id"`
	Alive     bool      `json:"alive"`
	Error     string    `json:"error,omitempty"`
}

// BackendStat represents the point-in-time metrics and state of a single backend.
type BackendStat struct {
	Name       string  `json:"name"`
	Address    string  `json:"address"`
	Alive      bool    `json:"alive"`
	TotalReqs  uint64  `json:"total_reqs"`
	ActiveReqs int64   `json:"active_reqs"`
	SharePct   float64 `json:"share_pct"`
}

// ClusterSnapshot provides an authoritative, point-in-time snapshot of the entire load balancer.
type ClusterSnapshot struct {
	Timestamp     time.Time     `json:"timestamp"`
	Uptime        time.Duration `json:"uptime"`
	ProxyStatus   string        `json:"proxy_status"`
	TotalRequests uint64        `json:"total_requests"`
	MeasuredRPS   float64       `json:"measured_rps"`
	Backends      []BackendStat `json:"backends"`
}

// EventEmitter defines the interface for publishing proxy and health events non-blockingly.
type EventEmitter interface {
	EmitRequest(evt RequestEvent)
	EmitNodeStatus(evt NodeStatusEvent)
}

// EventBus is a bounded, concurrency-safe event bus that delivers events to subscribers without blocking.
type EventBus struct {
	mu         sync.RWMutex
	reqChan    chan RequestEvent
	statusChan chan NodeStatusEvent
	closed     bool
}

// NewEventBus creates a new EventBus with the specified channel capacities.
func NewEventBus(capacity int) *EventBus {
	if capacity <= 0 {
		capacity = 1000
	}
	return &EventBus{
		reqChan:    make(chan RequestEvent, capacity),
		statusChan: make(chan NodeStatusEvent, 200),
	}
}

// EmitRequest delivers a RequestEvent non-blockingly. If the channel is full, the event is dropped.
func (eb *EventBus) EmitRequest(evt RequestEvent) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	if eb.closed {
		return
	}

	select {
	case eb.reqChan <- evt:
	default:
		// Drop high-frequency event to prevent stalling proxy
	}
}

// EmitNodeStatus delivers a NodeStatusEvent non-blockingly.
func (eb *EventBus) EmitNodeStatus(evt NodeStatusEvent) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	if eb.closed {
		return
	}

	select {
	case eb.statusChan <- evt:
	default:
		// Bounded buffer prevents unbounded memory growth
	}
}

// RequestChannel returns the read-only channel for request events.
func (eb *EventBus) RequestChannel() <-chan RequestEvent {
	return eb.reqChan
}

// StatusChannel returns the read-only channel for node status events.
func (eb *EventBus) StatusChannel() <-chan NodeStatusEvent {
	return eb.statusChan
}

// Close closes the event channels safely.
func (eb *EventBus) Close() {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if !eb.closed {
		eb.closed = true
		close(eb.reqChan)
		close(eb.statusChan)
	}
}

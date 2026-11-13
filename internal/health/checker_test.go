package health

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sync"
	"testing"
	"time"

	"baryon/internal/events"
	"baryon/internal/hashing"
	"baryon/internal/proxy"
)

// mockEmitter collects emitted NodeStatusEvents for testing assertions.
type mockEmitter struct {
	mu     sync.Mutex
	events []events.NodeStatusEvent
}

func (m *mockEmitter) EmitRequest(evt events.RequestEvent) {}

func (m *mockEmitter) EmitNodeStatus(evt events.NodeStatusEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, evt)
}

func (m *mockEmitter) GetEvents() []events.NodeStatusEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]events.NodeStatusEvent, len(m.events))
	copy(copied, m.events)
	return copied
}

func TestHealthCheckerTransitions(t *testing.T) {
	// Start two raw TCP listeners to simulate two backends
	ln1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen ln1: %v", err)
	}
	defer ln1.Close()

	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen ln2: %v", err)
	}
	defer ln2.Close()

	u1, _ := url.Parse(fmt.Sprintf("http://%s", ln1.Addr().String()))
	u2, _ := url.Parse(fmt.Sprintf("http://%s", ln2.Addr().String()))

	b1 := proxy.NewBackend("backend-1", u1)
	b2 := proxy.NewBackend("backend-2", u2)

	ring := hashing.NewHashRing(50)
	// Initial state: both added
	_ = ring.AddNode(b1)
	_ = ring.AddNode(b2)

	emitter := &mockEmitter{}
	cfg := CheckerConfig{
		Interval:    50 * time.Millisecond,
		DialTimeout: 50 * time.Millisecond,
	}

	checker := NewHealthChecker([]*proxy.Backend{b1, b2}, ring, emitter, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	checker.Start(ctx)

	// Wait for initial check
	time.Sleep(100 * time.Millisecond)

	if !b1.IsAlive() || !b2.IsAlive() {
		t.Errorf("both backends should be alive initially")
	}
	if !ring.HasNode("backend-1") || !ring.HasNode("backend-2") {
		t.Errorf("both backends should be in ring")
	}

	// Now stop ln1 to simulate backend-1 failure
	addr1 := ln1.Addr().String()
	_ = ln1.Close()

	// Wait for health checker to detect failure
	time.Sleep(150 * time.Millisecond)

	if b1.IsAlive() {
		t.Errorf("backend-1 should be marked dead after closing listener")
	}
	if ring.HasNode("backend-1") {
		t.Errorf("backend-1 should be removed from ring after failure")
	}
	if !b2.IsAlive() || !ring.HasNode("backend-2") {
		t.Errorf("backend-2 should remain alive and in ring")
	}

	// Verify failure event was emitted
	evts := emitter.GetEvents()
	var foundDead bool
	for _, e := range evts {
		if e.BackendID == "backend-1" && !e.Alive {
			foundDead = true
			break
		}
	}
	if !foundDead {
		t.Errorf("expected failure event for backend-1")
	}

	// Now resurrect backend-1 by listening on the same port again
	newLn1, err := net.Listen("tcp", addr1)
	if err != nil {
		t.Fatalf("failed to re-listen on %s: %v", addr1, err)
	}
	defer newLn1.Close()

	// Wait for health checker to detect recovery
	time.Sleep(150 * time.Millisecond)

	if !b1.IsAlive() {
		t.Errorf("backend-1 should be marked alive after resurrection")
	}
	if !ring.HasNode("backend-1") {
		t.Errorf("backend-1 should be restored to ring after resurrection")
	}

	// Verify recovery event was emitted
	evts = emitter.GetEvents()
	var foundAlive bool
	for _, e := range evts {
		if e.BackendID == "backend-1" && e.Alive {
			foundAlive = true
			break
		}
	}
	if !foundAlive {
		t.Errorf("expected recovery event for backend-1")
	}

	// Stop checker
	checker.Stop()
}


func TestHealthCheckerNoDuplicateTransitions(t *testing.T) {
	// Point to an unused closed port
	u, _ := url.Parse("http://127.0.0.1:54320")
	b := proxy.NewBackend("unreachable", u)
	b.SetAlive(true)

	ring := hashing.NewHashRing(50)
	_ = ring.AddNode(b)

	emitter := &mockEmitter{}
	cfg := CheckerConfig{
		Interval:    30 * time.Millisecond,
		DialTimeout: 20 * time.Millisecond,
	}

	checker := NewHealthChecker([]*proxy.Backend{b}, ring, emitter, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	checker.Start(ctx)

	// Run for several ticks while backend remains dead
	time.Sleep(150 * time.Millisecond)
	cancel()
	checker.Stop()

	evts := emitter.GetEvents()
	deadCount := 0
	for _, e := range evts {
		if e.BackendID == "unreachable" && !e.Alive {
			deadCount++
		}
	}

	if deadCount != 1 {
		t.Errorf("expected exactly 1 dead transition event, got %d", deadCount)
	}
	if b.IsAlive() {
		t.Errorf("backend should be marked dead")
	}
	if ring.HasNode("unreachable") {
		t.Errorf("backend should not be in ring")
	}
}

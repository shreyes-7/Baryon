package simulator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestTrafficSimulator(t *testing.T) {
	var mu sync.Mutex
	receivedIPs := make(map[string]int)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		xff := r.Header.Get("X-Forwarded-For")
		mu.Lock()
		receivedIPs[xff]++
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	cfg := Config{
		TargetURL:  server.URL,
		TargetRPS:  100,
		ClientIPs:  DefaultClientIPs,
		Timeout:    1 * time.Second,
		MaxWorkers: 10,
	}

	sim := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())

	sim.Start(ctx)

	// Run for 200ms
	time.Sleep(200 * time.Millisecond)

	cancel()
	sim.Stop()

	sent, success, failed := sim.Stats()
	t.Logf("Simulator stats: sent=%d, success=%d, failed=%d", sent, success, failed)

	if success == 0 {
		t.Fatalf("expected successful requests, got %d", success)
	}

	mu.Lock()
	defer mu.Unlock()

	// Verify multiple client IPs were used
	if len(receivedIPs) < 5 {
		t.Errorf("expected at least 5 distinct client IPs to be observed, got %d", len(receivedIPs))
	}

	for _, ip := range DefaultClientIPs {
		if receivedIPs[ip] > 0 {
			t.Logf("Observed IP %s: %d requests", ip, receivedIPs[ip])
		}
	}
}

func TestTrafficSimulatorCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := Config{
		TargetURL:  server.URL,
		TargetRPS:  50,
		ClientIPs:  DefaultClientIPs,
		Timeout:    1 * time.Second,
		MaxWorkers: 5,
	}

	sim := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())

	sim.Start(ctx)
	// Cancel immediately
	cancel()
	sim.Stop()

	// Should exit cleanly without deadlock or hanging
}


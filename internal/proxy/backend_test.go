package proxy

import (
	"net/url"
	"sync"
	"testing"
)

func TestBackendAlive(t *testing.T) {
	u, err := url.Parse("http://localhost:8081")
	if err != nil {
		t.Fatalf("failed to parse url: %v", err)
	}

	b := NewBackend("backend-1", u)
	if !b.IsAlive() {
		t.Errorf("new backend should be alive by default")
	}

	b.SetAlive(false)
	if b.IsAlive() {
		t.Errorf("backend should be marked dead")
	}

	b.SetAlive(true)
	if !b.IsAlive() {
		t.Errorf("backend should be marked alive")
	}
}

func TestBackendConcurrentMetrics(t *testing.T) {
	u, _ := url.Parse("http://localhost:8081")
	b := NewBackend("backend-1", u)

	const goroutines = 50
	const iterations = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				b.IncRequests()
				b.IncActive()
				b.DecActive()
			}
		}()
	}

	wg.Wait()

	total, active := b.GetStats()
	expectedTotal := uint64(goroutines * iterations)
	if total != expectedTotal {
		t.Errorf("expected total requests %d, got %d", expectedTotal, total)
	}
	if active != 0 {
		t.Errorf("expected active requests 0, got %d", active)
	}
}


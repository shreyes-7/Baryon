package hashing

import (
	"fmt"
	"math"
	"net/url"
	"testing"

	"baryon/internal/proxy"
)

func newTestBackend(name, rawURL string) *proxy.Backend {
	u, _ := url.Parse(rawURL)
	return proxy.NewBackend(name, u)
}

func TestEmptyRing(t *testing.T) {
	ring := NewHashRing(100)
	if node := ring.GetNode("192.168.1.1"); node != nil {
		t.Fatalf("expected nil from empty ring, got %v", node)
	}
	if ring.NodeCount() != 0 {
		t.Errorf("expected 0 nodes, got %d", ring.NodeCount())
	}
}

func TestAddDuplicateNode(t *testing.T) {
	ring := NewHashRing(100)
	b1 := newTestBackend("backend-1", "http://localhost:8081")

	if err := ring.AddNode(b1); err != nil {
		t.Fatalf("unexpected error adding node: %v", err)
	}

	// Adding same node again should fail
	if err := ring.AddNode(b1); err != ErrNodeAlreadyExists {
		t.Fatalf("expected ErrNodeAlreadyExists, got %v", err)
	}

	// Adding nil backend should fail
	if err := ring.AddNode(nil); err != ErrNilBackend {
		t.Fatalf("expected ErrNilBackend, got %v", err)
	}
}

func TestRemoveNonExistentNode(t *testing.T) {
	ring := NewHashRing(100)
	if err := ring.RemoveNode("backend-unknown"); err != ErrNodeNotFound {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
	if err := ring.RemoveNode(""); err != ErrEmptyBackendName {
		t.Fatalf("expected ErrEmptyBackendName, got %v", err)
	}
}

func TestAddRemoveNode(t *testing.T) {
	ring := NewHashRing(100)
	b1 := newTestBackend("backend-1", "http://localhost:8081")
	b2 := newTestBackend("backend-2", "http://localhost:8082")

	if err := ring.AddNode(b1); err != nil {
		t.Fatalf("failed to add b1: %v", err)
	}
	if err := ring.AddNode(b2); err != nil {
		t.Fatalf("failed to add b2: %v", err)
	}

	if ring.NodeCount() != 2 {
		t.Errorf("expected 2 nodes, got %d", ring.NodeCount())
	}
	if !ring.HasNode("backend-1") || !ring.HasNode("backend-2") {
		t.Errorf("expected both backends to be registered")
	}

	// Lookup should return one of them
	node := ring.GetNode("10.0.0.1")
	if node == nil || (node.Name != "backend-1" && node.Name != "backend-2") {
		t.Fatalf("unexpected node returned: %v", node)
	}

	// Remove b1
	if err := ring.RemoveNode("backend-1"); err != nil {
		t.Fatalf("failed to remove b1: %v", err)
	}
	if ring.NodeCount() != 1 {
		t.Errorf("expected 1 node, got %d", ring.NodeCount())
	}
	if ring.HasNode("backend-1") {
		t.Errorf("backend-1 should not exist")
	}

	// All lookups now must return b2
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("192.168.1.%d", i)
		n := ring.GetNode(key)
		if n == nil || n.Name != "backend-2" {
			t.Fatalf("expected backend-2, got %v for key %s", n, key)
		}
	}
}

func TestStickyRouting(t *testing.T) {
	ring := NewHashRing(100)
	b1 := newTestBackend("backend-1", "http://localhost:8081")
	b2 := newTestBackend("backend-2", "http://localhost:8082")
	b3 := newTestBackend("backend-3", "http://localhost:8083")

	_ = ring.AddNode(b1)
	_ = ring.AddNode(b2)
	_ = ring.AddNode(b3)

	clientIPs := []string{
		"192.168.1.10",
		"10.0.0.5",
		"172.16.0.42",
		"203.0.113.195",
		"198.51.100.8",
	}

	// First pass: capture initial assigned backend for each IP
	initialMap := make(map[string]string)
	for _, ip := range clientIPs {
		node := ring.GetNode(ip)
		if node == nil {
			t.Fatalf("expected node for IP %s", ip)
		}
		initialMap[ip] = node.Name
	}

	// Repeated passes: verify 1,000 lookups per IP stay on the exact same backend
	for iteration := 0; iteration < 1000; iteration++ {
		for _, ip := range clientIPs {
			node := ring.GetNode(ip)
			if node.Name != initialMap[ip] {
				t.Fatalf("sticky routing violated for IP %s on iteration %d: expected %s, got %s",
					ip, iteration, initialMap[ip], node.Name)
			}
		}
	}
}

func TestDistribution(t *testing.T) {
	ring := NewHashRing(100)
	b1 := newTestBackend("backend-1", "http://localhost:8081")
	b2 := newTestBackend("backend-2", "http://localhost:8082")
	b3 := newTestBackend("backend-3", "http://localhost:8083")

	_ = ring.AddNode(b1)
	_ = ring.AddNode(b2)
	_ = ring.AddNode(b3)

	const totalKeys = 10000
	counts := make(map[string]int)

	// Generate 10,000 deterministic test keys
	for i := 0; i < totalKeys; i++ {
		key := fmt.Sprintf("client-%d-%d", i*7+3, i)
		node := ring.GetNode(key)
		if node == nil {
			t.Fatalf("nil node returned for key %s", key)
		}
		counts[node.Name]++
	}

	expectedShare := 1.0 / 3.0 // ~33.3%
	// Statistical tolerance: For V=100 virtual nodes, theoretical variance is ~1/sqrt(V) = 10%.
	// Per PROJECT_CONTEXT.md section 13.1, tolerance is adjusted to 10 percentage points (23.3% - 43.3%)
	// based on empirical deterministic CRC32 distribution over 3 nodes.
	tolerance := 0.10

	for name, count := range counts {
		share := float64(count) / float64(totalKeys)
		diff := math.Abs(share - expectedShare)
		t.Logf("Backend %s: %d keys (%.2f%% share, diff: %.2f%%)", name, count, share*100, diff*100)
		if diff > tolerance {
			t.Errorf("Backend %s share %.2f%% outside tolerance (diff %.2f%% > %.2f%%)",
				name, share*100, diff*100, tolerance*100)
		}
	}
}

func TestMinimalRemapping(t *testing.T) {
	ring := NewHashRing(100)
	b1 := newTestBackend("backend-1", "http://localhost:8081")
	b2 := newTestBackend("backend-2", "http://localhost:8082")
	b3 := newTestBackend("backend-3", "http://localhost:8083")

	_ = ring.AddNode(b1)
	_ = ring.AddNode(b2)
	_ = ring.AddNode(b3)

	const totalKeys = 10000
	initialMappings := make(map[string]string, totalKeys)

	for i := 0; i < totalKeys; i++ {
		key := fmt.Sprintf("route-key-%d", i)
		node := ring.GetNode(key)
		initialMappings[key] = node.Name
	}

	// Remove backend-1
	if err := ring.RemoveNode("backend-1"); err != nil {
		t.Fatalf("failed to remove backend-1: %v", err)
	}

	remappedFromB1 := 0
	disruptedOther := 0

	for key, originalBackend := range initialMappings {
		newNode := ring.GetNode(key)
		if newNode == nil {
			t.Fatalf("nil node returned after removal for key %s", key)
		}

		if newNode.Name == "backend-1" {
			t.Fatalf("key %s still routed to removed backend-1", key)
		}

		if originalBackend == "backend-1" {
			// This key had to be remapped to b2 or b3
			remappedFromB1++
		} else {
			// This key originally belonged to b2 or b3. Under consistent hashing,
			// it should NOT change!
			if newNode.Name != originalBackend {
				disruptedOther++
			}
		}
	}

	t.Logf("Keys remapped from removed backend-1: %d", remappedFromB1)
	t.Logf("Unaffected keys disrupted: %d", disruptedOther)

	if disruptedOther != 0 {
		t.Errorf("Expected 0 disruptions for keys not mapped to backend-1, got %d", disruptedOther)
	}
}

func BenchmarkHashRingGetNode(b *testing.B) {
	ring := NewHashRing(100)
	b1 := newTestBackend("backend-1", "http://localhost:8081")
	b2 := newTestBackend("backend-2", "http://localhost:8082")
	b3 := newTestBackend("backend-3", "http://localhost:8083")

	_ = ring.AddNode(b1)
	_ = ring.AddNode(b2)
	_ = ring.AddNode(b3)

	keys := []string{
		"192.168.1.1", "192.168.1.2", "192.168.1.3", "192.168.1.4", "192.168.1.5",
		"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4", "10.0.0.5",
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		idx := 0
		for pb.Next() {
			key := keys[idx%len(keys)]
			node := ring.GetNode(key)
			if node == nil {
				b.Fatal("nil node returned")
			}
			idx++
		}
	})
}

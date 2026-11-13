package proxy_test

import (
	"context"
	"fmt"
	"net"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"baryon/internal/events"
	"baryon/internal/hashing"
	"baryon/internal/health"
	"baryon/internal/proxy"
	"baryon/pkg/simulator"
)

func getFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestEndToEndFailureAndRecoverySimulation(t *testing.T) {
	// 1. Setup 3 dynamic demo backend servers
	p1 := getFreePort(t)
	p2 := getFreePort(t)
	p3 := getFreePort(t)

	backendsConfig := []struct {
		Name string
		Addr string
	}{
		{Name: "backend-1", Addr: fmt.Sprintf("127.0.0.1:%d", p1)},
		{Name: "backend-2", Addr: fmt.Sprintf("127.0.0.1:%d", p2)},
		{Name: "backend-3", Addr: fmt.Sprintf("127.0.0.1:%d", p3)},
	}

	dm := proxy.NewDemoManager(backendsConfig)
	if err := dm.StartAll(); err != nil {
		t.Fatalf("failed to start demo servers: %v", err)
	}
	defer dm.StopAll()

	// 2. Setup backend models and HashRing
	u1, _ := url.Parse(fmt.Sprintf("http://%s", backendsConfig[0].Addr))
	u2, _ := url.Parse(fmt.Sprintf("http://%s", backendsConfig[1].Addr))
	u3, _ := url.Parse(fmt.Sprintf("http://%s", backendsConfig[2].Addr))

	b1 := proxy.NewBackend("backend-1", u1)
	b2 := proxy.NewBackend("backend-2", u2)
	b3 := proxy.NewBackend("backend-3", u3)

	proxy.SetupBackendReverseProxy(b1, nil)
	proxy.SetupBackendReverseProxy(b2, nil)
	proxy.SetupBackendReverseProxy(b3, nil)

	ring := hashing.NewHashRing(100)
	_ = ring.AddNode(b1)
	_ = ring.AddNode(b2)
	_ = ring.AddNode(b3)

	eventBus := events.NewEventBus(500)
	defer eventBus.Close()

	// 3. Setup Proxy Server
	proxyHandler := proxy.NewProxyHandler(ring, eventBus, []string{"127.0.0.1"})
	proxyServer := httptest.NewServer(proxyHandler)
	defer proxyServer.Close()

	// 4. Setup Health Checker with fast interval
	healthCfg := health.CheckerConfig{
		Interval:    50 * time.Millisecond,
		DialTimeout: 50 * time.Millisecond,
	}
	healthChecker := health.NewHealthChecker([]*proxy.Backend{b1, b2, b3}, ring, eventBus, healthCfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	healthChecker.Start(ctx)
	defer healthChecker.Stop()

	// 5. Setup Traffic Simulator targeting Proxy Server
	simCfg := simulator.Config{
		TargetURL:  proxyServer.URL,
		TargetRPS:  100,
		ClientIPs:  simulator.DefaultClientIPs,
		Timeout:    1 * time.Second,
		MaxWorkers: 15,
	}
	trafficSim := simulator.New(simCfg)
	trafficSim.Start(ctx)
	defer trafficSim.Stop()

	// Let traffic run on all 3 backends
	time.Sleep(300 * time.Millisecond)

	b1Init, _ := b1.GetStats()
	b2Init, _ := b2.GetStats()
	b3Init, _ := b3.GetStats()

	t.Logf("Initial stats: b1=%d, b2=%d, b3=%d", b1Init, b2Init, b3Init)

	if b1Init == 0 || b2Init == 0 || b3Init == 0 {
		t.Fatalf("expected all backends to receive initial traffic, got b1=%d, b2=%d, b3=%d",
			b1Init, b2Init, b3Init)
	}

	// 6. Simulate 'K' (Kill backend-1 listener)
	t.Log("Simulating 'K': stopping backend-1 listener...")
	if err := dm.StopBackend("backend-1"); err != nil {
		t.Fatalf("failed to stop backend-1: %v", err)
	}

	// Wait for health checker to detect failure and remove backend-1
	time.Sleep(200 * time.Millisecond)

	if b1.IsAlive() {
		t.Errorf("backend-1 should be marked dead")
	}
	if ring.HasNode("backend-1") {
		t.Errorf("backend-1 should be removed from hash ring")
	}

	b1AtFailure, _ := b1.GetStats()
	t.Logf("Backend-1 stats at failure: %d", b1AtFailure)

	// Continue traffic under 2 backends
	time.Sleep(300 * time.Millisecond)

	b1PostFailure, _ := b1.GetStats()
	b2PostFailure, _ := b2.GetStats()
	b3PostFailure, _ := b3.GetStats()

	t.Logf("Stats after failure: b1=%d, b2=%d, b3=%d", b1PostFailure, b2PostFailure, b3PostFailure)

	// Backend-1 must NOT have received any new requests after removal
	if b1PostFailure > b1AtFailure {
		t.Errorf("backend-1 received %d new requests after removal", b1PostFailure-b1AtFailure)
	}

	// Backend-2 and Backend-3 must have absorbed the traffic
	if b2PostFailure <= b2Init || b3PostFailure <= b3Init {
		t.Errorf("remaining backends did not absorb traffic: b2Delta=%d, b3Delta=%d",
			b2PostFailure-b2Init, b3PostFailure-b3Init)
	}

	// 7. Simulate 'R' (Restart backend-1)
	t.Log("Simulating 'R': restarting backend-1 listener...")
	if err := dm.RestartBackend("backend-1"); err != nil {
		t.Fatalf("failed to restart backend-1: %v", err)
	}

	// Wait for health checker to detect recovery and re-add backend-1
	time.Sleep(200 * time.Millisecond)

	if !b1.IsAlive() {
		t.Errorf("backend-1 should be marked alive after restart")
	}
	if !ring.HasNode("backend-1") {
		t.Errorf("backend-1 should be re-added to hash ring")
	}

	// Let traffic run again with all 3 backends restored
	time.Sleep(300 * time.Millisecond)

	b1Recovered, _ := b1.GetStats()
	t.Logf("Stats after recovery: b1=%d", b1Recovered)

	// Backend-1 must have resumed receiving requests!
	if b1Recovered <= b1PostFailure {
		t.Errorf("backend-1 did not receive new requests after recovery: before=%d, after=%d",
			b1PostFailure, b1Recovered)
	}

	t.Log("Simulation successfully completed: Failure detected, traffic redistributed, recovered, traffic restored.")
}

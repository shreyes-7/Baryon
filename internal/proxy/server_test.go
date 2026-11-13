package proxy

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

// getFreePort asks the kernel for a free open TCP port.
func getFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestDemoServerLifecycle(t *testing.T) {
	port1 := getFreePort(t)
	port2 := getFreePort(t)
	port3 := getFreePort(t)

	backends := []struct {
		Name string
		Addr string
	}{
		{Name: "backend-1", Addr: fmt.Sprintf("127.0.0.1:%d", port1)},
		{Name: "backend-2", Addr: fmt.Sprintf("127.0.0.1:%d", port2)},
		{Name: "backend-3", Addr: fmt.Sprintf("127.0.0.1:%d", port3)},
	}

	dm := NewDemoManager(backends)
	if err := dm.StartAll(); err != nil {
		t.Fatalf("failed to start all demo servers: %v", err)
	}
	defer dm.StopAll()

	client := &http.Client{Timeout: 2 * time.Second}

	// Verify all 3 are responding
	for _, b := range backends {
		resp, err := client.Get(fmt.Sprintf("http://%s/test-path", b.Addr))
		if err != nil {
			t.Fatalf("failed to query %s: %v", b.Name, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 OK from %s, got %d", b.Name, resp.StatusCode)
		}
		var payload DummyResponse
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			resp.Body.Close()
			t.Fatalf("failed to decode json from %s: %v", b.Name, err)
		}
		resp.Body.Close()

		if payload.Backend != b.Name {
			t.Errorf("expected backend %s, got %s", b.Name, payload.Backend)
		}
		if payload.Path != "/test-path" {
			t.Errorf("expected path /test-path, got %s", payload.Path)
		}
	}

	// Stop backend-1
	if err := dm.StopBackend("backend-1"); err != nil {
		t.Fatalf("failed to stop backend-1: %v", err)
	}
	if dm.IsRunning("backend-1") {
		t.Errorf("backend-1 should report not running")
	}

	// Verify backend-1 is unreachable
	_, err := client.Get(fmt.Sprintf("http://%s/", backends[0].Addr))
	if err == nil {
		t.Errorf("expected error connecting to stopped backend-1, got success")
	}

	// Restart backend-1
	if err := dm.RestartBackend("backend-1"); err != nil {
		t.Fatalf("failed to restart backend-1: %v", err)
	}
	if !dm.IsRunning("backend-1") {
		t.Errorf("backend-1 should report running after restart")
	}

	// Verify backend-1 is reachable again
	resp, err := client.Get(fmt.Sprintf("http://%s/", backends[0].Addr))
	if err != nil {
		t.Fatalf("failed to connect to restarted backend-1: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK after restart, got %d", resp.StatusCode)
	}
}


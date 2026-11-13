package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"baryon/internal/events"
)

// mockRing implements Ring for proxy testing.
type mockRing struct {
	mapping map[string]*Backend
	nodes   []*Backend
}

func (m *mockRing) GetNode(key string) *Backend {
	if len(m.nodes) == 0 {
		return nil
	}
	if b, ok := m.mapping[key]; ok {
		return b
	}
	return m.nodes[0]
}

func TestProxyHandlerRouting(t *testing.T) {
	// Start 3 test upstream servers
	s1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Echo", r.Header.Get("X-Custom-Test"))
		w.Header().Set("X-Path-Echo", r.URL.Path)
		w.Header().Set("X-Query-Echo", r.URL.RawQuery)
		_ = json.NewEncoder(w).Encode(map[string]string{"backend": "backend-1"})
	}))
	defer s1.Close()

	s2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"backend": "backend-2"})
	}))
	defer s2.Close()

	s3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"backend": "backend-3"})
	}))
	defer s3.Close()

	u1, _ := url.Parse(s1.URL)
	u2, _ := url.Parse(s2.URL)
	u3, _ := url.Parse(s3.URL)

	b1 := NewBackend("backend-1", u1)
	b2 := NewBackend("backend-2", u2)
	b3 := NewBackend("backend-3", u3)

	SetupBackendReverseProxy(b1, nil)
	SetupBackendReverseProxy(b2, nil)
	SetupBackendReverseProxy(b3, nil)

	ring := &mockRing{
		mapping: map[string]*Backend{
			"192.168.1.10": b1,
			"192.168.1.20": b2,
			"192.168.1.30": b3,
		},
		nodes: []*Backend{b1, b2, b3},
	}

	eventBus := events.NewEventBus(100)
	handler := NewProxyHandler(ring, eventBus, []string{"127.0.0.1"})

	proxyServer := httptest.NewServer(handler)
	defer proxyServer.Close()

	client := proxyServer.Client()

	tests := []struct {
		clientIP        string
		expectedBackend string
	}{
		{"192.168.1.10", "backend-1"},
		{"192.168.1.20", "backend-2"},
		{"192.168.1.30", "backend-3"},
	}

	for _, tc := range tests {
		req, _ := http.NewRequest(http.MethodGet, proxyServer.URL+"/api/items?filter=active", nil)
		req.Header.Set("X-Forwarded-For", tc.clientIP)
		req.Header.Set("X-Custom-Test", "BaryonHeader")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed for %s: %v", tc.clientIP, err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", resp.StatusCode)
		}

		var body map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()

		if body["backend"] != tc.expectedBackend {
			t.Errorf("expected backend %s for IP %s, got %s", tc.expectedBackend, tc.clientIP, body["backend"])
		}

		// Verify path, query, and headers were preserved on b1
		if tc.expectedBackend == "backend-1" {
			if resp.Header.Get("X-Custom-Echo") != "BaryonHeader" {
				t.Errorf("custom header not forwarded")
			}
			if resp.Header.Get("X-Path-Echo") != "/api/items" {
				t.Errorf("path not forwarded correctly: %s", resp.Header.Get("X-Path-Echo"))
			}
			if resp.Header.Get("X-Query-Echo") != "filter=active" {
				t.Errorf("query string not forwarded correctly: %s", resp.Header.Get("X-Query-Echo"))
			}
		}
	}

	// Verify metrics
	if handler.TotalRequests() != 3 {
		t.Errorf("expected 3 total requests, got %d", handler.TotalRequests())
	}
	b1Total, _ := b1.GetStats()
	if b1Total != 1 {
		t.Errorf("expected 1 request on b1, got %d", b1Total)
	}
}

func TestProxyHandlerEmptyRing(t *testing.T) {
	ring := &mockRing{nodes: nil}
	handler := NewProxyHandler(ring, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "http://localhost:8080/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var errResp map[string]string
	_ = json.Unmarshal(body, &errResp)
	if errResp["error"] != "service unavailable" {
		t.Errorf("unexpected error message: %s", string(body))
	}
}

func TestProxyHandlerBackendDown(t *testing.T) {
	// Point backend to closed port
	u, _ := url.Parse("http://127.0.0.1:54321")
	b := NewBackend("dead-backend", u)
	SetupBackendReverseProxy(b, nil)

	ring := &mockRing{
		nodes: []*Backend{b},
	}

	handler := NewProxyHandler(ring, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "http://localhost:8080/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 Bad Gateway for dead backend, got %d", resp.StatusCode)
	}
}


func BenchmarkProxyHandler(b *testing.B) {
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer backendServer.Close()

	u, _ := url.Parse(backendServer.URL)
	backend := NewBackend("bench-backend", u)
	SetupBackendReverseProxy(backend, nil)

	ring := &mockRing{
		nodes: []*Backend{backend},
	}
	handler := NewProxyHandler(ring, nil, nil)
	proxyServer := httptest.NewServer(handler)
	defer proxyServer.Close()

	client := proxyServer.Client()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := client.Get(proxyServer.URL)
			if err != nil {
				b.Fatalf("benchmark request failed: %v", err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	})
}

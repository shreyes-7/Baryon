package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// DummyResponse represents the JSON payload returned by a demo backend server.
type DummyResponse struct {
	Backend string `json:"backend"`
	Address string `json:"address"`
	Status  string `json:"status"`
	Path    string `json:"path,omitempty"`
}

// DemoServer represents an individual manageable dummy HTTP backend.
type DemoServer struct {
	Name    string
	Addr    string
	mu      sync.Mutex
	server  *http.Server
	ln      net.Listener
	running bool
}

// NewDemoServer creates a new manageable demo backend server.
func NewDemoServer(name, addr string) *DemoServer {
	return &DemoServer{
		Name: name,
		Addr: addr,
	}
}

// Start creates the TCP listener and begins serving HTTP requests in the background.
func (ds *DemoServer) Start() error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if ds.running {
		return fmt.Errorf("backend server %s is already running", ds.Name)
	}

	ln, err := net.Listen("tcp", ds.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", ds.Addr, err)
	}
	ds.ln = ln

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Backend", ds.Name)
		resp := DummyResponse{
			Backend: ds.Name,
			Address: ds.Addr,
			Status:  "ok",
			Path:    r.URL.Path,
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	server := &http.Server{
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  15 * time.Second,
	}
	ds.server = server
	ds.running = true

	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			// Listener closed or server error
		}
	}()

	return nil
}

// Stop closes the server listener and terminates in-flight connections gracefully.
func (ds *DemoServer) Stop() error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if !ds.running {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := ds.server.Shutdown(ctx)
	ds.running = false
	ds.server = nil
	ds.ln = nil
	return err
}

// Restart stops the server if running and rebinds the listener.
func (ds *DemoServer) Restart() error {
	if err := ds.Stop(); err != nil {
		return fmt.Errorf("failed to stop %s during restart: %w", ds.Name, err)
	}
	// Small sleep to ensure OS TCP socket release
	time.Sleep(50 * time.Millisecond)
	return ds.Start()
}

// IsRunning returns whether the backend listener is currently active.
func (ds *DemoServer) IsRunning() bool {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return ds.running
}

// DemoManager coordinates the lifecycle of all demo backend servers.
type DemoManager struct {
	mu      sync.RWMutex
	servers map[string]*DemoServer
}

// NewDemoManager creates a DemoManager for the configured backends.
func NewDemoManager(backends []struct {
	Name string
	Addr string
}) *DemoManager {
	m := &DemoManager{
		servers: make(map[string]*DemoServer),
	}
	for _, b := range backends {
		m.servers[b.Name] = NewDemoServer(b.Name, b.Addr)
	}
	return m
}

// StartAll starts all configured backend servers.
func (dm *DemoManager) StartAll() error {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	for _, s := range dm.servers {
		if err := s.Start(); err != nil {
			return err
		}
	}
	return nil
}

// StopAll stops all configured backend servers.
func (dm *DemoManager) StopAll() error {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	var firstErr error
	for _, s := range dm.servers {
		if err := s.Stop(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// StopBackend stops an individual backend server by name.
func (dm *DemoManager) StopBackend(name string) error {
	dm.mu.RLock()
	s, ok := dm.servers[name]
	dm.mu.RUnlock()

	if !ok {
		return fmt.Errorf("backend %q not found", name)
	}
	return s.Stop()
}

// RestartBackend restarts an individual backend server by name.
func (dm *DemoManager) RestartBackend(name string) error {
	dm.mu.RLock()
	s, ok := dm.servers[name]
	dm.mu.RUnlock()

	if !ok {
		return fmt.Errorf("backend %q not found", name)
	}
	return s.Restart()
}

// IsRunning returns whether the named backend is currently running.
func (dm *DemoManager) IsRunning(name string) bool {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	s, ok := dm.servers[name]
	if !ok {
		return false
	}
	return s.IsRunning()
}


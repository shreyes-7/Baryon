package simulator

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultClientIPs contains the 10 stable client IP addresses used for traffic simulation.
var DefaultClientIPs = []string{
	"192.168.1.10",
	"192.168.1.20",
	"192.168.1.30",
	"192.168.1.40",
	"192.168.1.50",
	"10.0.0.1",
	"10.0.0.2",
	"10.0.0.3",
	"172.16.0.100",
	"172.16.0.200",
}

// Config specifies the runtime settings for the TrafficSimulator.
type Config struct {
	TargetURL  string
	TargetRPS  int
	ClientIPs  []string
	Timeout    time.Duration
	MaxWorkers int
}

// DefaultConfig provides defaults matching PROJECT_CONTEXT.md.
func DefaultConfig() Config {
	return Config{
		TargetURL:  "http://localhost:8080",
		TargetRPS:  500,
		ClientIPs:  DefaultClientIPs,
		Timeout:    5 * time.Second,
		MaxWorkers: 50,
	}
}

// TrafficSimulator continuously generates simulated HTTP traffic against the BARYON load balancer.
type TrafficSimulator struct {
	cfg        Config
	httpClient *http.Client

	// Atomic counters
	sentRequests    atomic.Uint64
	successRequests atomic.Uint64
	failedRequests  atomic.Uint64

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a new TrafficSimulator with the given configuration.
func New(cfg Config) *TrafficSimulator {
	if cfg.TargetRPS <= 0 {
		cfg.TargetRPS = 500
	}
	if len(cfg.ClientIPs) == 0 {
		cfg.ClientIPs = DefaultClientIPs
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = 50
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   2 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          500,
		MaxIdleConnsPerHost:   200,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   3 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   cfg.Timeout,
	}

	return &TrafficSimulator{
		cfg:        cfg,
		httpClient: client,
	}
}

// Start begins sending traffic in the background until the context is cancelled.
func (s *TrafficSimulator) Start(parentCtx context.Context) {
	s.mu.Lock()
	ctx, cancel := context.WithCancel(parentCtx)
	s.cancel = cancel
	s.mu.Unlock()

	tasks := make(chan string, s.cfg.TargetRPS*2)

	// Start worker pool to execute HTTP requests without unbounded goroutine growth
	for i := 0; i < s.cfg.MaxWorkers; i++ {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case clientIP, ok := <-tasks:
					if !ok {
						return
					}
					s.sendRequest(ctx, clientIP)
				}
			}
		}()
	}

	// Dispatcher goroutine that paces tasks according to TargetRPS
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer close(tasks)

		interval := time.Second / time.Duration(s.cfg.TargetRPS)
		if interval < time.Microsecond {
			interval = time.Microsecond
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		ipIdx := 0
		numIPs := len(s.cfg.ClientIPs)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ip := s.cfg.ClientIPs[ipIdx%numIPs]
				ipIdx++

				select {
				case tasks <- ip:
				default:
					// If workers are saturated, drop task to avoid queue bloat
				}
			}
		}
	}()
}

// sendRequest executes a single HTTP request with the simulated client IP in X-Forwarded-For.
func (s *TrafficSimulator) sendRequest(ctx context.Context, clientIP string) {
	s.sentRequests.Add(1)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.TargetURL, nil)
	if err != nil {
		s.failedRequests.Add(1)
		return
	}

	req.Header.Set("X-Forwarded-For", clientIP)
	req.Header.Set("User-Agent", "Baryon-Traffic-Simulator/1.0")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		s.failedRequests.Add(1)
		return
	}

	// Read and discard body, then close
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		s.successRequests.Add(1)
	} else {
		s.failedRequests.Add(1)
	}
}

// Stop gracefully terminates the simulator and waits for workers to finish.
func (s *TrafficSimulator) Stop() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()

	s.wg.Wait()
}

// Stats returns the total sent, successful, and failed request counts.
func (s *TrafficSimulator) Stats() (sent, success, failed uint64) {
	return s.sentRequests.Load(), s.successRequests.Load(), s.failedRequests.Load()
}
